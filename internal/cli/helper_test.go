package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// execute drives the real cobra tree with argv and captures both streams.
//
// Tests go through Execute rather than calling command implementations
// directly, so flag parsing and dispatch stay covered.
func execute(t *testing.T, argv ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = Execute(context.Background(), argv, &out, &errOut, strings.NewReader(""))
	return out.String(), errOut.String(), err
}

// TestMain isolates the whole package from the developer's own configuration.
//
// It is done here rather than per test so that nothing can forget it, and so
// that a test which does want a config file can simply t.Setenv over it and
// have the override unwound automatically.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gittree-cli-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "home"))
	os.Setenv("XDG_CONFIG_DIRS", filepath.Join(dir, "dirs"))
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// requireGit skips a test that needs a real git binary.
func needGit(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// gitRun runs a git command in dir, isolated from the developer's own config.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"HOME="+t.TempDir(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// fixtureTree builds a directory of repositories to run the CLI against:
//
//	root/
//	  alpha/            clean, no upstream
//	  nested/deep/beta/ one untracked file
//	  notarepo/         a plain directory, which must never appear
func fixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	for _, rel := range []string{"alpha", filepath.Join("nested", "deep", "beta")} {
		dir := filepath.Join(root, rel)
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "init", "-q", "-b", "main")
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun(t, dir, "add", "f.txt")
		gitRun(t, dir, "commit", "-qm", "first")
	}

	if err := os.MkdirAll(filepath.Join(root, "notarepo", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	// Make beta dirty so --dirty has something to select.
	beta := filepath.Join(root, "nested", "deep", "beta")
	if err := os.WriteFile(filepath.Join(beta, "untracked.txt"), []byte("u\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
