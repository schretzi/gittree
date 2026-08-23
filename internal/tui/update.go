package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/schretzi/gittree/internal/tree"
)

// Init starts the filesystem walk.
func (m Model) Init() tea.Cmd {
	return m.cfg.Cmds.Discover()
}

// Update is split into one handler per message family, both because that is
// clearer and because a single switch covering keys and async results grows
// past any sensible complexity limit.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(msg.Width)
		m.clampScroll()
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	default:
		return m.updateAsync(msg)
	}
}

// updateAsync folds discovery and scan results into the model.
func (m Model) updateAsync(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case discoveredMsg:
		if msg.err != nil {
			m.fatal = msg.err
			return m, tea.Quit
		}
		m.repoPaths = msg.dirs
		m.rebuild()
		if len(msg.dirs) == 0 {
			m.loading = false
			m.status = "no git repositories found"
			return m, nil
		}
		m.status = fmt.Sprintf("scanning %d repositories…", len(msg.dirs))
		if msg.stats.Denied > 0 {
			m.status += fmt.Sprintf(" (%d directories unreadable)", msg.stats.Denied)
		}
		return m, m.cfg.Cmds.ScanAll(msg.dirs)

	case scannedMsg:
		for _, r := range msg.repos {
			m.repos[r.Dir] = r
		}
		m.scanned = len(m.repos)
		m.loading = false
		m.status = ""
		m.rebuild()
		return m, m.startFetch()

	case repoScannedMsg:
		m.repos[msg.repo.Dir] = msg.repo
		m.status = ""
		m.rebuild()
		return m, nil

	case ToolExitedMsg:
		// Whatever happened in the tool, the row is now stale: rescan it so
		// what is on screen matches what was just done.
		if msg.Err != nil {
			m.status = "tool exited: " + msg.Err.Error()
		}
		return m, m.cfg.Cmds.ScanOne(msg.Dir)
	}
	return m.updateFetch(msg)
}

// updateFetch folds background fetch progress into the model.
func (m Model) updateFetch(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fetchStartedMsg:
		return m, waitForFetch(msg.ch)

	case fetchResultMsg:
		m.fetchDone++
		if msg.result.Err != nil {
			m.fetchErrs[msg.result.Dir] = msg.result.Err
		} else {
			delete(m.fetchErrs, msg.result.Dir)
		}
		m.status = m.fetchStatus()
		// A successful fetch moves the remote-tracking refs, so the row's
		// ahead/behind is now stale. Rescanning is what makes the counts
		// update rather than just the timestamp.
		cmds := []tea.Cmd{waitForFetch(msg.ch)}
		if msg.result.Err == nil {
			cmds = append(cmds, m.cfg.Cmds.ScanOne(msg.result.Dir))
		}
		return m, tea.Batch(cmds...)

	case fetchFinishedMsg:
		m.fetching = false
		m.status = ""
		if n := len(m.fetchErrs); n > 0 {
			m.status = fmt.Sprintf("%d %s could not be fetched", n, plural(n, "repository", "repositories"))
		}
		return m, nil
	}
	return m, nil
}

// updateKey handles a keypress.
func (m Model) updateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The help overlay swallows everything except the keys that dismiss it, so
	// nothing is accidentally triggered behind it.
	if m.showHelp {
		if key.Matches(msg, m.keys.Help, m.keys.Quit) {
			m.showHelp = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.showHelp = true
		return m, nil
	}

	if m.moveKey(msg) || m.expandKey(msg) {
		return m, nil
	}
	return m.actionKey(msg)
}

// moveKey handles cursor movement, reporting whether it recognised the key.
func (m *Model) moveKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1)
	case key.Matches(msg, m.keys.PageUp):
		m.moveCursor(-m.visibleRows() / 2)
	case key.Matches(msg, m.keys.PageDown):
		m.moveCursor(m.visibleRows() / 2)
	case key.Matches(msg, m.keys.Top):
		m.moveCursor(-len(m.rows))
	case key.Matches(msg, m.keys.Bottom):
		m.moveCursor(len(m.rows))
	default:
		return false
	}
	return true
}

// expandKey handles the expansion keys, reporting whether it recognised the
// key. Collapse is handled in actionKey because it can move the cursor.
func (m *Model) expandKey(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, m.keys.Expand):
		m.setExpanded(true)
	case key.Matches(msg, m.keys.Toggle):
		m.toggle()
	case key.Matches(msg, m.keys.ExpandAll):
		tree.ExpandAll(m.root)
		m.reflow()
	case key.Matches(msg, m.keys.CollapseAll):
		tree.CollapseAll(m.root)
		m.reflow()
	default:
		return false
	}
	return true
}

// actionKey handles the keys that do something beyond moving around.
func (m Model) actionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Collapse):
		return m.collapseOrParent()
	case key.Matches(msg, m.keys.Enter):
		return m.activate()
	case key.Matches(msg, m.keys.Open):
		return m.launch()
	case key.Matches(msg, m.keys.Rescan):
		return m.rescanSelected()
	case key.Matches(msg, m.keys.RescanAll):
		if len(m.repoPaths) > 0 {
			m.loading = true
			m.status = "rescanning…"
			return m, m.cfg.Cmds.ScanAll(m.repoPaths)
		}
	case key.Matches(msg, m.keys.Fetch):
		return m.fetchSelected()
	case key.Matches(msg, m.keys.FetchAll):
		return m, m.startFetch()
	case key.Matches(msg, m.keys.DirtyOnly):
		m.dirtyOnly = !m.dirtyOnly
		m.rebuild()
	}
	return m, nil
}

// fetchSelected fetches just the repository under the cursor.
func (m Model) fetchSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok || row.Kind == tree.RowDir || m.cfg.Cmds.Fetch == nil {
		return m, nil
	}
	r, scanned := m.repos[row.Node.Path]
	if !scanned || r.Remotes == 0 {
		m.status = row.Node.Name + " has no remote to fetch from"
		return m, nil
	}
	m.fetching = true
	m.fetchDone, m.fetchTotal = 0, 1
	m.status = m.fetchStatus()
	return m, m.cfg.Cmds.Fetch([]string{row.Node.Path})
}

// startFetch begins the background fetch, if there is one to begin.
//
// Only repositories that actually have a remote are fetched: spawning git for
// a repository with nothing to fetch from is pure cost, and four of the ten
// repositories this was designed against have no upstream at all.
func (m *Model) startFetch() tea.Cmd {
	if m.cfg.Cmds.Fetch == nil {
		return nil
	}
	var dirs []string
	for _, p := range m.repoPaths {
		if r, ok := m.repos[p]; ok && r.Err == nil && r.Remotes > 0 {
			dirs = append(dirs, p)
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	m.fetching = true
	m.fetchDone = 0
	m.fetchTotal = len(dirs)
	m.status = m.fetchStatus()
	return m.cfg.Cmds.Fetch(dirs)
}

func (m *Model) fetchStatus() string {
	if !m.fetching {
		return ""
	}
	return fmt.Sprintf("fetching %d/%d…", m.fetchDone, m.fetchTotal)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// activate is Enter: it toggles a directory, and opens the git tool on a
// repository or a branch.
//
// Splitting the meaning by row kind is what keeps Enter from doing something
// meaningless on a directory, where there is no repository to open. Expansion
// is never only on Enter, so nothing becomes unreachable either way.
func (m Model) activate() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok {
		return m, nil
	}
	if row.Kind == tree.RowDir {
		m.toggle()
		return m, nil
	}
	return m.launch()
}

// launch hands the terminal to the configured git tool.
func (m Model) launch() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok || m.cfg.Cmds.Launch == nil {
		return m, nil
	}
	dir := row.Node.Path
	// A branch checked out in a linked worktree opens in that worktree, which
	// is where its changes actually live.
	if row.Kind == tree.RowBranch && row.Branch != nil && row.Branch.Worktree != "" {
		dir = row.Branch.Worktree
	}
	if row.Kind == tree.RowDir {
		return m, nil
	}
	return m, m.cfg.Cmds.Launch(dir)
}

// rescanSelected refreshes just the repository under the cursor.
func (m Model) rescanSelected() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok || row.Kind == tree.RowDir {
		return m, nil
	}
	m.status = "rescanning " + row.Node.Name + "…"
	return m, m.cfg.Cmds.ScanOne(row.Node.Path)
}

func (m *Model) toggle() {
	row, ok := m.selected()
	if !ok || !row.Node.HasChildren() {
		return
	}
	row.Node.Expanded = !row.Node.Expanded
	m.reflow()
}

func (m *Model) setExpanded(v bool) {
	row, ok := m.selected()
	if !ok || !row.Node.HasChildren() {
		return
	}
	row.Node.Expanded = v
	m.reflow()
}

// collapseOrParent closes an open node, or jumps to the parent of a closed
// one -- the behaviour every tree UI has, and the reason left-arrow never
// feels like it does nothing.
func (m Model) collapseOrParent() (tea.Model, tea.Cmd) {
	row, ok := m.selected()
	if !ok {
		return m, nil
	}
	if row.Node.HasChildren() && row.Node.Expanded {
		row.Node.Expanded = false
		m.reflow()
		return m, nil
	}
	parent := row.Node.Parent
	if parent == nil || parent.Parent == nil {
		return m, nil // the root is not drawn, so there is nowhere above it
	}
	m.cursorID = parent.Path
	m.reflow()
	return m, nil
}
