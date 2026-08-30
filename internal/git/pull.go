package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// DefaultPullTimeout bounds a single repository's pull.
const DefaultPullTimeout = 30 * time.Second

// PullOptions controls a git pull invocation.
type PullOptions struct {
	Timeout time.Duration
}

// DefaultPullOptions is the default pull configuration.
func DefaultPullOptions() PullOptions {
	return PullOptions{
		Timeout: DefaultPullTimeout,
	}
}

const (
	reasonUncommittedChanges = "uncommitted changes"
	reasonNoUpstreamBranch   = "no upstream branch"
	reasonMergeConflict      = "merge conflict"
)

// pullFailures maps phrases in git pull error output onto concise reasons.
var pullFailures = []struct{ phrase, reason string }{
	{"local changes to the following files would be overwritten", reasonUncommittedChanges},
	{"your local changes to the following files would be overwritten", reasonUncommittedChanges},
	{"not possible to fast-forward", "cannot fast-forward"},
	{"no tracking information", reasonNoUpstreamBranch},
	{"there is no tracking information", reasonNoUpstreamBranch},
	{"merge conflict", reasonMergeConflict},
	{"automatic merge failed", reasonMergeConflict},
	{"conflict (content)", reasonMergeConflict},
	{"refusing to merge unrelated histories", "unrelated histories"},
	{"another git process seems to be running", "another git is running"},
}

// PullFailure summarises why a pull failed in a few words suitable for the status line.
func PullFailure(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}

	text := err.Error()
	if ce, ok := errors.AsType[*CommandError](err); ok {
		text = ce.Stderr
		if strings.TrimSpace(text) == "" && ce.Err != nil {
			text = ce.Err.Error()
		}
	}

	lower := strings.ToLower(text)
	for _, f := range pullFailures {
		if strings.Contains(lower, f.phrase) {
			return f.reason
		}
	}
	for _, f := range fetchFailures {
		if strings.Contains(lower, f.phrase) {
			return f.reason
		}
	}
	if line := firstComplaint(text); line != "" {
		return condense(line)
	}
	return "failed"
}

// pullArgs builds the argv for git pull.
func pullArgs(dir string) []string {
	return []string{
		"-C", dir,
		"pull", "--quiet",
	}
}

// Pull updates one repository by pulling changes from its remote.
func (c Client) Pull(ctx context.Context, dir string, o PullOptions) error {
	if o.Timeout <= 0 {
		o.Timeout = DefaultPullTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	args := pullArgs(dir)
	// #nosec G204 -- dir comes from gittree's own filesystem walk and every
	// other argument is a compile-time constant. Nothing goes through a shell.
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Env = hardenedEnv()
	cmd.Stdin = nil

	var stderr strings.Builder
	cmd.Stderr = &stderr

	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = waitDelay

	err := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return &CommandError{Args: args, Stderr: truncate(stderr.String()), Err: ctxErr}
	}
	if err != nil {
		return &CommandError{Args: args, Stderr: truncate(stderr.String()), Err: err}
	}
	return nil
}
