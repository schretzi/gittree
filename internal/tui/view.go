package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/schretzi/gittree/internal/git"
)

// View renders the whole screen.
//
// Only the rows inside the window are rendered. That is the reason for the
// hand-managed window rather than a viewport: a viewport takes a finished
// string, which would mean styling every row of a thousand-repository tree on
// every frame in order to show forty of them.
func (m Model) View() tea.View {
	v := tea.NewView(m.content())
	// In Bubble Tea v2 the alt screen is a property of the view rather than a
	// program option.
	v.AltScreen = true
	return v
}

func (m Model) content() string {
	if m.width == 0 || m.height == 0 {
		// No size reported yet. In a real terminal the first WindowSizeMsg
		// arrives immediately, but a terminal that never reports one would
		// otherwise leave the user staring at a blank screen with no hint
		// that quitting still works.
		return m.styles.Dim.Render("starting… (q to quit)")
	}
	if m.height < 6 || m.width < 24 {
		return m.styles.Dim.Render("terminal too small")
	}
	if m.showHelp {
		return m.helpView()
	}

	var b strings.Builder
	b.WriteString(m.header())
	b.WriteString("\n")
	b.WriteString(m.body())
	b.WriteString("\n")
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	right := fmt.Sprintf("%d repos", len(m.repoPaths))
	if m.dirtyOnly {
		right = "dirty only  " + right
	}
	return m.spread(m.cfg.Root, right)
}

func (m Model) body() string {
	h := m.visibleRows()
	if len(m.rows) == 0 {
		msg := "searching…"
		if !m.loading {
			msg = "no repositories to show"
			if m.dirtyOnly {
				msg = "no repositories with uncommitted changes (d to show all)"
			}
		}
		// One line for the message and h for the tree that is not there, so the
		// footer sits where it does when there are rows.
		return m.styles.Dim.Render(msg) + strings.Repeat("\n", h)
	}

	end := min(m.top+h, len(m.rows))
	l := m.newLayout(m.width, m.rows[m.top:end])
	var b strings.Builder
	b.WriteString(m.columnHeader(l))
	b.WriteString("\n")
	for i := m.top; i < end; i++ {
		b.WriteString(m.renderRow(m.rows[i], i == m.cursor, l))
		b.WriteString("\n")
	}
	// Pad to a fixed height so the footer never moves as rows come and go.
	for i := end - m.top; i < h; i++ {
		b.WriteString("\n")
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func (m Model) footer() string {
	if m.status != "" {
		return m.styles.Footer.Render(truncate(m.status, m.width))
	}
	return m.help.ShortHelpView(m.keys.ShortHelp())
}

// helpView is the whole screen while the help is open. It explains the columns
// as well as the keys, because nothing else says what "!3d in red" means.
//
// The way out of the help is on the first line rather than the last: the legend
// is longer than a short terminal, and what does not fit is dropped from the
// bottom.
func (m Model) helpView() string {
	var b strings.Builder
	b.WriteString(m.spread("gittree — keys and columns", "? or q closes this help"))
	b.WriteString("\n\n")
	b.WriteString(m.help.FullHelpView(m.keys.FullHelp()))
	b.WriteString("\n\n")
	b.WriteString(m.legend())
	b.WriteString("\n\n")
	b.WriteString(m.styles.Dim.Render("enter opens lazygit on a repository, and expands a directory"))
	return clip(b.String(), m.height)
}

// spread renders one header line with left pushed to the margin and right to
// the far edge.
func (m Model) spread(left, right string) string {
	gap := max(m.width-lipgloss.Width(left)-lipgloss.Width(right), 1)
	return m.styles.Header.Render(truncate(left, m.width) + strings.Repeat(" ", gap) + right)
}

// legendLabel is the width of the column-name gutter in the legend.
const legendLabel = 12

// legend explains what each column means, in the glyphs and colours this
// terminal is actually painting with. It is built from syncText and syncStyle
// rather than from hardcoded strings, so it cannot drift away from the rows it
// describes -- including in ASCII mode, where every symbol below is different.
func (m Model) legend() string {
	s, g := m.styles, m.glyphs
	dim := s.Dim.Render
	sync := func(state git.SyncState, ahead, behind int) string {
		return s.syncStyle(state).Render(g.syncText(state, ahead, behind))
	}

	lines := [][2]string{
		{hdrName, s.Dir.Render(g.Expanded+" directory") + dim("   ") +
			s.Repo.Render(g.Repo+" repository") + dim("   ") + s.Branch.Render(g.Branch+" branch")},

		{hdrBranch, dim("the branch the repository's worktree has checked out")},

		{hdrSync, sync(git.SyncInSync, 0, 0) + dim(" up to date   ") +
			sync(git.SyncAhead, 2, 0) + dim(" to push   ") +
			sync(git.SyncBehind, 0, 3) + dim(" to pull   ") +
			sync(git.SyncDiverged, 1, 2) + dim(" both")},
		{"", sync(git.SyncNoUpstream, 0, 0) + dim("     no upstream — an ordinary local branch, nothing wrong")},
		{"", sync(git.SyncGone, 0, 0) + dim("  the upstream branch was deleted on the remote")},
		{"", s.Stale.Render("greyed out") + dim("  the fetch below failed, so this could not be rechecked")},

		{hdrUpstream, dim("the remote-tracking branch that sync is measured against")},

		{hdrDirty, s.Clean.Render("clean") + dim(", or ") + s.Dirty.Render("+2") + dim(" staged  ") +
			s.Dirty.Render("~3") + dim(" modified  ") + s.Dirty.Render("?1") + dim(" untracked  ") +
			s.Dirty.Render("!1") + dim(" conflicted")},
		{"", dim("changes belong to a worktree, so only a checked-out branch has any")},

		{hdrAge, dim("how long ago a git fetch last ran here — sync is only as")},
		{"", dim("current as this, so f and F refetch and recheck")},
		{"", s.Dim.Render("never") + dim("  none ever has: a fresh clone, or the repo has no remote")},
		{"", s.Error.Render("3d (fetch failed: …)") + dim("  gittree's own last fetch failed and why;")},
		{"", dim("the 3d is when one last worked")},
	}

	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.styles.Header.Render(pad(l[0], legendLabel)))
		b.WriteString(l[1])
	}
	return b.String()
}

// clip drops whatever will not fit on the screen. The legend is longer than a
// short terminal, and overflowing it scrolls the top of the help off instead.
func clip(s string, height int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= height {
		return s
	}
	return strings.Join(lines[:height], "\n")
}

// Fatal reports the error that stopped the UI, if any.
func (m Model) Fatal() error { return m.fatal }
