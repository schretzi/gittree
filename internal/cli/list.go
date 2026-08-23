package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/schretzi/gittree/internal/discover"
	"github.com/schretzi/gittree/internal/git"
)

type listFlags struct {
	branches bool
	dirty    bool
	asJSON   bool
}

func newListCmd(a *app) *cobra.Command {
	var lf listFlags

	cmd := &cobra.Command{
		Use:   "list [flags] [path]",
		Short: "Print the state of every repository, one per line",
		Long: `list prints the same information the UI shows, as plain text.

It is what gittree falls back to when stdout is not a terminal, so piping and
redirecting a bare "gittree" produces this too.`,
		Example: `  # every repository below the current directory
  gittree list

  # only the ones with uncommitted changes
  gittree list --dirty

  # every branch, not just the checked-out one
  gittree list --branches ~/Workspace

  # for scripting
  gittree list --json | jq '.[] | select(.sync == "behind")'`,

		Args:          usageArgs(cobra.MaximumNArgs(1)),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), a, argPath(args), lf)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&lf.branches, "branches", "b", false, "print every local branch, not just the checked-out one")
	f.BoolVarP(&lf.dirty, "dirty", "d", false, "print only repositories with uncommitted changes")
	f.BoolVar(&lf.asJSON, "json", false, "print JSON instead of a table")

	// The walk is configured on the root command; list needs the same knobs.
	f.IntVar(&a.opts.depth, "depth", discover.DefaultMaxDepth, "how many directory levels below the root to search (0 for unlimited)")
	f.BoolVar(&a.opts.nested, "nested", false, "descend into repositories to find repositories inside them")
	f.StringArrayVar(&a.opts.ignore, "ignore", nil, "directory name to skip (repeatable)")
	f.BoolVar(&a.opts.noIgnoreDefault, "no-ignore-defaults", false, "do not skip the built-in list of directories such as node_modules")
	f.IntVar(&a.opts.scanConcurrency, "scan-concurrency", 0, "how many repositories to scan at once (0 for one per CPU)")

	return cmd
}

// scanTree is the whole M1 data path: walk, then scan what was found.
func (a *app) scanTree(ctx context.Context, path string) (root string, repos []*git.Repo, err error) {
	root, err = resolveRoot(path)
	if err != nil {
		return "", nil, err
	}

	var c git.Client
	if err := requireGit(ctx, c); err != nil {
		return "", nil, err
	}

	dirs, stats, err := a.findRepos(ctx, root)
	if err != nil {
		return "", nil, err
	}
	if a.verbose {
		a.infof("walked %d directories, skipped %d, could not read %d\n",
			stats.Dirs, stats.Skipped, stats.Denied)
	}
	if stats.Denied > 0 && !a.verbose {
		a.infof("note: %d directories could not be read\n", stats.Denied)
	}

	concurrency := a.opts.scanConcurrency
	if concurrency == 0 {
		concurrency = git.DefaultScanConcurrency()
	}
	return root, c.ScanAll(ctx, dirs, concurrency), nil
}

func runList(ctx context.Context, a *app, path string, lf listFlags) error {
	root, repos, err := a.scanTree(ctx, path)
	if err != nil {
		return err
	}

	rows := buildRows(root, repos, lf)
	if lf.asJSON {
		enc := json.NewEncoder(a.out)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	printTable(a, rows, lf.branches)
	return nil
}

// listRow is one printed line, and the JSON shape. The field names are part of
// the scripting interface, so they are chosen to be stable and obvious.
type listRow struct {
	Repo      string `json:"repo"`
	Path      string `json:"path"`
	Branch    string `json:"branch"`
	Sync      string `json:"sync"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead,omitempty"`
	Behind    int    `json:"behind,omitempty"`
	Dirty     int    `json:"dirty"`
	Staged    int    `json:"staged,omitempty"`
	Modified  int    `json:"modified,omitempty"`
	Untracked int    `json:"untracked,omitempty"`
	Conflicts int    `json:"conflicts,omitempty"`
	Stashes   int    `json:"stashes,omitempty"`
	Fetched   string `json:"fetched"`
	Head      bool   `json:"head"`
	Error     string `json:"error,omitempty"`
}

func buildRows(root string, repos []*git.Repo, lf listFlags) []listRow {
	var rows []listRow
	for _, r := range repos {
		name := relName(root, r.Dir)
		if r.Err != nil {
			rows = append(rows, listRow{Repo: name, Path: r.Dir, Error: r.Err.Error()})
			continue
		}
		if lf.dirty && !r.Dirty() {
			continue
		}
		if lf.branches && len(r.Branches) > 0 {
			for i := range r.Branches {
				rows = append(rows, branchRow(name, r, &r.Branches[i]))
			}
			continue
		}
		rows = append(rows, headRow(name, r))
	}
	return rows
}

func headRow(name string, r *git.Repo) listRow {
	row := listRow{
		Repo: name, Path: r.Dir, Head: true,
		Sync:    git.SyncUnknown.String(),
		Fetched: git.HumanAge(r.LastFetch),
		Stashes: r.Stashes,
	}
	if b := r.Head(); b != nil {
		row.Branch, row.Sync, row.Upstream = b.Name, b.Sync.String(), b.Upstream
		row.Ahead, row.Behind = b.Ahead, b.Behind
	} else if r.Status.Unborn {
		// A repository with no commits yet has no ref to list, so it would
		// otherwise be reported as a detached HEAD.
		row.Branch, row.Sync = r.Status.Head, "unborn"
	} else {
		row.Branch, row.Sync = "(detached)", "-"
	}
	applyStatus(&row, r.Status)
	return row
}

func branchRow(name string, r *git.Repo, b *git.Branch) listRow {
	row := listRow{
		Repo: name, Path: r.Dir, Branch: b.Name, Head: b.Head,
		Sync: b.Sync.String(), Upstream: b.Upstream,
		Ahead: b.Ahead, Behind: b.Behind,
		Fetched: git.HumanAge(r.LastFetch),
	}
	// Only a branch that is actually checked out somewhere can be dirty.
	if b.Status != nil {
		applyStatus(&row, *b.Status)
	}
	return row
}

func applyStatus(row *listRow, s git.Status) {
	row.Dirty = s.Files
	row.Staged, row.Modified = s.Staged, s.Modified
	row.Untracked, row.Conflicts = s.Untracked, s.Conflicted
}

func printTable(a *app, rows []listRow, withBranches bool) {
	w := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tBRANCH\tSYNC\tUPSTREAM\tDIRTY\tFETCHED")
	for _, r := range rows {
		if r.Error != "" {
			fmt.Fprintf(w, "%s\t-\terror\t-\t-\t%s\n", r.Repo, firstLine(r.Error))
			continue
		}
		branch := r.Branch
		if withBranches && r.Head {
			branch = "* " + branch
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Repo, branch, syncLabel(r), dashIfEmpty(r.Upstream), dirtyLabel(r), r.Fetched)
	}
	_ = w.Flush()
}

// syncLabel adds the counts to the state, since "ahead" alone does not say by
// how much.
func syncLabel(r listRow) string {
	switch {
	case r.Ahead > 0 && r.Behind > 0:
		return fmt.Sprintf("diverged +%d-%d", r.Ahead, r.Behind)
	case r.Ahead > 0:
		return fmt.Sprintf("ahead +%d", r.Ahead)
	case r.Behind > 0:
		return fmt.Sprintf("behind -%d", r.Behind)
	default:
		return r.Sync
	}
}

func dirtyLabel(r listRow) string {
	if r.Dirty == 0 {
		return "clean"
	}
	var parts []string
	for _, p := range []struct {
		n   int
		sym string
	}{{r.Staged, "+"}, {r.Modified, "~"}, {r.Untracked, "?"}, {r.Conflicts, "!"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%s%d", p.sym, p.n))
		}
	}
	return strings.Join(parts, " ")
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// relName renders a repository's path relative to the root, so the output
// reads as a tree rather than as a column of identical absolute prefixes.
func relName(root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return dir
	}
	if rel == "." {
		return filepath.Base(dir)
	}
	return filepath.ToSlash(rel)
}
