package cli

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"

	"github.com/schretzi/gittree/internal/git"
	"github.com/schretzi/gittree/internal/launch"
	"github.com/schretzi/gittree/internal/tui"
)

// runTUI draws the interactive tree.
func runTUI(ctx context.Context, a *app, path string) error {
	root, err := resolveRoot(path)
	if err != nil {
		return err
	}
	client := git.Client{}
	if err := requireGit(ctx, client); err != nil {
		return err
	}

	// Anything that writes to stderr while the alternate screen is up paints
	// over the UI. Silencing the standard logger is cheap insurance against a
	// stray log.Print in this code or in a dependency.
	log.SetOutput(io.Discard)
	if a.logFile != "" {
		f, err := tea.LogToFile(a.logFile, appName)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer func() { _ = f.Close() }()
	}

	launcher := a.launcher()

	model := tui.New(tui.Config{
		Root:            root,
		Discover:        a.discoverOptions(root),
		Client:          client,
		ScanConcurrency: a.opts.scanConcurrency,
		ASCII:           a.opts.ascii,
		Dark:            lipgloss.HasDarkBackground(os.Stdin, os.Stdout),
		Cmds:            a.commands(ctx, client, root, launcher),
	})

	opts := []tea.ProgramOption{tea.WithContext(ctx)}
	if a.noColor {
		opts = append(opts, tea.WithColorProfile(colorprofile.NoTTY))
	}

	final, err := tea.NewProgram(model, opts...).Run()
	if err != nil {
		return err
	}
	if m, ok := final.(tui.Model); ok {
		return m.Fatal()
	}
	return nil
}

// commands wires the model's side effects, capturing the context here so the
// model never has to hold one in a field.
func (a *app) commands(ctx context.Context, c git.Client, root string, l launch.Launcher) tui.Commands {
	concurrency := a.opts.scanConcurrency
	if concurrency == 0 {
		concurrency = git.DefaultScanConcurrency()
	}
	cmds := tui.Commands{
		Discover: func() tea.Cmd { return tui.DiscoverCmd(ctx, a.discoverOptions(root)) },
		ScanAll:  func(dirs []string) tea.Cmd { return tui.ScanAllCmd(ctx, c, dirs, concurrency) },
		ScanOne:  func(dir string) tea.Cmd { return tui.ScanOneCmd(ctx, c, dir) },
		Launch:   func(dir string) tea.Cmd { return launchCmd(l, dir) },
		Pull: func(dir, repoPath, name string) tea.Cmd {
			return tui.PullCmd(ctx, c, dir, repoPath, name, git.DefaultPullOptions())
		},
	}
	if !a.opts.noFetch {
		fo := a.fetchOptions()
		cmds.Fetch = func(dirs []string) tea.Cmd { return tui.FetchStreamCmd(ctx, c, dirs, fo) }
	}
	return cmds
}

// launchCmd suspends the UI and gives the terminal to the git tool.
func launchCmd(l launch.Launcher, dir string) tea.Cmd {
	cmd, _, err := l.Command(dir)
	if err != nil {
		return func() tea.Msg { return tui.ToolExitedMsg{Dir: dir, Err: err} }
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return tui.ToolExitedMsg{Dir: dir, Err: err}
	})
}
