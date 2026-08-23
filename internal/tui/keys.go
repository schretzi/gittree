package tui

import "charm.land/bubbles/v2/key"

// KeyMap is every binding the UI understands.
//
// Enter is context-sensitive -- it toggles a directory but launches the git
// tool on a repository -- so expansion also has its own keys. Nothing is ever
// reachable only through Enter.
type KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	Expand      key.Binding
	Collapse    key.Binding
	Toggle      key.Binding
	ExpandAll   key.Binding
	CollapseAll key.Binding

	Enter key.Binding
	Open  key.Binding
	Shell key.Binding

	Rescan    key.Binding
	RescanAll key.Binding
	Fetch     key.Binding
	FetchAll  key.Binding

	DirtyOnly key.Binding
	Help      key.Binding
	Quit      key.Binding
}

// DefaultKeyMap follows the conventions of the tools this sits next to: vi
// motions, k9s-style Enter, and arrow keys for everyone else.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g", "top")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end"), key.WithHelp("G", "bottom")),
		PageUp:   key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("^u", "page up")),
		PageDown: key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("^d", "page down")),

		Expand:      key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "expand")),
		Collapse:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "collapse")),
		Toggle:      key.NewBinding(key.WithKeys(" ", "tab"), key.WithHelp("space", "toggle")),
		ExpandAll:   key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "expand all")),
		CollapseAll: key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "collapse all")),

		Enter: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/toggle")),
		Open:  key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open tool")),
		Shell: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "shell")),

		Rescan:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "rescan")),
		RescanAll: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "rescan all")),
		Fetch:     key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch")),
		FetchAll:  key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "fetch all")),

		DirtyOnly: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "dirty only")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp is the one-line hint bar at the bottom of the screen.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Enter, k.Rescan, k.Help, k.Quit}
}

// FullHelp is the expanded help, grouped by what each column is for.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Top, k.Bottom, k.PageUp, k.PageDown},
		{k.Expand, k.Collapse, k.Toggle, k.ExpandAll, k.CollapseAll},
		{k.Enter, k.Open, k.Shell},
		{k.Rescan, k.RescanAll, k.Fetch, k.FetchAll},
		{k.DirtyOnly, k.Help, k.Quit},
	}
}
