package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeConfig puts a config file in a temp XDG config home and points the
// environment at it.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "gittree")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", home)
	return path
}

func TestLoadFullConfig(t *testing.T) {
	writeConfig(t, `
tool:
  command: tig
  args: ["--all"]
  fallback_shell: false
discovery:
  depth: 3
  nested: true
  ignore: [target, .stack-work]
fetch:
  enabled: false
  concurrency: 2
  timeout: 45s
  prune: false
scan:
  concurrency: 4
ui:
  ascii: true
`)
	c, path, err := Load("", "gittree")
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Error("Load did not report where the file was found")
	}
	if c.Tool.Command != "tig" || len(c.Tool.Args) != 1 {
		t.Errorf("tool = %+v", c.Tool)
	}
	// A false in the file has to beat a true default, which is the whole
	// reason these fields are pointers.
	if c.Tool.FallbackShell == nil || *c.Tool.FallbackShell {
		t.Error("fallback_shell: false was not read as false")
	}
	if c.Fetch.Enabled == nil || *c.Fetch.Enabled {
		t.Error("fetch.enabled: false was not read as false")
	}
	if c.Fetch.Prune == nil || *c.Fetch.Prune {
		t.Error("fetch.prune: false was not read as false")
	}
	if got := c.Fetch.Timeout.Duration(); got != 45*time.Second {
		t.Errorf("timeout = %v, want 45s", got)
	}
	if c.Discovery.Depth == nil || *c.Discovery.Depth != 3 {
		t.Errorf("depth = %v, want 3", c.Discovery.Depth)
	}
	if len(c.Discovery.Ignore) != 2 {
		t.Errorf("ignore = %v, want two entries", c.Discovery.Ignore)
	}
}

func TestUnsetFieldsStayNil(t *testing.T) {
	writeConfig(t, "tool:\n  command: tig\n")
	c, _, err := Load("", "gittree")
	if err != nil {
		t.Fatal(err)
	}
	// nil is what tells the CLI to leave the built-in default alone, as
	// distinct from a zero the user actually asked for.
	if c.Fetch.Enabled != nil {
		t.Error("an unset field should stay nil, not become false")
	}
	if c.Discovery.Depth != nil {
		t.Error("an unset depth should stay nil, not become 0")
	}
}

func TestMissingConfigIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_DIRS", t.TempDir())
	// The file is entirely optional; most users will never write one.
	c, path, err := Load("", "gittree")
	if err != nil {
		t.Fatalf("a missing config file must not be an error: %v", err)
	}
	if c == nil {
		t.Fatal("Load returned no config")
	}
	if path != "" {
		t.Errorf("path = %q, want empty when nothing was found", path)
	}
}

func TestExplicitMissingPathIsAnError(t *testing.T) {
	// Asking for a specific file that is not there is a mistake worth
	// reporting, unlike simply having no config at all.
	_, _, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), "gittree")
	if err == nil {
		t.Fatal("want an error for an explicitly named missing file")
	}
}

func TestInvalidDurationIsReportedClearly(t *testing.T) {
	writeConfig(t, "fetch:\n  timeout: soon\n")
	_, _, err := Load("", "gittree")
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.Contains(err.Error(), "soon") {
		t.Errorf("error %q should quote the offending value", err)
	}
}

func TestInvalidYAMLIsReported(t *testing.T) {
	writeConfig(t, "tool: [this is not a mapping\n")
	if _, _, err := Load("", "gittree"); err == nil {
		t.Fatal("want an error for malformed YAML")
	}
}

func TestDefaultPathUsesXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/config")
	if got, want := DefaultPath("gittree"), filepath.Join("/custom/config", "gittree", FileName); got != want {
		t.Errorf("DefaultPath = %q, want %q", got, want)
	}
}

func TestNilDurationIsZero(t *testing.T) {
	var d *Duration
	if d.Duration() != 0 {
		t.Error("a nil Duration should read as zero, not panic")
	}
}
