// Package git runs git as a subprocess and turns its output into the values
// the rest of gittree displays. Nothing here knows about the terminal UI, so
// the whole data layer can be exercised headlessly.
//
// Every read-only invocation passes --no-optional-locks. Without it git takes
// index.lock while it works, which would make gittree fight any shell the user
// has open in the same repository.
package git

import (
	"fmt"
	"time"
)

// SyncState is where a local branch stands relative to its upstream.
//
// The distinction between SyncNoUpstream and SyncGone matters and is easy to
// collapse by accident: the first means no upstream was ever configured, the
// second means one was configured and has since been deleted from the remote.
// Only the second is a problem worth flagging.
type SyncState int

const (
	// SyncUnknown is the zero value: the repository has not been scanned yet.
	SyncUnknown SyncState = iota
	// SyncNoUpstream means the branch tracks nothing.
	SyncNoUpstream
	// SyncGone means the configured upstream no longer exists on the remote.
	SyncGone
	// SyncInSync means the branch and its upstream point at the same commit.
	SyncInSync
	// SyncAhead means the branch has commits the upstream does not.
	SyncAhead
	// SyncBehind means the upstream has commits the branch does not.
	SyncBehind
	// SyncDiverged means both sides have commits the other does not.
	SyncDiverged
)

// String renders the state as a short lower-case label, for the list command
// and for test failure messages.
func (s SyncState) String() string {
	switch s {
	case SyncUnknown:
		return "unknown"
	case SyncNoUpstream:
		return "no-upstream"
	case SyncGone:
		return "gone"
	case SyncInSync:
		return "in-sync"
	case SyncAhead:
		return "ahead"
	case SyncBehind:
		return "behind"
	case SyncDiverged:
		return "diverged"
	default:
		return "invalid"
	}
}

// Status is the working tree's dirtiness, counted by category.
//
// It belongs to a working tree, not to a branch: only the checked-out branch
// can be dirty, and a repository with linked worktrees has one Status per
// worktree.
type Status struct {
	Head       string // branch name, or "(detached)"
	Upstream   string // empty when nothing is tracked
	Ahead      int
	Behind     int
	Staged     int
	Modified   int
	Untracked  int
	Conflicted int

	// Files is the number of distinct paths with any change. It is not the sum
	// of the counters above: a path staged *and* modified in the working tree
	// counts once here but towards both Staged and Modified.
	Files int

	// Unborn marks a branch that has no commits yet, as in a freshly created
	// repository. Such a branch has no ref, so it cannot appear in Branches --
	// without this it would be misreported as a detached HEAD.
	Unborn bool
}

// Dirty reports whether the working tree has any change at all.
func (s Status) Dirty() bool {
	return s.Staged+s.Modified+s.Untracked+s.Conflicted > 0
}

// Total counts change records across every category. A path that is both
// staged and modified counts twice, once per category; use Files for a count
// of distinct paths.
func (s Status) Total() int {
	return s.Staged + s.Modified + s.Untracked + s.Conflicted
}

// Branch is one local branch, and the worktree it is checked out in if any.
type Branch struct {
	Name     string
	Upstream string
	Sync     SyncState
	Ahead    int
	Behind   int
	Head     bool      // checked out in the repository's primary worktree
	Worktree string    // path of the worktree holding it; empty if not checked out
	Commit   string    // abbreviated object name
	When     time.Time // last commit date

	// Status is the dirt of the worktree this branch is checked out in, and is
	// nil for every branch that is not checked out anywhere.
	Status *Status
}

// Repo is a single repository as gittree knows it.
type Repo struct {
	Dir       string // working tree root, absolute
	GitDir    string
	CommonDir string // shared with linked worktrees; where FETCH_HEAD lives

	Branches []Branch
	Status   Status // the primary worktree's dirt

	LastFetch time.Time // zero means never fetched
	Stashes   int
	Remotes   int

	// Err records a scan failure. A repository that failed to scan is still
	// shown, with the error against it, rather than dropped.
	Err error
	// FetchErr records a background fetch failure, kept separate from Err so a
	// network problem never hides a repository's local state.
	FetchErr error
}

// Dirty reports whether the primary worktree has uncommitted changes.
func (r *Repo) Dirty() bool { return r.Status.Dirty() }

// Head returns the branch checked out in the primary worktree, or nil when the
// repository is in a detached HEAD state.
func (r *Repo) Head() *Branch {
	for i := range r.Branches {
		if r.Branches[i].Head {
			return &r.Branches[i]
		}
	}
	return nil
}

// HumanAge renders how long ago something happened, in the coarsest unit that
// still says something useful. A zero time means it never happened, which for
// a fetch is the common case rather than an error.
func HumanAge(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
