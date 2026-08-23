package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubPATH points PATH at a directory holding fake executables, so these tests
// exercise the real exec.LookPath rather than a stand-in for it.
func stubPATH(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	return dir
}

func TestResolvePrefersConfiguredTool(t *testing.T) {
	dir := stubPATH(t, "lazygit", "tig")
	r, err := Launcher{Tool: "tig"}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != filepath.Join(dir, "tig") {
		t.Errorf("resolved %q, want the configured tool", r.Path)
	}
	if r.Fallback {
		t.Error("the configured tool is not a fallback")
	}
}

func TestResolveDefaultsToLazygit(t *testing.T) {
	dir := stubPATH(t, "lazygit")
	r, err := Launcher{}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != filepath.Join(dir, "lazygit") || r.Name != DefaultTool {
		t.Errorf("resolved %+v, want lazygit", r)
	}
}

func TestResolveFallsBackToShell(t *testing.T) {
	dir := stubPATH(t, "bash")
	t.Setenv("SHELL", filepath.Join(dir, "bash"))

	// gittree has to stay useful on a machine where lazygit was never
	// installed, which is the state this was designed against.
	r, err := Launcher{FallbackShell: true}.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if !r.Fallback {
		t.Error("falling back to the shell should be reported as such")
	}
	if !strings.HasSuffix(r.Path, "bash") {
		t.Errorf("resolved %q, want the shell", r.Path)
	}
}

func TestResolveWithoutFallbackFails(t *testing.T) {
	stubPATH(t)
	_, err := Launcher{FallbackShell: false}.Resolve()
	if err == nil {
		t.Fatal("want an error when the tool is missing and there is no fallback")
	}
	if !strings.Contains(err.Error(), DefaultTool) {
		t.Errorf("error %q should name the missing tool", err)
	}
}

func TestCommandRunsInTheRepoDirectory(t *testing.T) {
	stubPATH(t, "lazygit")
	cmd, r, err := Launcher{}.Command("/some/repo")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/some/repo" {
		t.Errorf("cmd.Dir = %q, want the repository directory", cmd.Dir)
	}
	if r.Name != DefaultTool {
		t.Errorf("resolved %q, want %q", r.Name, DefaultTool)
	}
	// Bubble Tea's ExecProcess wires the streams and restores the terminal;
	// setting them here would break the handoff.
	if cmd.Stdin != nil || cmd.Stdout != nil || cmd.Stderr != nil {
		t.Error("streams must be left alone for ExecProcess to wire")
	}
	// The user's lazygit config, GPG agent, pager and editor all have to keep
	// working, so the environment is inherited untouched.
	if cmd.Env != nil {
		t.Error("the environment must be inherited, not replaced")
	}
}

func TestCommandPassesArgs(t *testing.T) {
	stubPATH(t, "lazygit")
	cmd, _, err := Launcher{Args: []string{"--use-config-file", "x.yml"}}.Command("/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 3 || cmd.Args[1] != "--use-config-file" {
		t.Errorf("args = %v, want the configured arguments", cmd.Args)
	}
}
