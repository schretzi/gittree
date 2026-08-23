// Package tui draws the interactive tree.
//
// The model here owns view state only. Everything it displays is produced by
// internal/discover, internal/git and internal/tree, none of which import
// Bubble Tea -- so the whole data path is testable without a terminal, and
// Update is a pure function of (model, message).
package tui

import (
	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"

	"github.com/schretzi/gittree/internal/discover"
	"github.com/schretzi/gittree/internal/git"
	"github.com/schretzi/gittree/internal/tree"
)

// Commands are the side effects the model triggers, injected as factories.
//
// They are passed in rather than built here for two reasons: the model never
// has to hold a context.Context in a field, and tests can drive Update with
// stubs and no git binary at all.
type Commands struct {
	Discover func() tea.Cmd
	ScanAll  func(dirs []string) tea.Cmd
	ScanOne  func(dir string) tea.Cmd
	Launch   func(dir string) tea.Cmd
	// Fetch is nil when background fetching is switched off.
	Fetch func(dirs []string) tea.Cmd
}

// Config is what the CLI hands the UI at startup.
type Config struct {
	Root            string
	Discover        discover.Options
	Client          git.Client
	ScanConcurrency int
	ASCII           bool
	Dark            bool
	Cmds            Commands
}

// Model is the Bubble Tea model.
type Model struct {
	cfg Config

	// repos is the source of truth for scan results, keyed by repository
	// directory. The tree is rebuilt from it, so filtering and re-shaping
	// never risk losing a result.
	repos     map[string]*git.Repo
	repoPaths []string

	root  *tree.Node
	index map[string]*tree.Node
	rows  []tree.Row

	// cursorID is authoritative and cursor is derived from it. Keeping the
	// identity rather than the index is what lets the cursor stay on the same
	// thing when rows appear above it or the tree is re-shaped.
	cursorID string
	cursor   int
	top      int

	width, height int

	styles Styles
	glyphs Glyphs
	keys   KeyMap
	help   help.Model

	showHelp  bool
	dirtyOnly bool
	loading   bool
	scanned   int

	// Fetch progress. fetchErrs is keyed by repository directory so a failure
	// can be shown against its own row instead of as a wall of messages.
	fetching   bool
	fetchDone  int
	fetchTotal int
	fetchErrs  map[string]error

	status string
	fatal  error
}

// New builds the initial model. Nothing is discovered or scanned yet; Init
// starts that.
func New(cfg Config) Model {
	if cfg.ScanConcurrency == 0 {
		cfg.ScanConcurrency = git.DefaultScanConcurrency()
	}
	h := help.New()
	return Model{
		cfg:       cfg,
		fetchErrs: map[string]error{},
		repos:     map[string]*git.Repo{},
		root:      tree.Build(cfg.Root, nil),
		index:     map[string]*tree.Node{},
		styles:    NewStyles(cfg.Dark),
		glyphs:    PickGlyphs(cfg.ASCII),
		keys:      DefaultKeyMap(),
		help:      h,
		loading:   true,
		status:    "searching…",
	}
}

// rebuild reconstructs the tree from the repositories known so far.
//
// It rebuilds rather than patching because compaction is not stable as
// repositories arrive: a/b/repo1 collapses to a single row, and the arrival of
// a/c/repo2 forces it to split again. Rebuilding is microseconds and always
// correct; the saved per-path state is what makes it invisible to the user.
func (m *Model) rebuild() {
	saved := tree.Save(m.root)

	m.root = tree.Build(m.cfg.Root, m.visiblePaths())
	tree.Compact(m.root, true)
	tree.Restore(m.root, saved)
	m.index = tree.Index(m.root)

	// Attach scan results, and choose a default for anything the user has not
	// touched. Both are keyed by absolute path, which survives compaction --
	// the display label does not.
	for path, node := range m.index {
		if r, ok := m.repos[path]; ok {
			node.Repo = r
		}
		if _, touched := saved[path]; touched {
			continue
		}
		// A directory exists only to show where repositories are, so hiding
		// its contents by default would hide the whole point. A repository is
		// the opposite: its collapsed row already carries everything, and its
		// branches are detail to open on demand.
		node.Expanded = node.Kind == tree.KindDir
	}
	tree.AutoExpand(m.root)

	m.reflow()
}

// visiblePaths applies the dirty-only filter to the discovered repositories.
func (m *Model) visiblePaths() []string {
	if !m.dirtyOnly {
		return m.repoPaths
	}
	out := make([]string, 0, len(m.repoPaths))
	for _, p := range m.repoPaths {
		// A repository that has not been scanned yet is kept: hiding it would
		// make rows flicker away as results land.
		r, ok := m.repos[p]
		if !ok || r.Err != nil || r.Dirty() {
			out = append(out, p)
		}
	}
	return out
}

// reflow recomputes the visible rows and puts the cursor back on whatever it
// was pointing at.
func (m *Model) reflow() {
	m.rows = tree.Flatten(m.root)
	m.restoreCursor()
	m.clampScroll()
}

// restoreCursor re-finds the cursor row by identity, falling back to the
// nearest valid index when the row it was on has gone away.
func (m *Model) restoreCursor() {
	if len(m.rows) == 0 {
		m.cursor, m.cursorID = 0, ""
		return
	}
	if m.cursorID != "" {
		for i, r := range m.rows {
			if r.ID == m.cursorID {
				m.cursor = i
				return
			}
		}
	}
	m.cursor = min(m.cursor, len(m.rows)-1)
	m.cursor = max(m.cursor, 0)
	m.cursorID = m.rows[m.cursor].ID
}

// visibleRows is how many tree rows fit between the headers and the footer.
func (m *Model) visibleRows() int {
	const chrome = 4 // path header, column header, footer, blank line
	return max(m.height-chrome, 1)
}

// clampScroll keeps the cursor inside the window, with a couple of rows of
// context above and below wherever there is room for them.
func (m *Model) clampScroll() {
	const scrolloff = 2
	h := m.visibleRows()

	if m.cursor < m.top+scrolloff {
		m.top = m.cursor - scrolloff
	}
	if m.cursor >= m.top+h-scrolloff {
		m.top = m.cursor - h + 1 + scrolloff
	}
	m.top = min(m.top, len(m.rows)-h)
	m.top = max(m.top, 0)
}

// selected returns the row under the cursor, and whether there is one.
func (m *Model) selected() (tree.Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return tree.Row{}, false
	}
	return m.rows[m.cursor], true
}

// moveCursor shifts the cursor by delta and records the new identity.
func (m *Model) moveCursor(delta int) {
	if len(m.rows) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+delta, 0), len(m.rows)-1)
	m.cursorID = m.rows[m.cursor].ID
	m.clampScroll()
}
