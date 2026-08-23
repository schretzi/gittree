package tui

import (
	"github.com/schretzi/gittree/internal/discover"
	"github.com/schretzi/gittree/internal/git"
)

// discoveredMsg carries the result of the filesystem walk.
type discoveredMsg struct {
	dirs  []string
	stats discover.Stats
	err   error
}

// scannedMsg carries a batch of scan results.
type scannedMsg struct {
	repos []*git.Repo
}

// repoScannedMsg carries a single repository's rescan, which is what a manual
// refresh and the return from the git tool both produce.
type repoScannedMsg struct {
	repo *git.Repo
}

// fetchStartedMsg hands the model the channel that background fetch results
// will arrive on.
type fetchStartedMsg struct {
	ch <-chan git.FetchResult
}

// fetchResultMsg is one repository's fetch outcome.
type fetchResultMsg struct {
	result git.FetchResult
	ch     <-chan git.FetchResult
}

// fetchFinishedMsg reports that every fetch has completed.
type fetchFinishedMsg struct{}

// ToolExitedMsg reports that the external git tool has finished and the
// terminal is ours again.
//
// It is exported because the CLI builds the tea.ExecProcess callback that
// produces it, having resolved which tool to run.
type ToolExitedMsg struct {
	Dir string
	Err error
}
