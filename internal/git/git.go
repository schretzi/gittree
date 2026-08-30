package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// DefaultBin is the git executable looked up on PATH when a Client does not
// name one.
const DefaultBin = "git"

// maxCapturedStderr bounds what a CommandError carries, so a repository that
// makes git produce megabytes of complaint cannot bloat the log ring.
const maxCapturedStderr = 2000

// Client runs git. The zero value is usable and runs "git" from PATH.
type Client struct {
	Bin string
}

func (c Client) bin() string {
	if c.Bin == "" {
		return DefaultBin
	}
	return c.Bin
}

// CommandError is a git invocation that failed. It carries the full argv and
// git's own complaint, because "exit status 128" on its own is never enough to
// tell what went wrong.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *CommandError) Unwrap() error { return e.Err }

// readArgs are prepended to every read-only invocation.
//
// --no-optional-locks is the important one: without it git takes index.lock
// while it reads, and gittree -- which runs constantly and in parallel across
// a whole tree -- would collide with whatever the user is doing in a shell.
var readArgs = []string{"--no-optional-locks"}

// run executes a read-only git command in dir and returns its stdout.
//
// It checks ctx.Err() after the fact so a cancelled or timed-out command
// reports as such rather than as a meaningless non-zero exit.
func (c Client) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := make([]string, 0, len(readArgs)+len(args)+2)
	full = append(full, "-C", dir)
	full = append(full, readArgs...)
	full = append(full, args...)

	// #nosec G204 -- dir comes from gittree's own filesystem walk and the args
	// are compile-time constants; neither is user-supplied command text.
	cmd := exec.CommandContext(ctx, c.bin(), full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil

	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, &CommandError{Args: full, Stderr: stderr.String(), Err: ctxErr}
	}
	if err != nil {
		e := &CommandError{Args: full, Stderr: truncate(stderr.String()), Err: err}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			e.ExitCode = exitErr.ExitCode()
		}
		return nil, e
	}
	return stdout.Bytes(), nil
}

func truncate(s string) string {
	if len(s) <= maxCapturedStderr {
		return s
	}
	return "..." + s[len(s)-maxCapturedStderr:]
}

// Available reports whether the git binary can be found at all.
func (c Client) Available() bool {
	_, err := exec.LookPath(c.bin())
	return err == nil
}

// Version returns git's reported version string, e.g. "2.50.1".
func (c Client) Version(ctx context.Context) (string, error) {
	// #nosec G204 -- no variable arguments.
	out, err := exec.CommandContext(ctx, c.bin(), "--version").Output()
	if err != nil {
		return "", fmt.Errorf("git --version: %w", err)
	}
	// "git version 2.50.1 (Apple Git-155)"
	fields := strings.Fields(string(out))
	if len(fields) < 3 {
		return "", fmt.Errorf("git --version: unexpected output %q", out)
	}
	return fields[2], nil
}
