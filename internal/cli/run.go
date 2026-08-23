package cli

import (
	"context"
	"os"

	"github.com/charmbracelet/x/term"
)

// runRoot draws the UI, or prints the list when there is no terminal to draw
// on.
//
// The non-terminal fallback is what makes "gittree | less" and "gittree > f"
// behave the way anyone would expect, rather than emitting escape sequences
// into a pipe.
func runRoot(ctx context.Context, a *app, path string) error {
	if !stdoutIsTerminal(a) {
		return runList(ctx, a, path, listFlags{})
	}
	return runTUI(ctx, a, path)
}

// stdoutIsTerminal reports whether the real process stdout is a terminal.
//
// It deliberately inspects os.Stdout rather than a.out: tests wire a buffer
// into a.out and must never start a UI, and the UI itself has to talk to the
// actual terminal regardless.
func stdoutIsTerminal(a *app) bool {
	if a.out != os.Stdout {
		return false
	}
	return term.IsTerminal(os.Stdout.Fd())
}
