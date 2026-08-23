package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Scan reads everything gittree displays about one repository.
//
// It costs three git processes for the ordinary case -- rev-parse,
// for-each-ref and status -- plus one status per additional linked worktree
// and one worktree list only when a detached worktree might exist.
func (c Client) Scan(ctx context.Context, dir string) *Repo {
	r := &Repo{Dir: dir}

	if err := c.scanDirs(ctx, r); err != nil {
		r.Err = err
		return r
	}
	c.scanBranches(ctx, r)
	c.scanPrimaryStatus(ctx, r)
	c.scanLinkedWorktrees(ctx, r)
	c.scanRemotes(ctx, r)
	r.LastFetch = lastFetch(r.CommonDir)
	r.Stashes = stashCount(r.CommonDir)

	return r
}

// scanDirs resolves the three directories in one invocation. --git-common-dir
// is what makes FETCH_HEAD and the stash reflog resolvable from inside a
// linked worktree, where --git-dir points at a per-worktree subdirectory.
func (c Client) scanDirs(ctx context.Context, r *Repo) error {
	out, err := c.run(ctx, r.Dir, "rev-parse", "--show-toplevel", "--git-dir", "--git-common-dir")
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		return &CommandError{Args: []string{"rev-parse"}, Err: os.ErrInvalid}
	}
	r.Dir = lines[0]
	r.GitDir = abs(r.Dir, lines[1])
	r.CommonDir = abs(r.Dir, lines[2])
	return nil
}

// abs resolves a possibly-relative path that git reported against the working
// tree root. rev-parse prints ".git" rather than an absolute path when run
// from the top of a repository.
func abs(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

func (c Client) scanBranches(ctx context.Context, r *Repo) {
	out, err := c.run(ctx, r.Dir, "for-each-ref", "--format="+forEachRefFormat, "refs/heads/")
	if err != nil {
		r.Err = err
		return
	}
	r.Branches = parseForEachRef(out)
}

// scanPrimaryStatus attaches the repository's own working tree dirt, and hangs
// it off the checked-out branch too.
//
// Dirt belongs to a worktree, never to a branch: only the branch that is
// actually checked out somewhere can carry it.
func (c Client) scanPrimaryStatus(ctx context.Context, r *Repo) {
	st, err := c.status(ctx, r.Dir)
	if err != nil {
		if r.Err == nil {
			r.Err = err
		}
		return
	}
	r.Status = st
	if head := r.Head(); head != nil {
		s := st
		head.Status = &s
	}
}

// scanLinkedWorktrees gives every branch checked out in a *linked* worktree
// its own status, since several branches can be dirty at once.
//
// %(worktreepath) already told us where each branch is checked out, so the
// worktree list is only consulted when nothing reports a path -- the case of a
// detached worktree, which has no branch to hang the path off.
func (c Client) scanLinkedWorktrees(ctx context.Context, r *Repo) {
	for i := range r.Branches {
		br := &r.Branches[i]
		if br.Worktree == "" || br.Status != nil || sameDir(br.Worktree, r.Dir) {
			continue
		}
		st, err := c.status(ctx, br.Worktree)
		if err != nil {
			continue // a worktree on an unmounted volume must not fail the scan
		}
		br.Status = &st
	}
}

func sameDir(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func (c Client) status(ctx context.Context, dir string) (Status, error) {
	out, err := c.run(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, err
	}
	return parseStatusV2(out), nil
}

// scanRemotes counts configured remotes, so a repository with none can be
// skipped by the fetch pool instead of spawning a process that cannot work.
func (c Client) scanRemotes(ctx context.Context, r *Repo) {
	out, err := c.run(ctx, r.Dir, "remote")
	if err != nil {
		return
	}
	for line := range strings.Lines(strings.TrimSpace(string(out))) {
		if strings.TrimSpace(line) != "" {
			r.Remotes++
		}
	}
}

// lastFetch reports when the repository was last fetched. A missing FETCH_HEAD
// means it never has been, which is the common case in practice and is
// rendered as such rather than as an error.
func lastFetch(commonDir string) time.Time {
	fi, err := os.Stat(filepath.Join(commonDir, "FETCH_HEAD"))
	if err != nil {
		return time.Time{}
	}
	return fi.ModTime()
}

// stashCount reads the stash reflog directly. Best-effort and free: no process.
func stashCount(commonDir string) int {
	// #nosec G304 -- commonDir is what git itself reported for
	// --git-common-dir, and the rest of the path is a constant. There is no
	// caller-supplied component to traverse with.
	b, err := os.ReadFile(filepath.Join(commonDir, "logs", "refs", "stash"))
	if err != nil {
		return 0
	}
	n := 0
	for line := range strings.Lines(string(b)) {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
