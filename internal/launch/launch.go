// Package launch resolves and builds the command gittree hands the terminal
// to.
//
// gittree deliberately does not implement git operations. When you want to act
// on a repository it suspends itself and gives the whole terminal to a tool
// that already does that job well -- lazygit by default, your shell otherwise.
package launch

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// DefaultTool is what gittree reaches for first.
const DefaultTool = "lazygit"

// Launcher resolves which program to run.
type Launcher struct {
	// Tool is the configured command. Empty means DefaultTool.
	Tool string
	// Args are passed to Tool before the repository directory is applied as
	// the working directory.
	Args []string
	// FallbackShell allows dropping into $SHELL when Tool is not installed.
	FallbackShell bool
}

// ErrNoTool is returned when nothing suitable could be found to run.
var ErrNoTool = errors.New("no git tool or shell found")

// Resolved describes what will be run, so the UI can say so before running it.
type Resolved struct {
	Path     string
	Args     []string
	Name     string
	Fallback bool // true when this is the shell rather than the wanted tool
}

// Resolve picks the program to run: the configured tool if it is installed,
// otherwise the user's shell, so gittree stays useful on a machine where
// lazygit was never installed.
func (l Launcher) Resolve() (Resolved, error) {
	tool := l.Tool
	if tool == "" {
		tool = DefaultTool
	}
	if path, err := exec.LookPath(tool); err == nil {
		return Resolved{Path: path, Args: l.Args, Name: tool}, nil
	}
	if !l.FallbackShell {
		return Resolved{}, fmt.Errorf("%s is not installed", tool)
	}
	for _, sh := range []string{os.Getenv("SHELL"), "/bin/sh"} {
		if sh == "" {
			continue
		}
		if path, err := exec.LookPath(sh); err == nil {
			return Resolved{Path: path, Name: sh, Fallback: true}, nil
		}
	}
	return Resolved{}, ErrNoTool
}

// Command builds the process to run in dir.
//
// Three things are deliberately not done here, each of which would break the
// handoff:
//
//   - No exec.CommandContext. A SIGINT arriving while lazygit owns the
//     foreground would kill lazygit out from under the user.
//   - No stdin/stdout/stderr. Bubble Tea's ExecProcess wires the program's own
//     streams and restores the terminal afterwards.
//   - No environment changes. The user's lazygit config, GPG agent, pager and
//     editor all have to keep working.
func (l Launcher) Command(dir string) (*exec.Cmd, Resolved, error) {
	r, err := l.Resolve()
	if err != nil {
		return nil, Resolved{}, err
	}
	// #nosec G204 -- the program comes from LookPath over a configured name and
	// dir comes from gittree's own filesystem walk; neither is user-supplied
	// command text, and nothing is passed through a shell.
	cmd := exec.Command(r.Path, r.Args...)
	cmd.Dir = dir
	return cmd, r, nil
}
