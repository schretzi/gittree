package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/schretzi/gittree/internal/git"
	"github.com/schretzi/gittree/internal/tree"
)

// The UI is driven entirely through Update, which is a pure function of
// (model, message). None of these tests start a Program, open a terminal or
// run git: the command factories are stubs.

func press(s string) tea.KeyPressMsg {
	if len(s) == 1 {
		return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
	}
	// Named keys such as "enter" carry a key code rather than text.
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}
	panic("unknown key " + s)
}

// send applies messages in order and returns the resulting model.
func send(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		got, ok := next.(Model)
		if !ok {
			t.Fatalf("Update returned %T, want tui.Model", next)
		}
		m = got
	}
	return m
}

// sendCmd applies one message and returns the model and the command it asked
// for, so tests can assert on side effects without running them.
func sendCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want tui.Model", next)
	}
	return got, cmd
}

// repo builds a scanned repository with one branch in the given state.
func repo(dir, branch string, sync git.SyncState, dirty int) *git.Repo {
	st := git.Status{Head: branch, Untracked: dirty, Files: dirty}
	r := &git.Repo{
		Dir:    dir,
		Status: st,
		Branches: []git.Branch{{
			Name: branch, Head: true, Sync: sync, Worktree: dir,
			Upstream: "origin/" + branch, Status: &st,
		}},
	}
	if sync == git.SyncNoUpstream {
		r.Branches[0].Upstream = ""
	} else {
		r.Remotes = 1
	}
	return r
}

// newTestModel returns a model already sized, with stub commands that record
// what was asked of them.
func newTestModel(t *testing.T, root string) (Model, *stubs) {
	t.Helper()
	s := &stubs{}
	m := New(Config{
		Root:  root,
		ASCII: true, // pin the glyphs so assertions do not depend on the locale
		Cmds: Commands{
			Discover: func() tea.Cmd { s.discovers++; return nil },
			ScanAll:  func(dirs []string) tea.Cmd { s.scanAll = append(s.scanAll, dirs); return nil },
			ScanOne:  func(dir string) tea.Cmd { s.scanOne = append(s.scanOne, dir); return nil },
			Launch:   func(dir string) tea.Cmd { s.launched = append(s.launched, dir); return nil },
			Fetch:    func(dirs []string) tea.Cmd { s.fetched = append(s.fetched, dirs); return nil },
			Pull: func(dir, repoPath, name string) tea.Cmd {
				s.pulled = append(s.pulled, pullCall{dir: dir, repoPath: repoPath, name: name})
				return nil
			},
		},
	})
	return send(t, m, tea.WindowSizeMsg{Width: 100, Height: 20}), s
}

type pullCall struct {
	dir      string
	repoPath string
	name     string
}

type stubs struct {
	discovers int
	scanAll   [][]string
	scanOne   []string
	launched  []string
	fetched   [][]string
	pulled    []pullCall
}

// frame renders the view and strips styling, so assertions read as plain text.
func frame(m Model) string {
	return stripANSI(m.View().Content)
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' && s[i] != 'K' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func TestInitStartsDiscovery(t *testing.T) {
	s := &stubs{}
	m := New(Config{Root: "/w", Cmds: Commands{Discover: func() tea.Cmd { s.discovers++; return nil }}})
	m.Init()
	if s.discovers != 1 {
		t.Errorf("Init triggered %d discoveries, want 1", s.discovers)
	}
}

func TestDiscoveryTriggersScan(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a", "/w/b"}})
	if len(s.scanAll) != 1 || len(s.scanAll[0]) != 2 {
		t.Fatalf("scanAll calls = %v, want one call with two dirs", s.scanAll)
	}
	if !strings.Contains(frame(m), "scanning 2 repositories") {
		t.Errorf("status not shown:\n%s", frame(m))
	}
}

func TestEmptyDiscoveryReportsNothingFound(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: nil})
	if len(s.scanAll) != 0 {
		t.Error("nothing was found, so nothing should be scanned")
	}
	if !strings.Contains(frame(m), "no git repositories found") {
		t.Errorf("frame:\n%s", frame(m))
	}
}

func TestRendersRepoRows(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/alpha", "/w/beta"}},
		scannedMsg{repos: []*git.Repo{
			repo("/w/alpha", "main", git.SyncInSync, 0),
			repo("/w/beta", "main", git.SyncNoUpstream, 3),
		}},
	)
	out := frame(m)
	for _, want := range []string{"alpha", "beta", "main", "clean", "?3", "never"} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing %q:\n%s", want, out)
		}
	}
	// The header carries the root and the count.
	if !strings.Contains(out, "2 repos") {
		t.Errorf("header missing the count:\n%s", out)
	}
}

func TestNoUpstreamRendersAsAStateNotAnError(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/solo"}},
		scannedMsg{repos: []*git.Repo{repo("/w/solo", "main", git.SyncNoUpstream, 0)}},
	)
	out := frame(m)
	if strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("a repository with no upstream must not read as an error:\n%s", out)
	}
	// The ASCII glyph set renders "no upstream" as a dash.
	if !strings.Contains(out, "main") {
		t.Errorf("frame:\n%s", out)
	}
}

func TestCursorMovesAndStaysInRange(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a", "/w/b", "/w/c"}})

	if m.cursor != 0 {
		t.Fatalf("cursor starts at %d, want 0", m.cursor)
	}
	m = send(t, m, press("j"), press("j"))
	if m.cursor != 2 {
		t.Errorf("after two downs cursor = %d, want 2", m.cursor)
	}
	// Moving past the end must clamp rather than wrap or panic.
	m = send(t, m, press("j"), press("j"), press("j"))
	if m.cursor != 2 {
		t.Errorf("cursor ran past the last row: %d", m.cursor)
	}
	m = send(t, m, press("k"), press("k"), press("k"), press("k"))
	if m.cursor != 0 {
		t.Errorf("cursor ran past the first row: %d", m.cursor)
	}
}

func TestEnterOnRepoLaunchesToolAndOnDirToggles(t *testing.T) {
	m, s := newTestModel(t, "/w")
	// Two repositories under one directory, so the tree keeps a directory row.
	m = send(t, m, discoveredMsg{dirs: []string{"/w/p/a", "/w/p/b"}})

	// Row 0 is the directory "p". Enter must expand or collapse it, not launch.
	if got := m.rows[0].Kind; got != tree.RowDir {
		t.Fatalf("row 0 kind = %v, want a directory", got)
	}
	before := len(m.rows)
	m = send(t, m, press("enter"))
	if len(s.launched) != 0 {
		t.Errorf("enter on a directory launched the tool: %v", s.launched)
	}
	if len(m.rows) == before {
		t.Error("enter on a directory did not change the visible rows")
	}

	// Move onto a repository row; now enter must launch.
	m = send(t, m, press("enter")) // re-expand
	m = send(t, m, press("j"))
	if m.rows[m.cursor].Kind != tree.RowRepo {
		t.Fatalf("expected a repository row, got %v", m.rows[m.cursor].Kind)
	}
	m = send(t, m, press("enter"))
	if len(s.launched) != 1 || s.launched[0] != "/w/p/a" {
		t.Errorf("launched = %v, want [/w/p/a]", s.launched)
	}
}

func TestToolExitRescansThatRepo(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a"}})
	send(t, m, ToolExitedMsg{Dir: "/w/a"})
	if len(s.scanOne) != 1 || s.scanOne[0] != "/w/a" {
		t.Errorf("scanOne = %v, want a rescan of /w/a after the tool exits", s.scanOne)
	}
}

func TestRescanKeyRescansSelected(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a"}})
	send(t, m, press("r"))
	if len(s.scanOne) != 1 || s.scanOne[0] != "/w/a" {
		t.Errorf("scanOne = %v, want [/w/a]", s.scanOne)
	}
}

func TestRescanAllKeyRescansAll(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a", "/w/b"}})
	before := s.discovers
	m = send(t, m, press("R"))
	if s.discovers != before+1 {
		t.Errorf("discovers = %d, want %d", s.discovers, before+1)
	}
	if !strings.Contains(frame(m), "rescanning") {
		t.Errorf("status not shown:\n%s", frame(m))
	}
}

func TestPullKeyPullsSelected(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncBehind, 0)}},
	)
	m = send(t, m, press("p"))
	if len(s.pulled) != 1 || s.pulled[0].dir != "/w/a" {
		t.Errorf("pulled = %+v, want [/w/a]", s.pulled)
	}
	if !strings.Contains(frame(m), "pulling a") {
		t.Errorf("status not shown:\n%s", frame(m))
	}
}

func TestPullOnDirectoryDoesNothing(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/p/a", "/w/p/b"}})
	send(t, m, press("p"))
	if len(s.pulled) != 0 {
		t.Errorf("pull on directory ran pull: %+v", s.pulled)
	}
}

func TestPullOnBranchInWorktreeUsesWorktreeDir(t *testing.T) {
	m, s := newTestModel(t, "/w")
	st := git.Status{Head: "feature", Untracked: 0}
	r := &git.Repo{
		Dir:     "/w/a",
		Status:  st,
		Remotes: 1,
		Branches: []git.Branch{
			{Name: "main", Head: false, Sync: git.SyncInSync, Worktree: "/w/a"},
			{Name: "feature", Head: true, Sync: git.SyncBehind, Worktree: "/w/worktrees/feature", Status: &st},
		},
	}
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{r}},
	)
	// Open repository to reveal branches, move down onto feature branch
	m = send(t, m, press("right"), press("j"), press("j"))
	send(t, m, press("p"))
	if len(s.pulled) != 1 || s.pulled[0].dir != "/w/worktrees/feature" || s.pulled[0].repoPath != "/w/a" {
		t.Errorf("pulled = %+v, want dir=/w/worktrees/feature repoPath=/w/a", s.pulled)
	}
}

func TestPullKeyOnRepoWithNoRemoteSaysSo(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/solo"}},
		scannedMsg{repos: []*git.Repo{repo("/w/solo", "main", git.SyncNoUpstream, 0)}},
	)
	m = send(t, m, press("p"))
	if len(s.pulled) != 0 {
		t.Errorf("pull should not run on repo with no remote: %+v", s.pulled)
	}
	if !strings.Contains(frame(m), "no remote to pull from") {
		t.Errorf("frame should explain why nothing happened:\n%s", frame(m))
	}
}

func TestPullSuccessUpdatesStatusAndTriggersRescan(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncBehind, 0)}},
	)
	before := len(s.scanOne)
	m = send(t, m, pullResultMsg{Dir: "/w/a", RepoPath: "/w/a", Name: "a", Err: nil})
	if len(s.scanOne) != before+1 || s.scanOne[len(s.scanOne)-1] != "/w/a" {
		t.Errorf("scanOne = %v, want a rescan of /w/a after successful pull", s.scanOne)
	}
	if !strings.Contains(frame(m), "pulled a") {
		t.Errorf("frame should show pulled status:\n%s", frame(m))
	}
}

func TestPullFailureUpdatesStatusAndTriggersRescan(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncBehind, 0)}},
	)
	before := len(s.scanOne)
	m = send(t, m, pullResultMsg{
		Dir:      "/w/a",
		RepoPath: "/w/a",
		Name:     "a",
		Err:      &git.CommandError{Stderr: "error: Your local changes to the following files would be overwritten by merge:\n"},
	})
	if len(s.scanOne) != before+1 || s.scanOne[len(s.scanOne)-1] != "/w/a" {
		t.Errorf("scanOne = %v, want a rescan of /w/a even after failed pull", s.scanOne)
	}
	if !strings.Contains(frame(m), "pull failed (a): uncommitted changes") {
		t.Errorf("frame should show pull failed status:\n%s", frame(m))
	}
}

func TestDirtyOnlyFilter(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/clean", "/w/messy"}},
		scannedMsg{repos: []*git.Repo{
			repo("/w/clean", "main", git.SyncInSync, 0),
			repo("/w/messy", "main", git.SyncInSync, 2),
		}},
	)
	if !strings.Contains(frame(m), "clean") {
		t.Fatal("the clean repository should be visible to start with")
	}
	m = send(t, m, press("d"))
	out := frame(m)
	if strings.Contains(out, "/w/clean") || countRows(m, "clean") > 0 {
		t.Errorf("dirty-only still shows the clean repository:\n%s", out)
	}
	if countRows(m, "messy") != 1 {
		t.Errorf("dirty-only dropped the dirty repository:\n%s", out)
	}
	// Toggling back restores it.
	m = send(t, m, press("d"))
	if countRows(m, "clean") != 1 {
		t.Error("toggling the filter off did not restore the clean repository")
	}
}

func countRows(m Model, name string) int {
	n := 0
	for _, r := range m.rows {
		if r.Node.Name == name {
			n++
		}
	}
	return n
}

func TestQuitKey(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	_, cmd := sendCmd(t, m, press("q"))
	if cmd == nil {
		t.Fatal("q produced no command, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("q produced %T, want tea.QuitMsg", cmd())
	}
}

func TestHelpOverlaySwallowsKeys(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a"}})
	m = send(t, m, press("?"))
	if !strings.Contains(frame(m), "keys") {
		t.Errorf("help overlay not shown:\n%s", frame(m))
	}
	// A key that would otherwise act must not act while help is up.
	m = send(t, m, press("r"))
	if len(s.scanOne) != 0 {
		t.Error("a keypress leaked through the help overlay")
	}
	m = send(t, m, press("?"))
	if strings.Contains(frame(m), "keys —") {
		t.Error("help overlay did not close")
	}
}

func TestTinyTerminal(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m, tea.WindowSizeMsg{Width: 10, Height: 3})
	if !strings.Contains(frame(m), "too small") {
		t.Errorf("frame = %q, want a too-small notice", frame(m))
	}
}

func TestNarrowTerminalDropsColumnsRatherThanOverflowing(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		tea.WindowSizeMsg{Width: 40, Height: 20},
		discoveredMsg{dirs: []string{"/w/some-repository-with-a-long-name"}},
		scannedMsg{repos: []*git.Repo{repo("/w/some-repository-with-a-long-name", "main", git.SyncInSync, 0)}},
	)
	for line := range strings.SplitSeq(frame(m), "\n") {
		if w := len([]rune(line)); w > 40 {
			t.Errorf("line is %d cells wide, want at most 40: %q", w, line)
		}
	}
}

func TestViewUsesAltScreen(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	if !m.View().AltScreen {
		t.Error("the view must request the alternate screen")
	}
}

func TestScanResultsUpdateRows(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a"}})
	// Before the scan lands the row exists but has nothing to say yet.
	if strings.Contains(frame(m), "in-sync") {
		t.Error("sync state shown before the scan completed")
	}
	m = send(t, m, repoScannedMsg{repo: repo("/w/a", "trunk", git.SyncAhead, 0)})
	out := frame(m)
	if !strings.Contains(out, "trunk") {
		t.Errorf("frame did not pick up the scan result:\n%s", out)
	}
}

func TestFetchAgeRendering(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	r := repo("/w/a", "main", git.SyncInSync, 0)
	r.LastFetch = time.Now().Add(-3 * time.Hour)
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a"}}, scannedMsg{repos: []*git.Repo{r}})
	if !strings.Contains(frame(m), "3h") {
		t.Errorf("frame missing the fetch age:\n%s", frame(m))
	}
}

func TestFetchStartsAfterScanAndSkipsReposWithNoRemote(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/tracked", "/w/solo"}},
		scannedMsg{repos: []*git.Repo{
			repo("/w/tracked", "main", git.SyncInSync, 0),  // has a remote
			repo("/w/solo", "main", git.SyncNoUpstream, 0), // has none
		}},
	)
	if len(s.fetched) != 1 {
		t.Fatalf("fetch calls = %v, want exactly one", s.fetched)
	}
	// Spawning git for a repository with no remote is pure cost, and four of
	// the ten repositories this was built against are in that state.
	if got := s.fetched[0]; len(got) != 1 || got[0] != "/w/tracked" {
		t.Errorf("fetched %v, want only the repository that has a remote", got)
	}
	if !strings.Contains(frame(m), "fetching 0/1") {
		t.Errorf("fetch progress not shown:\n%s", frame(m))
	}
}

func TestNoFetchWhenDisabled(t *testing.T) {
	s := &stubs{}
	m := New(Config{
		Root: "/w", ASCII: true,
		Cmds: Commands{
			Discover: func() tea.Cmd { return nil },
			ScanAll:  func([]string) tea.Cmd { return nil },
			ScanOne:  func(string) tea.Cmd { return nil },
			// Fetch left nil, which is what --no-fetch produces.
		},
	})
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: 20})
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncInSync, 0)}},
	)
	if len(s.fetched) != 0 {
		t.Error("fetching happened even though it was switched off")
	}
	if strings.Contains(frame(m), "fetching") {
		t.Errorf("fetch progress shown with fetching disabled:\n%s", frame(m))
	}
}

func TestSuccessfulFetchTriggersRescan(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncInSync, 0)}},
	)
	before := len(s.scanOne)
	// A fetch moves the remote-tracking refs, so ahead/behind is stale until
	// the repository is read again.
	send(t, m, fetchResultMsg{result: git.FetchResult{Dir: "/w/a"}})
	if len(s.scanOne) != before+1 || s.scanOne[len(s.scanOne)-1] != "/w/a" {
		t.Errorf("scanOne = %v, want a rescan of /w/a after a successful fetch", s.scanOne)
	}
}

func TestFailedFetchIsMarkedOnTheRowNotAnnounced(t *testing.T) {
	m, s := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncInSync, 0)}},
	)
	before := len(s.scanOne)
	m = send(t, m, fetchResultMsg{result: git.FetchResult{Dir: "/w/a", Err: errFake}})

	if len(s.scanOne) != before {
		t.Error("a failed fetch should not trigger a rescan; nothing changed")
	}
	if _, ok := m.fetchErrs["/w/a"]; !ok {
		t.Error("the failure was not recorded against the repository")
	}
	// The row has to say what happened, not just flag that something did: a
	// marker on its own is unreadable without a legend somewhere else.
	if !strings.Contains(frame(m), "(fetch failed: connection refused)") {
		t.Errorf("the row does not say why the fetch failed:\n%s", frame(m))
	}

	m = send(t, m, fetchFinishedMsg{})
	if !strings.Contains(frame(m), "1 repository could not be fetched") {
		t.Errorf("summary not shown:\n%s", frame(m))
	}
}

func TestFetchKeyOnRepoWithNoRemoteSaysSo(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/solo"}},
		scannedMsg{repos: []*git.Repo{repo("/w/solo", "main", git.SyncNoUpstream, 0)}},
	)
	m = send(t, m, press("f"))
	if !strings.Contains(frame(m), "no remote") {
		t.Errorf("frame should explain why nothing happened:\n%s", frame(m))
	}
}

var errFake = errors.New("dial tcp: connection refused")

// bigTree returns a model holding n repositories, which is what exercises the
// window rather than the whole list.
func bigTree(t *testing.T, n, height int) Model {
	t.Helper()
	m, _ := newTestModel(t, "/w")
	dirs := make([]string, n)
	for i := range dirs {
		dirs[i] = "/w/repo" + itoa(i)
	}
	m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: height})
	return send(t, m, discoveredMsg{dirs: dirs})
}

func TestScrollingShowsOnlyAWindow(t *testing.T) {
	const height = 20
	m := bigTree(t, 200, height)

	lines := strings.Count(frame(m), "\n") + 1
	if lines > height {
		t.Errorf("rendered %d lines into a %d-row terminal", lines, height)
	}
	if len(m.rows) != 200 {
		t.Fatalf("got %d rows, want 200", len(m.rows))
	}
	if m.top != 0 {
		t.Errorf("initial scroll offset = %d, want 0", m.top)
	}
}

func TestScrollFollowsCursorDown(t *testing.T) {
	m := bigTree(t, 200, 20)
	for range 100 {
		m = send(t, m, press("j"))
	}
	if m.cursor != 100 {
		t.Fatalf("cursor = %d, want 100", m.cursor)
	}
	// The cursor must be inside the window, or it is off screen.
	if m.cursor < m.top || m.cursor >= m.top+m.visibleRows() {
		t.Errorf("cursor %d is outside the window [%d,%d)", m.cursor, m.top, m.top+m.visibleRows())
	}
}

func TestJumpToBottomAndBack(t *testing.T) {
	m := bigTree(t, 200, 20)
	m = send(t, m, press("G"))
	if m.cursor != len(m.rows)-1 {
		t.Errorf("G left the cursor at %d, want %d", m.cursor, len(m.rows)-1)
	}
	if m.cursor >= m.top+m.visibleRows() {
		t.Errorf("last row is off screen: cursor %d, window [%d,%d)", m.cursor, m.top, m.top+m.visibleRows())
	}
	m = send(t, m, press("g"))
	if m.cursor != 0 || m.top != 0 {
		t.Errorf("g left cursor=%d top=%d, want 0/0", m.cursor, m.top)
	}
}

func TestPagingStaysInRange(t *testing.T) {
	m := bigTree(t, 200, 20)
	for range 40 {
		m = send(t, m, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	}
	if m.cursor >= len(m.rows) {
		t.Errorf("paging ran off the end: cursor %d, rows %d", m.cursor, len(m.rows))
	}
	for range 40 {
		m = send(t, m, tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	}
	if m.cursor != 0 {
		t.Errorf("paging back left cursor at %d, want 0", m.cursor)
	}
}

func TestCursorStaysOnSameRepoWhenRowsAppearAbove(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m, discoveredMsg{dirs: []string{"/w/m", "/w/z"}})
	m = send(t, m, press("j")) // onto /w/z
	want := m.cursorID

	// A repository sorting before the cursor arrives. Keeping the cursor by
	// identity rather than by index is what stops it jumping to a different
	// repository under the user's hands.
	m = send(t, m, discoveredMsg{dirs: []string{"/w/a", "/w/m", "/w/z"}})
	if m.cursorID != want {
		t.Errorf("cursor moved to %q, want it to stay on %q", m.cursorID, want)
	}
	if m.rows[m.cursor].ID != want {
		t.Errorf("cursor index %d points at %q", m.cursor, m.rows[m.cursor].ID)
	}
}

func TestCollapsingKeepsCursorVisible(t *testing.T) {
	m := bigTree(t, 100, 20)
	m = send(t, m, press("G"))
	// Everything collapses, so the row the cursor was on may vanish.
	m = send(t, m, press("C"))
	if m.cursor >= len(m.rows) {
		t.Fatalf("cursor %d is past the end after collapsing (%d rows)", m.cursor, len(m.rows))
	}
	if m.top < 0 || (len(m.rows) > 0 && m.cursor < m.top) {
		t.Errorf("window is inconsistent: cursor %d, top %d, rows %d", m.cursor, m.top, len(m.rows))
	}
}

func TestRendersSomethingBeforeTheFirstSize(t *testing.T) {
	s := &stubs{}
	m := New(Config{Root: "/w", ASCII: true, Cmds: Commands{Discover: func() tea.Cmd { return nil }}})
	_ = s
	// A terminal that never reports a size must not leave a blank screen with
	// no indication that the program is alive or how to leave it.
	out := frame(m)
	if !strings.Contains(out, "q to quit") {
		t.Errorf("frame before the first WindowSizeMsg = %q", out)
	}
}

// rawLines returns the frame with its styling intact, which is the only way to
// tell a highlighted row from an ordinary one.
func rawLines(m Model) []string {
	return strings.Split(m.View().Content, "\n")
}

// highlighted reports whether a line carries a background colour. The cursor is
// the only thing that paints one.
func highlighted(line string) bool {
	return strings.Contains(line, "\x1b[") && strings.Contains(line, "48;")
}

func TestCursorRowIsHighlighted(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/alpha", "/w/beta"}},
		scannedMsg{repos: []*git.Repo{
			repo("/w/alpha", "main", git.SyncInSync, 0),
			repo("/w/beta", "main", git.SyncInSync, 0),
		}},
	)

	var lit []int
	for i, line := range rawLines(m) {
		if highlighted(line) {
			lit = append(lit, i)
		}
	}
	if len(lit) != 1 {
		t.Fatalf("%d rows are highlighted, want exactly the one under the cursor:\n%s", len(lit), m.View().Content)
	}

	// It has to follow the cursor, not sit on whichever row happens to be first.
	before := lit[0]
	m = send(t, m, press("down"))
	after := -1
	for i, line := range rawLines(m) {
		if highlighted(line) {
			after = i
		}
	}
	if after != before+1 {
		t.Errorf("highlight moved from line %d to %d, want %d", before, after, before+1)
	}
}

func TestHighlightSpansTheFullWidth(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncInSync, 0)}},
	)
	// A bar that stops at the last column reads as an artefact rather than a
	// cursor, so the row is padded to the window instead of being trimmed.
	for i, line := range rawLines(m) {
		if !highlighted(line) {
			continue
		}
		if w := len([]rune(stripANSI(line))); w != m.width {
			t.Errorf("highlighted line %d is %d cells wide, want %d", i, w, m.width)
		}
	}
}

func TestColumnsAreLabelled(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m,
		discoveredMsg{dirs: []string{"/w/a"}},
		scannedMsg{repos: []*git.Repo{repo("/w/a", "main", git.SyncInSync, 0)}},
	)
	// Whole, not truncated: this row's fetch age is "never" at 5 cells and its
	// sync is one glyph, so a layout sized from content alone would render the
	// headings as "fetc…" and "…".
	out := frame(m)
	for _, want := range []string{hdrName, hdrBranch, hdrSync, hdrUpstream, hdrDirty, hdrAge} {
		if !strings.Contains(out, want) {
			t.Errorf("frame missing the %q heading:\n%s", want, out)
		}
	}
}

func TestHelpExplainsTheColumns(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = send(t, m, tea.WindowSizeMsg{Width: 110, Height: 40}, press("?"))
	out := frame(m)
	// Every column, and the two readings that are not self-evident: a fetch
	// that has never run, and one that failed.
	for _, want := range []string{
		hdrName, hdrBranch, hdrSync, hdrUpstream, hdrDirty, hdrAge,
		"no upstream", "deleted on the remote", "never", "fetch failed",
		"? or q closes this help",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q:\n%s", want, out)
		}
	}
}

func TestHelpNeverOverflowsTheScreen(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	// The legend is longer than a short terminal. Overflowing scrolls the way
	// out of the help off the top, so the tail is dropped instead.
	for _, h := range []int{10, 20, 24, 40} {
		m = send(t, m, tea.WindowSizeMsg{Width: 100, Height: h}, press("?"))
		if n := len(rawLines(m)); n > h {
			t.Errorf("help is %d lines on a %d-line terminal", n, h)
		}
		if !strings.Contains(frame(m), "? or q closes this help") {
			t.Errorf("the way out of the help is missing at height %d:\n%s", h, frame(m))
		}
		m = send(t, m, press("?"))
	}
}

// failedFetch returns a scanned repository whose last fetch attempt failed.
func failedFetch(t *testing.T, m Model, dir, stderr string) Model {
	t.Helper()
	r := repo(dir, "main", git.SyncInSync, 0)
	r.LastFetch = time.Now().Add(-3 * 24 * time.Hour)
	m = send(t, m, discoveredMsg{dirs: []string{dir}}, scannedMsg{repos: []*git.Repo{r}})
	return send(t, m, fetchResultMsg{result: git.FetchResult{
		Dir: dir, Err: &git.CommandError{Stderr: stderr},
	}})
}

func TestFailedFetchKeepsTheAgeAndAddsTheReason(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = failedFetch(t, m, "/w/a", "git@github.com: Permission denied (publickey).\n")
	out := frame(m)
	// The age is when a fetch last worked, which is not the same question as
	// why this one did not, so both have to survive.
	if !strings.Contains(out, "3d (fetch failed: ssh key rejected)") {
		t.Errorf("frame does not carry the age and the reason together:\n%s", out)
	}
}

func TestFetchReasonShrinksRatherThanCostingAColumn(t *testing.T) {
	// The reason is the only thing on a row sized to a sentence. On a narrow
	// window it has to give its width back before any column is dropped: a
	// truncated reason still says a fetch failed, a missing column does not.
	for _, width := range []int{70, 80, 100, 120, 200} {
		m, _ := newTestModel(t, "/w")
		m = send(t, m, tea.WindowSizeMsg{Width: width, Height: 20})
		m = failedFetch(t, m, "/w/a", "fatal: unable to access 'https://x/y.git/': Could not resolve host: x\n")

		out := frame(m)
		for _, want := range []string{hdrBranch, hdrSync, hdrUpstream, hdrDirty, hdrAge} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: the %q column was dropped for the reason:\n%s", width, want, out)
			}
		}
		if !strings.Contains(out, "fetch failed") {
			t.Errorf("width %d: the failure is not reported at all:\n%s", width, out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if n := len([]rune(line)); n > width {
				t.Errorf("width %d: line is %d cells: %q", width, n, line)
			}
		}
	}
}

func TestFailedFetchGreysOutTheSyncStateItCouldNotCheck(t *testing.T) {
	m, _ := newTestModel(t, "/w")
	m = failedFetch(t, m, "/w/a", "ssh: connect to host github.com port 22: Operation timed out\n")

	// The tick is still there -- it is the best answer available -- but not in
	// the colour that claims the branch was just verified against its remote.
	var row string
	for _, line := range rawLines(m) {
		if strings.Contains(stripANSI(line), "fetch failed") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("no failed row rendered:\n%s", frame(m))
	}
	if !strings.Contains(stripANSI(row), "=") {
		t.Errorf("the sync state was dropped rather than greyed:\n%s", stripANSI(row))
	}

	m2, _ := newTestModel(t, "/w")
	r := repo("/w/a", "main", git.SyncInSync, 0)
	r.LastFetch = time.Now().Add(-3 * 24 * time.Hour)
	m2 = send(t, m2, discoveredMsg{dirs: []string{"/w/a"}}, scannedMsg{repos: []*git.Repo{r}})
	var ok string
	for _, line := range rawLines(m2) {
		if strings.Contains(stripANSI(line), "3d") {
			ok = line
		}
	}
	if ok == "" {
		t.Fatalf("no healthy row rendered:\n%s", frame(m2))
	}
	if syncColour(ok) == "" || syncColour(row) == "" {
		t.Fatalf("could not find the sync column's styling: %q / %q", row, ok)
	}
	// Same state, same text, different colour: that is the whole signal.
	if syncColour(row) == syncColour(ok) {
		t.Errorf("a sync state that could not be rechecked is painted like one that was:\n%s\n%s", row, ok)
	}
}

// syncColour returns the styling that wraps the sync column's glyph.
func syncColour(line string) string {
	for seg := range strings.SplitSeq(line, "\x1b[") {
		code, text, found := strings.Cut(seg, "m")
		if found && strings.HasPrefix(strings.TrimSpace(text), "=") {
			return code
		}
	}
	return ""
}
