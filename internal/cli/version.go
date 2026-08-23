package cli

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Build information. These are set at link time by goreleaser:
//
//	-X github.com/schretzi/gittree/internal/cli.version=...
//
// When they are not - a plain "go build", or "go install" - buildInfo falls
// back to what the toolchain stamped into the binary.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

const licenseNotice = "gittree is MIT licensed. Copyright (c) 2026 Martin Fuchsluger."

type buildDetails struct {
	Version string
	Commit  string
	Date    string
}

// buildInfo prefers link-time values and falls back to the VCS stamps the Go
// toolchain embeds, so a binary built with plain "go build" still reports
// something useful.
func buildInfo() buildDetails {
	b := buildDetails{Version: version, Commit: commit, Date: date}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	if b.Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		b.Version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if b.Commit == "" {
				b.Commit = s.Value
			}
		case "vcs.time":
			if b.Date == "" {
				b.Date = s.Value
			}
		case "vcs.modified":
			if s.Value == "true" {
				b.Version += "-dirty"
			}
		}
	}
	return b
}

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:           "version",
		Short:         "Print version, build and runtime information",
		Args:          usageArgs(cobra.NoArgs),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			b := buildInfo()
			a.outf("%s %s\n", appName, b.Version)
			if b.Commit != "" {
				a.outf("commit:  %s\n", b.Commit)
			}
			if b.Date != "" {
				a.outf("built:   %s\n", b.Date)
			}
			a.outf("go:      %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
			a.outln(licenseNotice)
			return nil
		},
	}
}

// atLeast reports whether a dotted version string is at least major.minor.
// Anything it cannot parse counts as new enough, so an unusual git build
// (Apple's, say) is never rejected on a parsing technicality.
func atLeast(v string, major, minor int) bool {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return true
	}
	gotMajor, err := strconv.Atoi(parts[0])
	if err != nil {
		return true
	}
	gotMinor, err := strconv.Atoi(parts[1])
	if err != nil {
		return true
	}
	if gotMajor != major {
		return gotMajor > major
	}
	return gotMinor >= minor
}
