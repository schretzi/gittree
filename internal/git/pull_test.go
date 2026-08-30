package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestPullArgs(t *testing.T) {
	args := pullArgs("/repo")
	for _, want := range []string{"-C", "/repo", "pull", "--quiet"} {
		if !slices.Contains(args, want) {
			t.Errorf("args %v missing %q", args, want)
		}
	}
}

func TestPullFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "uncommitted changes conflict",
			err:  &CommandError{Stderr: "error: Your local changes to the following files would be overwritten by merge:\n\tfile.txt\nPlease commit your changes or stash them before you merge.\nAborting\n"},
			want: "uncommitted changes",
		},
		{
			name: "cannot fast forward",
			err:  &CommandError{Stderr: "fatal: Not possible to fast-forward, aborting.\n"},
			want: "cannot fast-forward",
		},
		{
			name: "no tracking information",
			err:  &CommandError{Stderr: "fatal: There is no tracking information for the current branch.\nPlease specify which branch you want to merge with.\n"},
			want: "no upstream branch",
		},
		{
			name: "merge conflict",
			err:  &CommandError{Stderr: "CONFLICT (content): Merge conflict in file.txt\nAutomatic merge failed; fix conflicts and then commit the result.\n"},
			want: "merge conflict",
		},
		{
			name: "ssh failure falls back to fetch failure mappings",
			err:  &CommandError{Stderr: "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.\n"},
			want: "ssh key rejected",
		},
		{
			name: "timeout",
			err:  &CommandError{Stderr: "", Err: context.DeadlineExceeded},
			want: "timed out",
		},
		{
			name: "nil error",
			err:  nil,
			want: "",
		},
		{
			name: "unknown failure falls back to first complaint",
			err:  &CommandError{Stderr: "fatal: some unexpected git pull problem.\n"},
			want: "some unexpected git pull problem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PullFailure(tt.err); got != tt.want {
				t.Errorf("PullFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPullSuccess(t *testing.T) {
	requireGit(t)
	base := t.TempDir()

	// Upstream bare repository
	upstream := filepath.Join(base, "upstream.git")
	run(t, base, "init", "--bare", "--initial-branch=main", upstream)

	// Work repo to push initial commit to upstream
	work := newRepo(t, filepath.Join(base, "work"))
	run(t, work, "remote", "add", "origin", upstream)
	run(t, work, "push", "-u", "origin", "main")

	// Clone repo that will be pulled
	clone := filepath.Join(base, "clone")
	run(t, base, "clone", upstream, clone)

	// Add new commit in work and push
	if err := os.WriteFile(filepath.Join(work, "new.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	run(t, work, "add", "new.txt")
	run(t, work, "commit", "-m", "add new.txt")
	run(t, work, "push", "origin", "main")

	// Pull in clone using Client.Pull
	c := Client{}
	if err := c.Pull(context.Background(), clone, PullOptions{Timeout: 5 * time.Second}); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	// Verify new file exists in clone
	if _, err := os.Stat(filepath.Join(clone, "new.txt")); err != nil {
		t.Errorf("new.txt not found in clone after pull: %v", err)
	}
}

func TestPullTimesOutOnUnreachableHost(t *testing.T) {
	requireGit(t)

	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))
	// 203.0.113.0/24 is TEST-NET-3, guaranteed not routable.
	run(t, dir, "remote", "add", "origin", "ssh://git@203.0.113.1/repo.git")
	run(t, dir, "config", "branch.main.remote", "origin")
	run(t, dir, "config", "branch.main.merge", "refs/heads/main")

	err := Client{}.Pull(context.Background(), dir, PullOptions{Timeout: 2 * time.Second})
	if err == nil {
		t.Fatal("pulling an unreachable host should fail")
	}
	if _, ok := errors.AsType[*CommandError](err); !ok {
		t.Errorf("error %v is not a *CommandError", err)
	}
}
