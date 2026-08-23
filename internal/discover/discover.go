// Package discover finds git repositories beneath a directory.
//
// It knows nothing about git itself beyond how a working tree announces
// itself, and nothing about the terminal UI, so it is exercisable with a
// plain directory tree and no git binary at all.
package discover

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Kind distinguishes how a working tree is attached to its repository.
type Kind int

const (
	// KindWorktree is an ordinary repository: .git is a directory.
	KindWorktree Kind = iota
	// KindLinked is a worktree added with "git worktree add": .git is a file
	// pointing into the parent repository's worktrees/ directory.
	KindLinked
	// KindSubmodule is a submodule checkout: .git is a file pointing into the
	// superproject's modules/ directory.
	KindSubmodule
)

// Found is one discovered working tree.
type Found struct {
	Dir  string
	Kind Kind
}

// DefaultIgnore lists directory names never descended into.
//
// Dot-directories are deliberately not skipped wholesale: ~/.dotfiles and
// ~/.config routinely hold repositories that the user cares about most.
var DefaultIgnore = []string{
	"node_modules", "vendor", ".venv", "venv", ".direnv",
	"target", "build", "dist", ".next", ".nuxt",
	".cache", ".gradle", ".terraform", ".tox", "__pycache__",
	".m2", ".npm", ".cargo", ".rustup",
	"Library", ".Trash", "Applications",
}

// DefaultMaxDepth bounds how far below the root the walk descends. It exists
// to stop "gittree ~" wandering into deep, uninteresting trees, not as a
// correctness measure.
const DefaultMaxDepth = 8

// Options controls a walk. The zero value is not useful; use Defaults.
type Options struct {
	Root string
	// MaxDepth counts path segments below Root. Zero means unlimited.
	MaxDepth int
	// Ignore holds directory base names to skip.
	Ignore []string
	// Nested descends into a repository to find repositories inside it.
	// Off by default: submodules already show up in their parent's status, and
	// vendored trees are full of repositories nobody asked about.
	Nested bool
	// IncludeBare reports bare repositories too. Off by default, since a bare
	// repository has no working tree and so no branch status or dirt.
	IncludeBare bool
}

// Defaults returns the options a plain "gittree" invocation uses.
func Defaults(root string) Options {
	return Options{Root: root, MaxDepth: DefaultMaxDepth, Ignore: DefaultIgnore}
}

// Stats records what the walk stepped over, for a one-line footer rather than
// a stream of warnings.
type Stats struct {
	Dirs    int
	Denied  int
	Skipped int
}

// Walk finds every repository under o.Root, calling fn for each one in the
// order encountered.
//
// A permission error is counted and stepped over rather than returned: one
// unreadable directory must not abort a scan of the whole tree. The error
// returned is only ever the context's.
func Walk(ctx context.Context, o Options, fn func(Found)) (Stats, error) {
	var st Stats
	root := filepath.Clean(o.Root)
	ignore := make(map[string]bool, len(o.Ignore))
	for _, name := range o.Ignore {
		ignore[name] = true
	}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// A directory we may not read, or that vanished mid-walk.
			if d != nil && d.IsDir() {
				st.Denied++
				return fs.SkipDir
			}
			return nil
		}
		// Symlinks arrive as non-directory entries, so WalkDir never follows
		// them. That is the wanted behaviour: it makes cycles impossible and
		// stops one repository being reported under two paths.
		if !d.IsDir() {
			return nil
		}
		st.Dirs++

		if path != root {
			skip, counted := shouldSkip(o, ignore, root, path, d.Name())
			if skip {
				if counted {
					st.Skipped++
				}
				return fs.SkipDir
			}
		}

		return report(o, path, fn)
	})

	if err != nil && ctx.Err() != nil {
		return st, ctx.Err()
	}
	return st, nil
}

// report emits path if it is a repository, and returns what the walk should do
// next: descend, or step over the repository's contents.
func report(o Options, path string, fn func(Found)) error {
	kind, ok := classify(path)
	if !ok {
		if o.IncludeBare && isBare(path) {
			fn(Found{Dir: path, Kind: KindWorktree})
			return fs.SkipDir
		}
		return nil
	}
	fn(Found{Dir: path, Kind: kind})
	// Descending into a repository turns up submodules, which already show in
	// their parent's status, and vendored trees full of repositories nobody
	// asked about. --nested opts in.
	if o.Nested {
		return nil
	}
	return fs.SkipDir
}

// shouldSkip decides whether a directory is worth descending into. counted
// reports whether the skip was a deliberate exclusion worth showing the user,
// as opposed to routine housekeeping like stepping over a .git directory.
func shouldSkip(o Options, ignore map[string]bool, root, path, name string) (skip, counted bool) {
	if ignore[name] {
		return true, true
	}
	// .git is never a place to look for further repositories.
	if name == ".git" {
		return true, false
	}
	if o.MaxDepth > 0 && depth(root, path) > o.MaxDepth {
		return true, false
	}
	return false, false
}

// depth counts path segments between root and path.
func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

// classify reports whether dir is a working tree, and of what kind.
//
// .git is a directory for an ordinary repository and a file for both linked
// worktrees and submodules; all three are real working trees with branches and
// dirt, so all three are reported.
func classify(dir string) (Kind, bool) {
	fi, err := os.Lstat(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	if fi.IsDir() {
		return KindWorktree, true
	}
	if !fi.Mode().IsRegular() {
		return 0, false
	}
	// #nosec G304 -- dir comes from gittree's own walk and the file name is a
	// constant. The Lstat above already established that .git is a regular
	// file here, so this cannot be redirected through a symlink.
	b, err := os.ReadFile(filepath.Join(dir, ".git"))
	if err != nil {
		return 0, false
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
	if target == "" {
		return 0, false
	}
	if strings.Contains(filepath.ToSlash(target), "/worktrees/") {
		return KindLinked, true
	}
	return KindSubmodule, true
}

// isBare reports whether dir looks like a bare repository.
func isBare(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		return false
	}
	for _, sub := range []string{"objects", "refs"} {
		if fi, err := os.Stat(filepath.Join(dir, sub)); err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}
