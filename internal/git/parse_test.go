package git

import (
	"strings"
	"testing"
)

// nul builds a for-each-ref record from its eight fields, so the tables below
// stay readable instead of being littered with \x00.
func nul(fields ...string) string { return strings.Join(fields, "\x00") + "\n" }

func TestDeriveSync(t *testing.T) {
	cases := []struct {
		name          string
		upstream      string
		track         string
		want          SyncState
		ahead, behind int
	}{
		// Four of the ten repositories this was designed against are in this
		// state, so it is the common case rather than an edge case.
		{"no upstream configured", "", "", SyncNoUpstream, 0, 0},
		{"no upstream ignores track", "", "[ahead 3]", SyncNoUpstream, 0, 0},
		{"upstream deleted", "origin/gone", "[gone]", SyncGone, 0, 0},
		{"in sync", "origin/main", "", SyncInSync, 0, 0},
		{"ahead", "origin/main", "[ahead 3]", SyncAhead, 3, 0},
		{"behind", "origin/main", "[behind 5]", SyncBehind, 0, 5},
		{"diverged", "origin/main", "[ahead 1, behind 2]", SyncDiverged, 1, 2},
		{"unparseable track counts as in sync", "origin/main", "[weird]", SyncInSync, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ahead, behind := deriveSync(tc.upstream, tc.track)
			if got != tc.want || ahead != tc.ahead || behind != tc.behind {
				t.Errorf("deriveSync(%q, %q) = %v/%d/%d, want %v/%d/%d",
					tc.upstream, tc.track, got, ahead, behind, tc.want, tc.ahead, tc.behind)
			}
		})
	}
}

func TestParseForEachRef(t *testing.T) {
	// Both records are the real output shape captured from repositories in
	// ~/Workspace/Schretzi: one tracking, one with no upstream at all.
	in := nul("*", "main", "origin/main", "", "=", "1786711038", "36fa0fb", "/repo") +
		nul(" ", "feature/x", "origin/feature/x", "[ahead 1, behind 2]", "<>", "1786700000", "abc1234", "") +
		nul("*", "solo", "", "", "", "1787321869", "1103fdb", "/other")

	got := parseForEachRef([]byte(in))
	if len(got) != 3 {
		t.Fatalf("got %d branches, want 3", len(got))
	}

	if !got[0].Head || got[0].Name != "main" || got[0].Sync != SyncInSync {
		t.Errorf("branch 0 = %+v, want head main in sync", got[0])
	}
	if got[0].Worktree != "/repo" {
		t.Errorf("branch 0 worktree = %q, want /repo", got[0].Worktree)
	}
	if got[0].When.Unix() != 1786711038 {
		t.Errorf("branch 0 When = %v, want unix 1786711038", got[0].When)
	}

	if got[1].Head {
		t.Error("branch 1 should not be HEAD")
	}
	if got[1].Sync != SyncDiverged || got[1].Ahead != 1 || got[1].Behind != 2 {
		t.Errorf("branch 1 = %v/%d/%d, want diverged/1/2", got[1].Sync, got[1].Ahead, got[1].Behind)
	}
	// A branch that is not checked out anywhere has no worktree path.
	if got[1].Worktree != "" {
		t.Errorf("branch 1 worktree = %q, want empty", got[1].Worktree)
	}

	if got[2].Sync != SyncNoUpstream {
		t.Errorf("branch 2 sync = %v, want no-upstream", got[2].Sync)
	}
}

func TestParseForEachRefSkipsMalformed(t *testing.T) {
	// A single unreadable ref must not blank out the whole repository.
	in := "not\x00enough\n" + nul("*", "main", "", "", "", "0", "abc", "") + "\n"
	got := parseForEachRef([]byte(in))
	if len(got) != 1 || got[0].Name != "main" {
		t.Fatalf("got %+v, want just main", got)
	}
}

func TestParseForEachRefBranchNameWithPipe(t *testing.T) {
	// The reason the format uses NUL and not '|': this is a legal branch name.
	in := nul(" ", "feat|weird", "", "", "", "0", "abc1234", "")
	got := parseForEachRef([]byte(in))
	if len(got) != 1 || got[0].Name != "feat|weird" {
		t.Fatalf("got %+v, want a single branch named feat|weird", got)
	}
}

func TestParseStatusV2(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Status
	}{
		{
			// Captured from OauthMailToken: a tracking branch, clean.
			name: "clean and tracking",
			in: "# branch.oid 36fa0fbe\n" +
				"# branch.head main\n" +
				"# branch.upstream origin/main\n" +
				"# branch.ab +0 -0\n",
			want: Status{Head: "main", Upstream: "origin/main"},
		},
		{
			// Captured from MacbookSetup: no branch.upstream line at all.
			name: "no upstream emits no upstream header",
			in: "# branch.oid 96a5f92d\n" +
				"# branch.head main\n" +
				"1 .M N... 100644 100644 100644 874a85a0 874a85a0 Brewfiles/30_dev.brew\n",
			want: Status{Head: "main", Modified: 1, Files: 1},
		},
		{
			name: "counts every category",
			in: "# branch.head main\n" +
				"# branch.upstream origin/main\n" +
				"# branch.ab +2 -3\n" +
				"1 M. N... 100644 100644 100644 aaa bbb staged.go\n" +
				"1 .M N... 100644 100644 100644 aaa bbb modified.go\n" +
				"1 MM N... 100644 100644 100644 aaa bbb both.go\n" +
				"2 R. N... 100644 100644 100644 aaa bbb R100 new.go\x00old.go\n" +
				"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.go\n" +
				"? untracked.go\n" +
				"? another one with spaces.go\n",
			want: Status{
				Head: "main", Upstream: "origin/main", Ahead: 2, Behind: 3,
				// staged: staged.go, both.go, new.go   modified: modified.go, both.go
				Staged: 3, Modified: 2, Conflicted: 1, Untracked: 2,
				// Seven distinct paths, though both.go counts towards two
				// categories, so Total() is 8 and Files is 7.
				Files: 7,
			},
		},
		{
			name: "detached head",
			in:   "# branch.head (detached)\n",
			want: Status{Head: "(detached)"},
		},
		{
			// A repository with no commits yet: git reports the literal
			// "(initial)" as the oid. Without this the unborn branch would be
			// misread as a detached HEAD, since it has no ref to list.
			name: "unborn branch",
			in:   "# branch.oid (initial)\n# branch.head main\n? a.txt\n",
			want: Status{Head: "main", Unborn: true, Untracked: 1, Files: 1},
		},
		{name: "empty input", in: "", want: Status{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStatusV2([]byte(tc.in))
			if got != tc.want {
				t.Errorf("parseStatusV2() =\n %+v\nwant\n %+v", got, tc.want)
			}
		})
	}
}

func TestStatusDirty(t *testing.T) {
	if (Status{Head: "main"}).Dirty() {
		t.Error("a status with no changes must not be dirty")
	}
	for _, s := range []Status{{Staged: 1}, {Modified: 1}, {Untracked: 1}, {Conflicted: 1}} {
		if !s.Dirty() {
			t.Errorf("%+v should be dirty", s)
		}
	}
	if got := (Status{Staged: 1, Modified: 2, Untracked: 3, Conflicted: 4}).Total(); got != 10 {
		t.Errorf("Total() = %d, want 10", got)
	}
}

func TestParseWorktreeList(t *testing.T) {
	in := "worktree /Users/x/repo\n" +
		"HEAD 36fa0fbe9b48\n" +
		"branch refs/heads/main\n" +
		"\n" +
		"worktree /Users/x/repo-wt\n" +
		"HEAD abc1234def56\n" +
		"detached\n" +
		"\n"
	got := parseWorktreeList([]byte(in))
	if len(got) != 2 {
		t.Fatalf("got %d worktrees, want 2", len(got))
	}
	if got[0].Dir != "/Users/x/repo" || got[0].Branch != "main" || got[0].Detached {
		t.Errorf("worktree 0 = %+v", got[0])
	}
	if got[1].Dir != "/Users/x/repo-wt" || !got[1].Detached || got[1].Branch != "" {
		t.Errorf("worktree 1 = %+v", got[1])
	}
}

func TestSyncStateString(t *testing.T) {
	// Guards the exhaustive switch: a new state added without a label here
	// would silently render as "invalid".
	for s := SyncUnknown; s <= SyncDiverged; s++ {
		if got := s.String(); got == "invalid" || got == "" {
			t.Errorf("SyncState(%d).String() = %q", s, got)
		}
	}
}
