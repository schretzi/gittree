package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Fetch settings. The timeout is per repository, not for the whole run.
const (
	// DefaultFetchTimeout bounds a single repository's fetch. A hung network
	// call must never be able to stall the UI.
	DefaultFetchTimeout = 20 * time.Second
	// DefaultFetchConcurrency is how many fetches run at once. Fetching is
	// network-bound, and more parallel connections to one host invites rate
	// limiting rather than going faster.
	DefaultFetchConcurrency = 8
	// waitDelay is how long to wait for a killed process group to die before
	// giving up on draining its pipes.
	waitDelay = 2 * time.Second
)

// FetchOptions controls a background fetch.
type FetchOptions struct {
	Timeout     time.Duration
	Concurrency int
	// Prune removes remote-tracking refs whose upstream branch is gone.
	Prune bool
	// All fetches every remote rather than just the current branch's.
	All bool
}

// DefaultFetchOptions is what a plain startup fetch uses.
func DefaultFetchOptions() FetchOptions {
	return FetchOptions{
		Timeout:     DefaultFetchTimeout,
		Concurrency: DefaultFetchConcurrency,
		Prune:       true,
	}
}

// FetchResult is one repository's outcome.
type FetchResult struct {
	Dir string
	Err error
}

// The reasons that more than one phrase maps onto, named so that two spellings
// of the same cause cannot drift apart.
const (
	// #nosec G101 -- this is the words shown in a row when git could not find a
	// credential, not a credential.
	reasonNoCredentials = "no stored credentials"
	reasonSSHRejected   = "ssh key rejected"
	reasonHostNotFound  = "host not found"
	reasonTimedOut      = "connection timed out"
	reasonUnreachable   = "network unreachable"
	reasonBadCert       = "tls certificate rejected"
	reasonInterrupted   = "transfer interrupted"
)

// fetchFailures maps a phrase in git's or ssh's complaint onto the few words a
// row can hold. Matching is case-insensitive, so the phrases are lower case.
//
// Order matters: the first match wins, so the specific causes come before the
// generic wrappers. "unable to access 'https://…': Could not resolve host"
// contains both, and "host not found" is the useful half.
var fetchFailures = []struct{ phrase, reason string }{
	{"terminal prompts disabled", reasonNoCredentials},
	{"could not read username", reasonNoCredentials},
	{"could not read password", reasonNoCredentials},
	{"permission denied (publickey", reasonSSHRejected},
	{"permission denied, please try again", reasonSSHRejected},
	{"authentication failed", "authentication failed"},
	{"host key verification failed", "host key not trusted"},
	{"repository not found", "repository not found"},
	{"does not appear to be a git repository", "remote is not a repository"},
	{"could not resolve host", reasonHostNotFound},
	{"name or service not known", reasonHostNotFound},
	{"nodename nor servname", reasonHostNotFound},
	{"connection timed out", reasonTimedOut},
	{"operation timed out", reasonTimedOut},
	{"connection refused", "connection refused"},
	{"connection closed", "connection closed"},
	{"network is unreachable", reasonUnreachable},
	{"no route to host", reasonUnreachable},
	{"ssl certificate problem", reasonBadCert},
	{"server certificate verification failed", reasonBadCert},
	{"early eof", reasonInterrupted},
	{"rpc failed", reasonInterrupted},
	{"index.lock", "another git is running"},
	{"unable to access", "remote unreachable"},
}

// FetchFailure summarises why a fetch failed, in the few words a row can hold.
//
// It exists because a marker on its own says nothing: a "!" against a row
// needs a legend somewhere else to mean anything, and the row is where the
// reader is already looking.
//
// git's complaint is two or three lines and the useful one is rarely the last
// -- "fatal: Could not read from remote repository." is meaningless without
// the "Permission denied (publickey)." above it. Anything the table does not
// recognise falls back to git's own first line rather than to a shrug: an
// unfamiliar message is still a better hint than "failed".
func FetchFailure(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	}

	text := err.Error()
	if ce, ok := errors.AsType[*CommandError](err); ok {
		// CommandError.Error() leads with the whole argv, which belongs in a
		// log rather than in a row. git's own complaint is the useful part.
		text = ce.Stderr
		if strings.TrimSpace(text) == "" && ce.Err != nil {
			text = ce.Err.Error()
		}
	}

	lower := strings.ToLower(text)
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

// firstComplaint picks the line of stderr that names a cause, skipping the
// advice git prints around it.
func firstComplaint(stderr string) string {
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "warning:") || strings.HasPrefix(line, "hint:") {
			continue
		}
		for _, prefix := range []string{"fatal: ", "error: ", "remote: "} {
			line = strings.TrimPrefix(line, prefix)
		}
		return strings.TrimSuffix(line, ".")
	}
	return ""
}

// condense flattens a message onto one line. A reason is rendered inside a row,
// where a newline would tear the tree in half.
func condense(s string) string {
	return strings.TrimSuffix(strings.Join(strings.Fields(s), " "), ".")
}

// hardenedEnv returns the environment a background fetch runs with.
//
// The whole point is that a fetch can never block waiting for input that will
// never arrive: gittree is holding the alternate screen, so a credential or
// host-key prompt would hang the UI with nothing visible to type into.
//
//   - GIT_TERMINAL_PROMPT=0 stops git prompting for HTTPS credentials.
//   - GIT_ASKPASS and SSH_ASKPASS are cleared, and DISPLAY with them: with a
//     display set, ssh will happily pop up a GUI askpass dialog instead.
//   - BatchMode=yes turns ssh's "ask" into "fail immediately". Note this is
//     deliberately not StrictHostKeyChecking=accept-new, which would silently
//     trust a host key gittree has never seen.
//
// Credential *helpers* are left alone on purpose: the macOS keychain helper
// answers from storage without prompting, and if there is nothing stored,
// GIT_TERMINAL_PROMPT=0 makes it fail in milliseconds.
func hardenedEnv() []string {
	env := make([]string, 0, len(os.Environ())+6)
	for _, kv := range os.Environ() {
		switch key, _, _ := strings.Cut(kv, "="); key {
		case "GIT_ASKPASS", "SSH_ASKPASS", "DISPLAY", "GIT_TERMINAL_PROMPT", "GIT_SSH_COMMAND":
			continue // replaced below, or deliberately unset
		}
		env = append(env, kv)
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0",
		"SSH_ASKPASS_REQUIRE=never",
		"GCM_INTERACTIVE=never",
		"GIT_SSH_COMMAND="+sshCommand(),
	)
}

// sshCommand keeps whatever ssh command the user configured, and appends the
// options that make it fail fast instead of waiting for input.
func sshCommand() string {
	base := os.Getenv("GIT_SSH_COMMAND")
	if base == "" {
		base = "ssh"
	}
	return base + " -o BatchMode=yes -o ConnectTimeout=5"
}

// fetchArgs builds the argv for one fetch.
func fetchArgs(dir string, o FetchOptions) []string {
	args := []string{
		"-C", dir,
		"--no-optional-locks",
		// A background fetch must never trigger a garbage collection behind
		// the user's back.
		"-c", "gc.auto=0",
		"fetch", "--quiet",
		// Fetching submodules recursively turns one fetch into a storm.
		"--recurse-submodules=no",
	}
	if o.Prune {
		// Without --prune a deleted remote branch leaves its tracking ref
		// behind forever, so "[gone]" never appears and gittree would report a
		// branch whose upstream vanished as being in sync. Prune only deletes
		// refs/remotes/*; it cannot touch a local branch or an object.
		args = append(args, "--prune")
	}
	if o.All {
		args = append(args, "--all")
	}
	return args
}

// Fetch updates one repository's remote-tracking refs.
func (c Client) Fetch(ctx context.Context, dir string, o FetchOptions) error {
	if o.Timeout <= 0 {
		o.Timeout = DefaultFetchTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	args := fetchArgs(dir, o)
	// #nosec G204 -- dir comes from gittree's own filesystem walk and every
	// other argument is a compile-time constant. Nothing goes through a shell.
	cmd := exec.CommandContext(ctx, c.bin(), args...)
	cmd.Env = hardenedEnv()
	// /dev/null rather than the terminal: anything that tries to read gets an
	// immediate EOF instead of blocking on input nobody can supply.
	cmd.Stdin = nil

	var stderr strings.Builder
	cmd.Stderr = &stderr

	// Killing only git is not enough. git spawns ssh and credential helpers,
	// and those children inherit the pipes; if they survive, Wait blocks
	// forever and the timeout above is a lie. Killing the whole process group,
	// with a bounded wait for the pipes to drain, is what makes it real.
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

// FetchAll fetches every repository concurrently.
//
// One repository's failure never affects another: results carry their own
// error, and only the caller's context can stop the run.
func (c Client) FetchAll(ctx context.Context, dirs []string, o FetchOptions) []FetchResult {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultFetchConcurrency
	}
	out := make([]FetchResult, len(dirs))
	type job struct {
		i   int
		dir string
	}
	jobs := make([]job, len(dirs))
	for i, d := range dirs {
		jobs[i] = job{i, d}
	}
	pool(ctx, jobs, o.Concurrency, func(ctx context.Context, j job) {
		out[j.i] = FetchResult{Dir: j.dir, Err: c.Fetch(ctx, j.dir, o)}
	})
	for i := range out {
		if out[i].Dir == "" {
			out[i] = FetchResult{Dir: dirs[i], Err: ctx.Err()}
		}
	}
	return out
}

// FetchStream fetches every repository concurrently and delivers each result
// as it lands.
//
// The UI uses this rather than FetchAll so rows update one by one instead of
// all at once at the end: on a tree of any size the difference between "the
// screen fills in over ten seconds" and "nothing happens for ten seconds" is
// the difference between a tool that feels alive and one that feels stuck.
//
// The channel is closed when every fetch has finished or the context is done.
func (c Client) FetchStream(ctx context.Context, dirs []string, o FetchOptions) <-chan FetchResult {
	if o.Concurrency <= 0 {
		o.Concurrency = DefaultFetchConcurrency
	}
	ch := make(chan FetchResult)
	go func() {
		defer close(ch)
		pool(ctx, dirs, o.Concurrency, func(ctx context.Context, dir string) {
			res := FetchResult{Dir: dir, Err: c.Fetch(ctx, dir, o)}
			select {
			case ch <- res:
			case <-ctx.Done():
			}
		})
	}()
	return ch
}
