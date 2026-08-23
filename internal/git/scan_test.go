package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitEnv isolates a test repository from the developer's own git setup.
//
// Without this, a global gitconfig with commit signing, hooks or templates
// makes these tests fail on one machine and pass on another.
func gitEnv(home string) []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME="+home,
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
}

// run executes a git command in dir and fails the test if it does not succeed.
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// requireGit skips the test when there is no git to drive.
func requireGit(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// newRepo creates a repository with one commit on main.
func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "a.txt"), "one\n")
	run(t, dir, "add", "a.txt")
	run(t, dir, "commit", "-qm", "first")
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanCleanRepo(t *testing.T) {
	requireGit(t)
	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))

	r := Client{}.Scan(context.Background(), dir)
	if r.Err != nil {
		t.Fatalf("scan: %v", r.Err)
	}
	if len(r.Branches) != 1 || r.Branches[0].Name != "main" {
		t.Fatalf("branches = %+v, want just main", r.Branches)
	}
	head := r.Head()
	if head == nil {
		t.Fatal("no HEAD branch")
	}
	// A brand new repository tracks nothing, which is the common real-world
	// case and must not be reported as an error or as "gone".
	if head.Sync != SyncNoUpstream {
		t.Errorf("sync = %v, want no-upstream", head.Sync)
	}
	if r.Dirty() {
		t.Errorf("fresh repo reported dirty: %+v", r.Status)
	}
	if !r.LastFetch.IsZero() {
		t.Errorf("LastFetch = %v, want zero for a never-fetched repo", r.LastFetch)
	}
}

func TestScanDirty(t *testing.T) {
	requireGit(t)
	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))
	write(t, filepath.Join(dir, "a.txt"), "changed\n") // modified
	write(t, filepath.Join(dir, "new.txt"), "new\n")   // untracked
	write(t, filepath.Join(dir, "staged.txt"), "s\n")  // staged
	run(t, dir, "add", "staged.txt")

	r := Client{}.Scan(context.Background(), dir)
	if r.Err != nil {
		t.Fatalf("scan: %v", r.Err)
	}
	want := Status{Head: "main", Staged: 1, Modified: 1, Untracked: 1, Files: 3}
	if r.Status != want {
		t.Errorf("status = %+v, want %+v", r.Status, want)
	}
	// The dirt must also hang off the checked-out branch, and only that one.
	head := r.Head()
	if head.Status == nil || head.Status.Files != 3 {
		t.Errorf("head branch status = %+v, want 3 files", head.Status)
	}
}

// TestScanSyncStates drives a real remote so ahead, behind, diverged and gone
// are exercised against git itself rather than against captured strings.
func TestScanSyncStates(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	// -b main is not decoration. Without it the bare repository's HEAD follows
	// whatever init.defaultBranch the git binary was built with -- "main" on
	// Apple git, "master" on the upstream build CI runs -- and a clone of a
	// repository whose HEAD names a ref that does not exist lands on an unborn
	// branch. The push below then goes to master, origin/main never moves, and
	// the divergence this test is about never happens.
	run(t, base, "init", "-q", "-b", "main", "--bare", remote)

	local := newRepo(t, filepath.Join(base, "local"))
	run(t, local, "remote", "add", "origin", remote)
	run(t, local, "push", "-q", "-u", "origin", "main")

	head := func() *Branch {
		t.Helper()
		r := Client{}.Scan(context.Background(), local)
		if r.Err != nil {
			t.Fatalf("scan: %v", r.Err)
		}
		b := r.Head()
		if b == nil {
			t.Fatal("no HEAD branch")
		}
		return b
	}

	if b := head(); b.Sync != SyncInSync {
		t.Errorf("after push: sync = %v, want in-sync", b.Sync)
	}

	write(t, filepath.Join(local, "b.txt"), "b\n")
	run(t, local, "add", "b.txt")
	run(t, local, "commit", "-qm", "second")
	if b := head(); b.Sync != SyncAhead || b.Ahead != 1 {
		t.Errorf("after local commit: %v ahead=%d, want ahead=1", b.Sync, b.Ahead)
	}

	// A second clone pushes, so the first is now behind as well as ahead.
	other := filepath.Join(base, "other")
	run(t, base, "clone", "-q", remote, other)
	write(t, filepath.Join(other, "c.txt"), "c\n")
	run(t, other, "add", "c.txt")
	run(t, other, "commit", "-qm", "third")
	run(t, other, "push", "-q")

	run(t, local, "fetch", "-q")
	if b := head(); b.Sync != SyncDiverged || b.Ahead != 1 || b.Behind != 1 {
		t.Errorf("after remote commit: %v ahead=%d behind=%d, want diverged 1/1", b.Sync, b.Ahead, b.Behind)
	}
	// FETCH_HEAD now exists, so the repository is no longer "never fetched".
	if r := (Client{}).Scan(context.Background(), local); r.LastFetch.IsZero() {
		t.Error("LastFetch still zero after a fetch")
	}

	// Deleting the upstream must surface as gone, not as in-sync. Detecting it
	// is the whole reason the background fetch prunes.
	run(t, local, "checkout", "-q", "-b", "feature")
	run(t, local, "push", "-q", "-u", "origin", "feature")
	run(t, other, "push", "-q", "origin", "--delete", "feature")
	run(t, local, "fetch", "-q", "--prune")

	r := Client{}.Scan(context.Background(), local)
	var feature *Branch
	for i := range r.Branches {
		if r.Branches[i].Name == "feature" {
			feature = &r.Branches[i]
		}
	}
	if feature == nil {
		t.Fatal("feature branch missing")
	}
	if feature.Sync != SyncGone {
		t.Errorf("deleted upstream: sync = %v, want gone", feature.Sync)
	}
}

// TestScanLinkedWorktree checks the rule that dirt belongs to a worktree, not
// to a branch: two branches are checked out at once and only one is dirty.
func TestScanLinkedWorktree(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	main := newRepo(t, filepath.Join(base, "repo"))
	wt := filepath.Join(base, "wt")
	run(t, main, "branch", "side")
	run(t, main, "worktree", "add", "-q", wt, "side")

	write(t, filepath.Join(wt, "dirty.txt"), "x\n")

	r := Client{}.Scan(context.Background(), main)
	if r.Err != nil {
		t.Fatalf("scan: %v", r.Err)
	}
	if r.Dirty() {
		t.Errorf("primary worktree should be clean, got %+v", r.Status)
	}

	byName := map[string]*Branch{}
	for i := range r.Branches {
		byName[r.Branches[i].Name] = &r.Branches[i]
	}
	side := byName["side"]
	if side == nil {
		t.Fatal("side branch missing")
	}
	if side.Worktree == "" {
		t.Error("side branch should report the worktree it is checked out in")
	}
	if side.Status == nil || side.Status.Untracked != 1 {
		t.Errorf("side branch status = %+v, want 1 untracked", side.Status)
	}
	// The branch that is not checked out anywhere carries no status at all.
	if b := byName["main"]; b != nil && b.Status != nil && b.Status.Dirty() {
		t.Errorf("main should not be dirty, got %+v", b.Status)
	}
}

func TestScanUnbornBranch(t *testing.T) {
	requireGit(t)
	dir := filepath.Join(t.TempDir(), "fresh")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")

	r := Client{}.Scan(context.Background(), dir)
	if r.Err != nil {
		t.Fatalf("scan: %v", r.Err)
	}
	if len(r.Branches) != 0 {
		t.Errorf("branches = %+v, want none before the first commit", r.Branches)
	}
	if !r.Status.Unborn {
		t.Error("a repository with no commits must be reported as unborn, not detached")
	}
	if r.Status.Head != "main" {
		t.Errorf("head = %q, want main", r.Status.Head)
	}
}

func TestScanNotARepo(t *testing.T) {
	requireGit(t)
	r := Client{}.Scan(context.Background(), t.TempDir())
	if r.Err == nil {
		t.Fatal("scanning a non-repository should report an error")
	}
}
