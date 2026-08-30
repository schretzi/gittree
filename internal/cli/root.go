// Package cli implements the gittree command-line interface: the cobra command
// tree and the work each command drives. It lives outside package main so the
// whole CLI can be exercised in-process by tests, and so tools/gendocs can walk
// the command tree without running it.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/schretzi/gittree/internal/discover"
	"github.com/schretzi/gittree/internal/git"
)

// usageError marks an error as a misuse of the CLI - a bad flag, a wrong
// number of arguments - so Main can exit 2 and print the offending command's
// usage, instead of exiting 1 like a runtime failure.
type usageError struct {
	cmd *cobra.Command
	err error
}

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// usageArgs wraps a cobra positional-argument validator so the errors it
// produces are classified as usage errors.
func usageArgs(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			return &usageError{cmd: cmd, err: err}
		}
		return nil
	}
}

// Main runs the CLI with argv (typically os.Args[1:]) and returns the process
// exit code. It is the only entry point package main needs.
func Main(argv []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err := Execute(ctx, argv, os.Stdout, os.Stderr, os.Stdin)
	switch {
	case err == nil:
		return exitOK
	// A quit by SIGINT is how the TUI is meant to be left; it is not a failure.
	case errors.Is(err, context.Canceled):
		return exitOK
	}

	if usageErr, ok := errors.AsType[*usageError](err); ok {
		fmt.Fprintln(os.Stderr, "error:", err)
		fmt.Fprint(os.Stderr, usageErr.cmd.UsageString())
		return exitUsage
	}

	fmt.Fprintln(os.Stderr, "error:", err)
	return exitFailure
}

// Execute builds a fresh command tree, points it at the given streams, and
// runs it. Tests call this directly with buffers so they exercise the real
// parsing and dispatch rather than a stand-in.
func Execute(ctx context.Context, argv []string, out, errOut io.Writer, in io.Reader) error {
	cmd := newRootCmd(&app{out: out, err: errOut, in: in})
	cmd.SetArgs(argv)
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetIn(in)
	return cmd.ExecuteContext(ctx)
}

// Root returns a fresh command tree wired to the process streams. It exists
// for tools/gendocs, which needs the tree in order to render it without
// running anything.
func Root() *cobra.Command {
	return newRootCmd(&app{out: os.Stdout, err: os.Stderr, in: os.Stdin})
}

// newRootCmd assembles the whole command tree. It returns a new tree on every
// call: cobra accumulates flag state on a command, so sharing one instance
// across runs (or tests) leaks values between them.
func newRootCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   appName + " [flags] [path]",
		Short: "Browse the state of every git repository under a directory",
		Long: `gittree finds every git repository beneath a directory and shows them as
one scrollable tree: each repository's local branches, how each branch stands
against its upstream, and what is uncommitted in the working tree.

It never changes a repository. Pressing enter on one hands the terminal to
lazygit (or to your shell) so you can act on it there, and rescans the row
when you come back.

With no path, gittree starts from the current directory. When stdout is not a
terminal it prints the same data as "gittree list" instead of drawing a UI, so
piping and redirecting work.`,
		Example: `  # browse everything under the current directory
  gittree

  # browse a specific tree
  gittree ~/Workspace

  # the same data, scriptable
  gittree list --dirty ~/Workspace`,

		Args: usageArgs(cobra.MaximumNArgs(1)),

		// Runs for every command in the tree, so the config file is loaded and
		// folded into the options exactly once, before anything uses them.
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			return a.loadConfig(c)
		},

		// Main decides how to render errors and which exit code they map to,
		// so cobra must not print them or dump usage itself.
		SilenceUsage:  true,
		SilenceErrors: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoot(cmd.Context(), a, argPath(args))
		},
	}

	pf := cmd.PersistentFlags()
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "report skipped and unreadable directories")
	pf.BoolVar(&a.noColor, "no-color", false, "disable colour output")
	pf.StringVar(&a.config, "config", "", "path to config.yaml (default: XDG config directory)")
	pf.StringVar(&a.logFile, "log-file", "", "write debug logs to this file")

	f := cmd.Flags()
	f.IntVar(&a.opts.depth, "depth", discover.DefaultMaxDepth, "how many directory levels below the root to search (0 for unlimited)")
	f.BoolVar(&a.opts.nested, "nested", false, "descend into repositories to find repositories inside them")
	f.StringArrayVar(&a.opts.ignore, "ignore", nil, "directory name to skip (repeatable)")
	f.BoolVar(&a.opts.noIgnoreDefault, "no-ignore-defaults", false, "do not skip the built-in list of directories such as node_modules")
	f.BoolVar(&a.opts.noFetch, "no-fetch", false, "do not fetch in the background on startup")
	f.IntVar(&a.opts.concurrency, "concurrency", defaultFetchConcurrency, "how many repositories to fetch at once")
	f.IntVar(&a.opts.scanConcurrency, "scan-concurrency", 0, "how many repositories to scan at once (0 for one per CPU)")
	f.DurationVar(&a.opts.fetchTimeout, "fetch-timeout", defaultFetchTimeout, "give up on a single repository's fetch after this long")

	// A malformed flag is a misuse of the CLI just as much as a bad argument
	// is, so it must exit 2 as well. Subcommands inherit this from the root.
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &usageError{cmd: c, err: err}
	})

	cmd.AddCommand(
		newListCmd(a),
		newConfigCmd(a),
		newVersionCmd(a),
	)
	return cmd
}

// defaults used by both the root command's flags and the config file.
const (
	defaultFetchConcurrency = 8
	defaultFetchTimeout     = 20 * time.Second
)

// argPath returns the directory to start from, defaulting to the current one.
func argPath(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "."
}

// resolveRoot turns the path argument into an absolute, symlink-free
// directory, failing early with a clear message rather than showing an empty
// tree.
//
// Resolving symlinks is not cosmetic. "git rev-parse --show-toplevel" always
// reports the real path, so a root that still contains a symlink would not be
// a prefix of any repository path and every repository would be rendered with
// its full absolute path instead of its position in the tree. On macOS this
// bites immediately, since /tmp and /var are both symlinks.
func resolveRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("%s is not a directory", path)
	}
	// A path that cannot be resolved is still usable; fall back to it rather
	// than refusing to start.
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	return abs, nil
}

// discoverOptions builds the walk settings from the resolved flags.
func (a *app) discoverOptions(root string) discover.Options {
	o := discover.Defaults(root)
	o.MaxDepth = a.opts.depth
	o.Nested = a.opts.nested
	if a.opts.noIgnoreDefault {
		o.Ignore = nil
	}
	o.Ignore = append(o.Ignore, a.opts.ignore...)
	o.IncludeBare = a.opts.includeBare
	return o
}

// findRepos walks the tree and returns the repository directories found.
func (a *app) findRepos(ctx context.Context, root string) ([]string, discover.Stats, error) {
	var dirs []string
	stats, err := discover.Walk(ctx, a.discoverOptions(root), func(f discover.Found) {
		dirs = append(dirs, f.Dir)
	})
	return dirs, stats, err
}

// requireGit fails with an actionable message rather than letting every single
// repository report its own "executable file not found".
func requireGit(ctx context.Context, c git.Client) error {
	if !c.Available() {
		return errors.New("git not found on PATH")
	}
	// %(worktreepath) is what makes the branch-to-worktree mapping free.
	if v, err := c.Version(ctx); err == nil && !atLeast(v, 2, 29) {
		return fmt.Errorf("git %s is too old; gittree needs 2.29 or newer", v)
	}
	return nil
}
