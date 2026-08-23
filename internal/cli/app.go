package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/schretzi/gittree/internal/config"
)

// appName is how the tool is invoked, and the directory looked up under the
// XDG config directories to find config.yaml.
const appName = "gittree"

// Process exit codes, following the Unix convention: 0 on success, 1 for a
// runtime failure, 2 for a usage error.
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// app carries what every command implementation needs beyond its own flags:
// where to write, where to read from, and the flags shared by the whole tree.
//
// out and err are separate so the reporting commands' output can be piped
// without notices, warnings and progress mixed into it. In code: outf/outln
// for a command's actual result, infof/infoln for everything else.
type app struct {
	out io.Writer
	err io.Writer
	in  io.Reader

	verbose bool
	noColor bool
	config  string
	logFile string

	// configPath is where the config file was actually found, empty when there
	// is none.
	configPath string
	cfg        *config.Config

	opts options
}

// options are the discovery and scan settings shared by the TUI and by list.
type options struct {
	depth           int
	nested          bool
	ignore          []string
	noIgnoreDefault bool
	concurrency     int
	scanConcurrency int
	noFetch         bool
	fetchTimeout    time.Duration
	fetchPrune      bool
	fetchAll        bool
	includeBare     bool
	ascii           bool
}

// infoln is infof for a message that needs no formatting.
func (a *app) infoln(v ...any) { fmt.Fprintln(a.err, v...) }

// infof writes a notice, warning or progress message to the diagnostic
// stream, keeping stdout free for the command's actual result.
func (a *app) infof(format string, v ...any) { fmt.Fprintf(a.err, format, v...) }

// outf writes to the result stream. Use it only for output that *is* the
// command's answer.
func (a *app) outf(format string, v ...any) { fmt.Fprintf(a.out, format, v...) }

// outln is outf for a message that needs no formatting.
func (a *app) outln(v ...any) { fmt.Fprintln(a.out, v...) }
