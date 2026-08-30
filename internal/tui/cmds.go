package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/schretzi/gittree/internal/discover"
	"github.com/schretzi/gittree/internal/git"
)

// discoverCmd walks the tree for repositories.
func DiscoverCmd(ctx context.Context, o discover.Options) tea.Cmd {
	return func() tea.Msg {
		var dirs []string
		stats, err := discover.Walk(ctx, o, func(f discover.Found) {
			dirs = append(dirs, f.Dir)
		})
		return discoveredMsg{dirs: dirs, stats: stats, err: err}
	}
}

// scanAllCmd scans every discovered repository.
func ScanAllCmd(ctx context.Context, c git.Client, dirs []string, concurrency int) tea.Cmd {
	return func() tea.Msg {
		return scannedMsg{repos: c.ScanAll(ctx, dirs, concurrency)}
	}
}

// FetchStreamCmd starts the background fetch and hands back its channel.
func FetchStreamCmd(ctx context.Context, c git.Client, dirs []string, o git.FetchOptions) tea.Cmd {
	return func() tea.Msg {
		return fetchStartedMsg{ch: c.FetchStream(ctx, dirs, o)}
	}
}

// waitForFetch blocks on the next fetch result and re-arms itself, which is
// how a channel is drained into the update loop one message at a time.
func waitForFetch(ch <-chan git.FetchResult) tea.Cmd {
	return func() tea.Msg {
		res, ok := <-ch
		if !ok {
			return fetchFinishedMsg{}
		}
		return fetchResultMsg{result: res, ch: ch}
	}
}

// scanOneCmd rescans a single repository, for a manual refresh or on returning
// from the git tool.
func ScanOneCmd(ctx context.Context, c git.Client, dir string) tea.Cmd {
	return func() tea.Msg {
		return repoScannedMsg{repo: c.Scan(ctx, dir)}
	}
}

// PullCmd pulls changes for a single repository.
func PullCmd(ctx context.Context, c git.Client, dir, repoPath, name string, o git.PullOptions) tea.Cmd {
	return func() tea.Msg {
		err := c.Pull(ctx, dir, o)
		return pullResultMsg{Dir: dir, RepoPath: repoPath, Name: name, Err: err}
	}
}
