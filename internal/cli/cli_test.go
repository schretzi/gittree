package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/schretzi/gittree/internal/config"
	"github.com/schretzi/gittree/internal/launch"
)

func TestVersionGoesToStdout(t *testing.T) {
	out, errOut, err := execute(t, "version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.HasPrefix(out, appName+" ") {
		t.Errorf("stdout = %q, want it to start with the tool name", out)
	}
	if !strings.Contains(out, "MIT") {
		t.Errorf("stdout = %q, want the licence notice", out)
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want empty", errOut)
	}
}

func TestUsageErrorsAreClassified(t *testing.T) {
	cases := [][]string{
		{"list", "a", "b"},   // too many positional arguments
		{"a", "b"},           // same, on the root command
		{"--nope"},           // unknown flag on the root
		{"list", "--nope"},   // unknown flag on a subcommand
		{"version", "extra"}, // unexpected argument
	}
	for _, argv := range cases {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			_, _, err := execute(t, argv...)
			if err == nil {
				t.Fatal("want an error")
			}
			// Main maps this classification onto exit code 2; if it is missed,
			// misuse would be reported as a runtime failure instead.
			var ue *usageError
			if !errors.As(err, &ue) {
				t.Errorf("error %v is not a usageError", err)
			}
		})
	}
}

func TestListFindsReposAndSkipsPlainDirs(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)

	out, _, err := execute(t, "list", root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "alpha") {
		t.Errorf("output missing alpha:\n%s", out)
	}
	// The repo is reported at its full relative path, and the directories
	// leading to it are not rows of their own here.
	if !strings.Contains(out, "nested/deep/beta") {
		t.Errorf("output missing nested/deep/beta:\n%s", out)
	}
	// A directory with no repository below it must never appear.
	if strings.Contains(out, "notarepo") {
		t.Errorf("output contains a non-repository directory:\n%s", out)
	}
	// Names are relative to the root. Without this the assertions above would
	// also pass on absolute paths, which is how a symlinked root once slipped
	// through unnoticed.
	if strings.Contains(out, root) {
		t.Errorf("output contains absolute paths, want names relative to the root:\n%s", out)
	}
}

func TestListDirtyFilters(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)

	out, _, err := execute(t, "list", "--dirty", root)
	if err != nil {
		t.Fatalf("list --dirty: %v", err)
	}
	if strings.Contains(out, "alpha") {
		t.Errorf("--dirty included the clean repository:\n%s", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("--dirty dropped the dirty repository:\n%s", out)
	}
}

func TestListJSON(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)

	out, _, err := execute(t, "list", "--json", root)
	if err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var rows []listRow
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2:\n%s", len(rows), out)
	}
	byRepo := map[string]listRow{}
	for _, r := range rows {
		byRepo[r.Repo] = r
	}
	// A fresh repository tracks nothing; that is a state, not an error.
	if got := byRepo["alpha"].Sync; got != "no-upstream" {
		t.Errorf("alpha sync = %q, want no-upstream", got)
	}
	if got := byRepo["alpha"].Dirty; got != 0 {
		t.Errorf("alpha dirty = %d, want 0", got)
	}
	if got := byRepo["nested/deep/beta"].Untracked; got != 1 {
		t.Errorf("beta untracked = %d, want 1", got)
	}
	if got := byRepo["nested/deep/beta"].Fetched; got != "never" {
		t.Errorf("beta fetched = %q, want never", got)
	}
}

func TestListBranches(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)
	gitRun(t, root+"/alpha", "branch", "side")

	out, _, err := execute(t, "list", "--branches", root)
	if err != nil {
		t.Fatalf("list --branches: %v", err)
	}
	if !strings.Contains(out, "side") {
		t.Errorf("--branches did not list the extra branch:\n%s", out)
	}
	// The checked-out branch is marked so it stands out among the others.
	if !strings.Contains(out, "* main") {
		t.Errorf("--branches did not mark the checked-out branch:\n%s", out)
	}
}

func TestListRejectsMissingPath(t *testing.T) {
	_, _, err := execute(t, "list", "/no/such/directory/anywhere")
	if err == nil {
		t.Fatal("want an error for a missing path")
	}
	// It must be a runtime failure, not a usage error: the syntax was fine.
	var ue *usageError
	if errors.As(err, &ue) {
		t.Error("a missing directory should not be classified as misuse")
	}
}

func TestListRejectsAFile(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)
	_, _, err := execute(t, "list", root+"/alpha/f.txt")
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want a not-a-directory failure", err)
	}
}

func TestBareRootDoesNotStartUIWhenOutIsNotStdout(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)
	// execute wires buffers rather than os.Stdout, so the root command must
	// take the list path and never try to draw.
	out, _, err := execute(t, root)
	if err != nil {
		t.Fatalf("root: %v", err)
	}
	if !strings.Contains(out, "REPO") {
		t.Errorf("root command did not fall back to list output:\n%s", out)
	}
}

// writeUserConfig plants a config file where the XDG lookup will find it.
func writeUserConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, appName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", home)
}

func TestConfigFileIsApplied(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)
	writeUserConfig(t, "discovery:\n  depth: 1\n")

	out, _, err := execute(t, "list", root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// alpha sits one level down and survives; beta is three levels down.
	if !strings.Contains(out, "alpha") {
		t.Errorf("depth 1 should still find alpha:\n%s", out)
	}
	if strings.Contains(out, "beta") {
		t.Errorf("depth 1 from the config file was not applied:\n%s", out)
	}
}

func TestFlagBeatsConfigFile(t *testing.T) {
	needGit(t)
	root := fixtureTree(t)
	writeUserConfig(t, "discovery:\n  depth: 1\n")

	out, _, err := execute(t, "list", "--depth", "5", root)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("--depth should override the config file:\n%s", out)
	}
}

func TestBadConfigFails(t *testing.T) {
	writeUserConfig(t, "fetch:\n  timeout: whenever\n")
	_, _, err := execute(t, "list", t.TempDir())
	if err == nil {
		t.Fatal("want an error for an unparseable config file")
	}
	if !strings.Contains(err.Error(), "whenever") {
		t.Errorf("error %q should quote the offending value", err)
	}
}

func TestConfigInitIsValidAndComplete(t *testing.T) {
	out, _, err := execute(t, "config", "init")
	if err != nil {
		t.Fatalf("config init: %v", err)
	}
	// The reference file has to actually parse, or it is worse than useless.
	var c config.Config
	if err := yaml.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("the reference config does not parse: %v", err)
	}
	if c.Tool.Command != launch.DefaultTool {
		t.Errorf("reference tool = %q, want %q", c.Tool.Command, launch.DefaultTool)
	}
	if c.Fetch.Enabled == nil || !*c.Fetch.Enabled {
		t.Error("the reference config should show fetching enabled, the real default")
	}
}
