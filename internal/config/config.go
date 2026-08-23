// Package config loads gittree's optional config.yaml.
//
// The file is a plain typed struct decoded with yaml.v3, deliberately not a
// flag/env/file layering problem: everything here is a default that a command
// line flag overrides, which is one rule and needs no framework for it.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// FileName is the file looked for inside the application's config directory.
const FileName = "config.yaml"

// Config is the whole file.
type Config struct {
	Tool      Tool      `yaml:"tool"`
	Discovery Discovery `yaml:"discovery"`
	Fetch     Fetch     `yaml:"fetch"`
	Scan      Scan      `yaml:"scan"`
	UI        UI        `yaml:"ui"`
}

// Tool is which program the enter key hands the terminal to.
type Tool struct {
	Command       string   `yaml:"command"`
	Args          []string `yaml:"args"`
	FallbackShell *bool    `yaml:"fallback_shell"`
}

// Discovery controls the filesystem walk.
type Discovery struct {
	Depth          *int     `yaml:"depth"`
	Nested         *bool    `yaml:"nested"`
	IncludeBare    *bool    `yaml:"include_bare"`
	Ignore         []string `yaml:"ignore"`
	IgnoreDefaults *bool    `yaml:"ignore_defaults"`
}

// Fetch controls the background fetch.
type Fetch struct {
	Enabled     *bool     `yaml:"enabled"`
	Concurrency *int      `yaml:"concurrency"`
	Timeout     *Duration `yaml:"timeout"`
	Prune       *bool     `yaml:"prune"`
	All         *bool     `yaml:"all"`
}

// Scan controls how repositories are read.
type Scan struct {
	Concurrency *int `yaml:"concurrency"`
}

// UI controls presentation.
type UI struct {
	ASCII *bool `yaml:"ascii"`
}

// Pointer fields above are deliberate: a nil means "not set in the file", which
// is what lets a false or zero in the file win over a non-zero built-in
// default. A plain bool could not express the difference.

// Duration is a time.Duration that decodes from a string such as "20s".
type Duration time.Duration

// UnmarshalYAML decodes a duration string.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var s string
	if err := node.Decode(&s); err != nil {
		return fmt.Errorf("duration must be a string such as \"20s\": %w", err)
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the value as a time.Duration.
func (d *Duration) Duration() time.Duration {
	if d == nil {
		return 0
	}
	return time.Duration(*d)
}

// Load reads the configuration.
//
// An explicit path must exist; without one, a missing file is not an error,
// because the file is entirely optional and most users will never write one.
func Load(path, appName string) (*Config, string, error) {
	if path == "" {
		found, ok := locate(appName)
		if !ok {
			// No config file anywhere is the normal case, not a failure: the
			// file is optional and most users will never write one.
			return &Config{}, "", nil
		}
		path = found
	}

	// #nosec G304 -- the path is either given by the user on the command line
	// or found under their own XDG config directories.
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, path, fmt.Errorf("config file %s does not exist", path)
		}
		return nil, path, fmt.Errorf("read config: %w", err)
	}

	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, path, fmt.Errorf("parse %s: %w", path, err)
	}
	return &c, path, nil
}

// Locate finds config.yaml under the XDG config directories, most preferred
// first.
func Locate(appName string) (string, error) {
	if path, ok := locate(appName); ok {
		return path, nil
	}
	return "", fmt.Errorf("no %s found in the XDG config directories", FileName)
}

func locate(appName string) (string, bool) {
	for _, dir := range configDirs() {
		candidate := filepath.Join(dir, appName, FileName)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

// DefaultPath is where a config file would go if the user wrote one.
func DefaultPath(appName string) string {
	return filepath.Join(configDirs()[0], appName, FileName)
}

// configDirs returns the XDG config search path, most preferred first:
// $XDG_CONFIG_HOME (default "$HOME/.config"), then $XDG_CONFIG_DIRS.
func configDirs() []string {
	var dirs []string
	if home := os.Getenv("XDG_CONFIG_HOME"); home != "" {
		dirs = append(dirs, home)
	} else if h, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(h, ".config"))
	}
	extra := os.Getenv("XDG_CONFIG_DIRS")
	if extra == "" {
		extra = "/etc/xdg"
	}
	for dir := range strings.SplitSeq(extra, ":") {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		dirs = append(dirs, ".")
	}
	return dirs
}
