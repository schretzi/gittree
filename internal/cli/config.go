package cli

import (
	"github.com/spf13/cobra"

	"github.com/schretzi/gittree/internal/config"
	"github.com/schretzi/gittree/internal/git"
	"github.com/schretzi/gittree/internal/launch"
)

// loadConfig reads the config file and folds it into the resolved options.
//
// Precedence is flag > file > built-in default, implemented by only taking a
// value from the file when the corresponding flag was not given. cobra's
// Changed() is what makes that distinguishable from a flag left at its default.
func (a *app) loadConfig(cmd *cobra.Command) error {
	cfg, path, err := config.Load(a.config, appName)
	if err != nil {
		return err
	}
	a.configPath = path
	a.cfg = cfg

	set := cmd.Flags().Changed
	a.applyDiscovery(cfg.Discovery, set)
	a.applyFetch(cfg.Fetch, set)
	if cfg.Scan.Concurrency != nil && !set("scan-concurrency") {
		a.opts.scanConcurrency = *cfg.Scan.Concurrency
	}
	a.opts.ascii = cfg.UI.ASCII != nil && *cfg.UI.ASCII
	return nil
}

func (a *app) applyDiscovery(d config.Discovery, set func(string) bool) {
	if d.Depth != nil && !set("depth") {
		a.opts.depth = *d.Depth
	}
	if d.Nested != nil && !set("nested") {
		a.opts.nested = *d.Nested
	}
	if d.IgnoreDefaults != nil && !set("no-ignore-defaults") {
		a.opts.noIgnoreDefault = !*d.IgnoreDefaults
	}
	// Entries from the file and from --ignore are additive: both are things
	// the user has said they never want to see.
	a.opts.ignore = append(a.opts.ignore, d.Ignore...)
	a.opts.includeBare = d.IncludeBare != nil && *d.IncludeBare
}

func (a *app) applyFetch(f config.Fetch, set func(string) bool) {
	if f.Enabled != nil && !set("no-fetch") {
		a.opts.noFetch = !*f.Enabled
	}
	if f.Concurrency != nil && !set("concurrency") {
		a.opts.concurrency = *f.Concurrency
	}
	if f.Timeout != nil && !set("fetch-timeout") {
		a.opts.fetchTimeout = f.Timeout.Duration()
	}
	a.opts.fetchPrune = f.Prune == nil || *f.Prune
	a.opts.fetchAll = f.All != nil && *f.All
}

// launcher builds the handoff from the config file.
func (a *app) launcher() launch.Launcher {
	l := launch.Launcher{FallbackShell: true}
	if a.cfg == nil {
		return l
	}
	l.Tool = a.cfg.Tool.Command
	l.Args = a.cfg.Tool.Args
	if a.cfg.Tool.FallbackShell != nil {
		l.FallbackShell = *a.cfg.Tool.FallbackShell
	}
	return l
}

// fetchOptions builds the background fetch settings.
func (a *app) fetchOptions() git.FetchOptions {
	o := git.DefaultFetchOptions()
	o.Timeout = a.opts.fetchTimeout
	o.Concurrency = a.opts.concurrency
	o.Prune = a.opts.fetchPrune
	o.All = a.opts.fetchAll
	return o
}

// referenceConfig is printed by "gittree config init". Every value shown is
// the built-in default, so the file can be edited down to just the lines that
// differ.
const referenceConfig = `# gittree configuration.
#
# Every setting here is a default; the matching command line flag wins over it.
# Delete anything you do not want to change.

tool:
  # What enter hands the terminal to. gittree never changes a repository
  # itself; this is the program that does.
  command: lazygit
  args: []
  # Drop into $SHELL in the repository when the tool above is not installed.
  fallback_shell: true

discovery:
  # How many directory levels below the starting point to search.
  depth: 8
  # Descend into a repository to find repositories inside it. Off by default:
  # submodules already show in their parent's status, and vendored trees are
  # full of repositories nobody asked about.
  nested: false
  # Bare repositories have no working tree, so most of the display is
  # meaningless for them.
  include_bare: false
  # Apply the built-in skip list (node_modules, vendor, Library, ...).
  ignore_defaults: true
  # Extra directory names to skip. Added to the built-in list, not replacing it.
  ignore: []

fetch:
  # Fetch every repository in the background at startup, so ahead/behind
  # reflects the remote rather than whenever you last fetched by hand.
  enabled: true
  concurrency: 8
  timeout: 20s
  # Remove remote-tracking refs whose branch is gone on the remote. Without
  # this a deleted upstream is reported as "in sync" forever. It only touches
  # refs/remotes; local branches and tags are never affected.
  prune: true
  # Fetch every remote rather than just the current branch's.
  all: false

scan:
  # How many repositories to read at once. 0 means one per CPU.
  concurrency: 0

ui:
  # Force the ASCII glyph set. By default the locale decides.
  ascii: false
`

func newConfigCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "config",
		Short:         "Show or create the configuration file",
		Args:          usageArgs(cobra.NoArgs),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "path",
			Short: "Print where the configuration file is, or would be",
			Long: `path prints the config file gittree is using.

When no file exists anywhere, it prints where one would be looked for first,
so the output is always a usable target for redirection.`,
			Args:          usageArgs(cobra.NoArgs),
			SilenceUsage:  true,
			SilenceErrors: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				if a.configPath != "" {
					a.outln(a.configPath)
					return nil
				}
				a.outln(config.DefaultPath(appName))
				a.infoln("note: this file does not exist yet")
				return nil
			},
		},
		&cobra.Command{
			Use:   "init",
			Short: "Print a commented reference configuration",
			Long: `init writes a fully commented configuration to stdout.

It is printed rather than written to disk so nothing is ever overwritten
behind your back:

    mkdir -p "$(dirname "$(gittree config path)")"
    gittree config init > "$(gittree config path)"`,
			Args:          usageArgs(cobra.NoArgs),
			SilenceUsage:  true,
			SilenceErrors: true,
			RunE: func(_ *cobra.Command, _ []string) error {
				a.outf("%s", referenceConfig)
				return nil
			},
		},
	)
	return cmd
}
