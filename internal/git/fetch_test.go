package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestHardenedEnv(t *testing.T) {
	// These would each be a way for a background fetch to end up waiting for
	// input that can never arrive while the alternate screen is up.
	t.Setenv("GIT_ASKPASS", "/usr/bin/something")
	t.Setenv("SSH_ASKPASS", "/usr/bin/something-else")
	t.Setenv("DISPLAY", ":0")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")

	env := hardenedEnv()
	get := func(key string) (string, bool) {
		for _, kv := range env {
			if k, v, _ := strings.Cut(kv, "="); k == key {
				return v, true
			}
		}
		return "", false
	}

	for _, key := range []string{"GIT_ASKPASS", "SSH_ASKPASS", "DISPLAY"} {
		if v, ok := get(key); ok {
			t.Errorf("%s survived hardening as %q; ssh would use it to prompt", key, v)
		}
	}
	if v, _ := get("GIT_TERMINAL_PROMPT"); v != "0" {
		t.Errorf("GIT_TERMINAL_PROMPT = %q, want 0", v)
	}
	ssh, ok := get("GIT_SSH_COMMAND")
	if !ok {
		t.Fatal("GIT_SSH_COMMAND not set")
	}
	if !strings.Contains(ssh, "BatchMode=yes") {
		t.Errorf("GIT_SSH_COMMAND = %q, want BatchMode=yes so ssh fails instead of asking", ssh)
	}
	if !strings.Contains(ssh, "ConnectTimeout=") {
		t.Errorf("GIT_SSH_COMMAND = %q, want a connect timeout", ssh)
	}
	// accept-new would silently trust a host key gittree has never seen.
	if strings.Contains(ssh, "accept-new") {
		t.Errorf("GIT_SSH_COMMAND = %q must not weaken host key checking", ssh)
	}
	// The rest of the environment has to survive, or the user's git config,
	// PATH and credential helpers stop working.
	if _, ok := get("PATH"); !ok {
		t.Error("PATH did not survive hardening")
	}
}

func TestHardenedEnvKeepsUserSSHCommand(t *testing.T) {
	t.Setenv("GIT_SSH_COMMAND", "ssh -i /my/key")
	for _, kv := range hardenedEnv() {
		if k, v, _ := strings.Cut(kv, "="); k == "GIT_SSH_COMMAND" {
			if !strings.Contains(v, "-i /my/key") {
				t.Errorf("GIT_SSH_COMMAND = %q, want the user's own options kept", v)
			}
			if !strings.Contains(v, "BatchMode=yes") {
				t.Errorf("GIT_SSH_COMMAND = %q, want BatchMode appended", v)
			}
			return
		}
	}
	t.Fatal("GIT_SSH_COMMAND missing")
}

func TestFetchArgs(t *testing.T) {
	args := fetchArgs("/repo", FetchOptions{Prune: true})
	for _, want := range []string{"--no-optional-locks", "--prune", "--quiet", "--recurse-submodules=no"} {
		if !slices.Contains(args, want) {
			t.Errorf("args %v missing %q", args, want)
		}
	}
	// --prune-tags would delete the user's local tags; --prune only touches
	// remote-tracking refs.
	if slices.Contains(args, "--prune-tags") {
		t.Error("--prune-tags must never be passed: it deletes local tags")
	}
	if !slices.Contains(args, "gc.auto=0") {
		t.Error("a background fetch must not be able to trigger a gc")
	}
	if slices.Contains(fetchArgs("/repo", FetchOptions{}), "--prune") {
		t.Error("--prune should only appear when asked for")
	}
}

// TestFetchTimesOutOnUnreachableHost is the test that matters for the UI never
// hanging: an unroutable remote must fail within the timeout, not sit there.
func TestFetchTimesOutOnUnreachableHost(t *testing.T) {
	requireGit(t)

	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))
	// 203.0.113.0/24 is TEST-NET-3, reserved for documentation and guaranteed
	// not to be routable, so this cannot accidentally reach anything.
	run(t, dir, "remote", "add", "origin", "ssh://git@203.0.113.1/repo.git")

	err := Client{}.Fetch(context.Background(), dir, FetchOptions{Timeout: 3 * time.Second})
	if err == nil {
		t.Fatal("fetching an unreachable host should fail")
	}
	// The reason has to survive as a *CommandError, or the UI has nothing to
	// show the user beyond "it failed".
	if _, ok := errors.AsType[*CommandError](err); !ok {
		t.Errorf("error %v is not a *CommandError, so the reason cannot be shown", err)
	}
}

// TestFetchTimeoutIsActuallyEnforced is the test that guards the process-group
// kill, and it is written to fail if that kill is removed.
//
// Counting stray ssh processes turns out not to discriminate: the orphan exits
// on its own a moment later either way. What does discriminate is the clock.
// exec.CommandContext kills only git; the ssh child it spawned inherits the
// stdout pipe, so Wait blocks until ssh gives up on its own -- about five
// seconds, set by ConnectTimeout -- no matter how short the timeout was. With
// the whole process group killed, the call returns when the timeout says so.
//
// So: a one-second timeout must return in about a second. Without the group
// kill this takes roughly five, and this test fails, which is the point.
func TestFetchTimeoutIsActuallyEnforced(t *testing.T) {
	requireGit(t)

	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))
	// TEST-NET-3, reserved for documentation and guaranteed unroutable, so
	// this can never accidentally reach a real host.
	run(t, dir, "remote", "add", "origin", "ssh://git@203.0.113.7/repo.git")

	const timeout = 1 * time.Second
	start := time.Now()
	err := Client{}.Fetch(context.Background(), dir, FetchOptions{Timeout: timeout})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("fetching an unroutable host should fail")
	}
	// Comfortably above the timeout, comfortably below ssh's own five-second
	// ConnectTimeout, so a loaded machine does not make this flaky while a
	// missing group kill still trips it.
	const limit = 3 * time.Second
	if elapsed > limit {
		t.Errorf("a %v timeout took %v (limit %v): the process group is not being killed, "+
			"so Wait is blocking on a surviving ssh child", timeout, elapsed, limit)
	}
}

func TestFetchAllReportsPerRepoErrors(t *testing.T) {
	requireGit(t)
	base := t.TempDir()
	good := newRepo(t, filepath.Join(base, "good"))
	bad := filepath.Join(base, "notarepo")
	if err := os.MkdirAll(bad, 0o750); err != nil {
		t.Fatal(err)
	}

	results := Client{}.FetchAll(context.Background(), []string{good, bad}, FetchOptions{Timeout: 5 * time.Second})
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	// A repository with no remote fetches successfully and does nothing; the
	// point is that one failure does not take the other down with it.
	if results[1].Err == nil {
		t.Error("scanning a non-repository should have failed")
	}
	if results[0].Dir != good || results[1].Dir != bad {
		t.Errorf("results came back in the wrong order: %+v", results)
	}
}

func TestFetchAllHonoursCancellation(t *testing.T) {
	requireGit(t)
	dir := newRepo(t, filepath.Join(t.TempDir(), "repo"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	results := Client{}.FetchAll(ctx, []string{dir, dir, dir}, FetchOptions{Timeout: time.Minute})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("a cancelled fetch took %v; quitting would feel stuck", elapsed)
	}
	for _, r := range results {
		if r.Err == nil {
			t.Error("a cancelled fetch should report an error")
		}
	}
}

func TestFetchFailureNamesTheCause(t *testing.T) {
	// Real stderr, because the point of the table is that the useful line is
	// rarely the last one git prints.
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ssh key rejected",
			err: &CommandError{Stderr: "git@github.com: Permission denied (publickey).\r\n" +
				"fatal: Could not read from remote repository.\n"},
			want: "ssh key rejected",
		},
		{
			name: "https with nothing in the credential store",
			err: &CommandError{Stderr: "fatal: could not read Username for 'https://github.com': " +
				"terminal prompts disabled\n"},
			want: "no stored credentials",
		},
		{
			name: "dns failure inside the generic wrapper",
			err: &CommandError{Stderr: "fatal: unable to access 'https://example.invalid/x.git/': " +
				"Could not resolve host: example.invalid\n"},
			want: "host not found",
		},
		{
			name: "host key never seen before",
			err:  &CommandError{Stderr: "Host key verification failed.\nfatal: Could not read from remote repository.\n"},
			want: "host key not trusted",
		},
		{
			name: "deleted or private repository",
			err:  &CommandError{Stderr: "remote: Repository not found.\nfatal: repository 'https://x/y.git/' not found\n"},
			want: "repository not found",
		},
		{
			name: "the per-repository timeout fired",
			err:  &CommandError{Stderr: "", Err: context.DeadlineExceeded},
			want: "timed out",
		},
		{
			name: "an unrecognised complaint still says something",
			err:  &CommandError{Stderr: "warning: something odd\nfatal: the widget frobnicator is on fire.\n"},
			want: "the widget frobnicator is on fire",
		},
		{
			name: "no stderr at all falls back to the exit status",
			err:  &CommandError{Stderr: "", Err: errors.New("exit status 128")},
			want: "exit status 128",
		},
		{
			name: "a plain error is classified too",
			err:  errors.New("dial tcp 10.0.0.1:22: connect: connection refused"),
			want: "connection refused",
		},
		{name: "success", err: nil, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FetchFailure(tt.err); got != tt.want {
				t.Errorf("FetchFailure() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchFailureIsAlwaysOneLine(t *testing.T) {
	// It is rendered inside a row, where a newline would tear the tree in half.
	err := &CommandError{Stderr: "fatal: a very\nmulti\nline\tcomplaint\n"}
	if got := FetchFailure(err); strings.ContainsAny(got, "\n\r\t") {
		t.Errorf("FetchFailure() = %q, want a single line", got)
	}
}
