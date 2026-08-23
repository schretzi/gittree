package tui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/schretzi/gittree/internal/git"
	"github.com/schretzi/gittree/internal/tree"
)

// Column widths for the fixed-size fields. The name column takes whatever is
// left, which is what makes the tree readable at any terminal size.
const (
	colBranch   = 18
	colSync     = 9
	colUpstream = 16
	colDirty    = 13
	colAge      = 7
	colGap      = 2
	minName     = 16

	// colAgeFailure is the room the fetch column may take when it is carrying
	// the reason a fetch failed. It is the only column sized to a sentence
	// rather than a field, which is why it is also the first to give width
	// back -- see newLayout.
	colAgeFailure = 42
)

// Column headings. Nothing labels these columns otherwise, and "!3d in red" is
// not self-evident, so the headings are drawn above the tree and explained in
// the help. They are measured into the layout as well as drawn, so a column can
// never come out narrower than its own label.
const (
	hdrName     = "repository"
	hdrBranch   = "branch"
	hdrSync     = "sync"
	hdrUpstream = "upstream"
	hdrDirty    = "local"
	hdrAge      = "fetched"
)

// layout records which columns are drawn and how wide each one is.
//
// Widths come from the content actually on screen, capped at the constants
// above. Sizing to the terminal instead leaves a gulf of padding between the
// name and the data on a wide window.
//
// When the window is too narrow, whole columns are given up rather than every
// column being squeezed: the fetch age is the least urgent thing on a row, and
// the repository name is the one piece that has to survive to the end.
type layout struct {
	branch, sync, upstream, dirty, age int // 0 means the column is not drawn
	name                               int
}

func (l layout) fixed() int {
	n := 0
	for _, w := range []int{l.branch, l.sync, l.upstream, l.dirty, l.age} {
		if w > 0 {
			n += w + colGap
		}
	}
	return n
}

// drop gives up the least valuable remaining column, reporting whether there
// was anything left to give up.
func (l *layout) drop() bool {
	switch {
	case l.age > 0:
		l.age = 0
	case l.upstream > 0:
		l.upstream = 0
	case l.dirty > 0:
		l.dirty = 0
	case l.branch > 0:
		l.branch = 0
	case l.sync > 0:
		l.sync = 0
	default:
		return false
	}
	return true
}

// newLayout measures the rows that will be drawn and fits them into width.
func (m *Model) newLayout(width int, rows []tree.Row) layout {
	l := layout{
		branch:   lipgloss.Width(hdrBranch),
		sync:     lipgloss.Width(hdrSync),
		upstream: lipgloss.Width(hdrUpstream),
		dirty:    lipgloss.Width(hdrDirty),
		age:      lipgloss.Width(hdrAge),
	}
	needed := lipgloss.Width(hdrName)
	for _, row := range rows {
		label := row.Node.Name
		if row.Kind == tree.RowBranch && row.Branch != nil {
			label = row.Branch.Name
		}
		// Two cells per level of indent, plus the marker, icon and their spaces.
		needed = max(needed, row.Depth*2+4+lipgloss.Width(label))

		branch, sync, upstream, dirty, age := m.cells(row)
		l.branch = max(l.branch, lipgloss.Width(branch.text))
		l.sync = max(l.sync, lipgloss.Width(sync.text))
		l.upstream = max(l.upstream, lipgloss.Width(upstream.text))
		l.dirty = max(l.dirty, lipgloss.Width(dirty.text))
		l.age = max(l.age, lipgloss.Width(age.text))
	}
	l.branch = min(l.branch, colBranch)
	l.sync = min(l.sync, colSync)
	l.upstream = min(l.upstream, colUpstream)
	l.dirty = min(l.dirty, colDirty)
	l.age = min(l.age, colAgeFailure)

	// The failure reason is a sentence, so it is what gives width back first: a
	// truncated "3d (fetch failed…" still says more than dropping the column
	// would, and squeezing it costs nothing on a row that is only showing "3d".
	if over := minName - (width - l.fixed()); over > 0 {
		l.age = max(l.age-over, min(l.age, colAge))
	}
	for width-l.fixed() < minName {
		if !l.drop() {
			break
		}
	}
	available := max(width-l.fixed(), 4)
	l.name = min(max(needed, minName), available)
	return l
}

// decorator adjusts a cell's style just before it renders. It is how the cursor
// highlight reaches inside a row.
type decorator func(lipgloss.Style) lipgloss.Style

func plain(s lipgloss.Style) lipgloss.Style { return s }

// highlight returns the decorator that paints the row under the cursor.
func (m *Model) highlight() decorator {
	return func(s lipgloss.Style) lipgloss.Style {
		return s.Background(m.styles.CursorBG).Bold(true)
	}
}

// columnHeader labels the fixed columns. It uses the layout the rows use, so a
// column dropped for want of width loses its heading with it.
func (m *Model) columnHeader(l layout) string {
	h := m.styles.Header
	var b strings.Builder
	b.WriteString(h.Render(pad(truncate(hdrName, l.name), l.name)))
	m.writeCell(&b, l.branch, cell{hdrBranch, h}, plain)
	m.writeCell(&b, l.sync, cell{hdrSync, h}, plain)
	m.writeCell(&b, l.upstream, cell{hdrUpstream, h}, plain)
	m.writeCell(&b, l.dirty, cell{hdrDirty, h}, plain)
	m.writeCell(&b, l.age, cell{hdrAge, h}, plain)
	return strings.TrimRight(b.String(), " ")
}

// renderRow draws one line of the tree.
//
// The cursor is mixed into every cell's own style rather than wrapped around
// the finished line. Each cell renders its own colour and closes with a reset,
// so a background applied to the outside would stop dead at the first of them
// -- which is what made the selection invisible.
func (m *Model) renderRow(row tree.Row, selected bool, l layout) string {
	deco := plain
	if selected {
		deco = m.highlight()
	}

	var b strings.Builder
	b.WriteString(m.treeCell(row, l.name, deco))

	branch, sync, upstream, dirty, age := m.cells(row)
	m.writeCell(&b, l.branch, branch, deco)
	m.writeCell(&b, l.sync, sync, deco)
	m.writeCell(&b, l.upstream, upstream, deco)
	m.writeCell(&b, l.dirty, dirty, deco)
	m.writeCell(&b, l.age, age, deco)

	if !selected {
		return strings.TrimRight(b.String(), " ")
	}
	// The highlight has to reach the edge of the window, so the tail is
	// extended rather than trimmed.
	line := b.String()
	if gap := m.width - lipgloss.Width(line); gap > 0 {
		line += deco(m.styles.Base).Render(strings.Repeat(" ", gap))
	}
	return line
}

type cell struct {
	text  string
	style lipgloss.Style
}

func (m *Model) writeCell(b *strings.Builder, w int, c cell, deco decorator) {
	if w <= 0 {
		return
	}
	b.WriteString(deco(m.styles.Base).Render(strings.Repeat(" ", colGap)))
	b.WriteString(deco(c.style).Render(pad(truncate(c.text, w), w)))
}

// treeCell draws the indent, the expansion marker, the icon and the label,
// padded to exactly w display cells so the fixed columns line up.
func (m *Model) treeCell(row tree.Row, w int, deco decorator) string {
	marker := m.glyphs.Leaf
	if row.Node.HasChildren() && row.Kind != tree.RowBranch {
		marker = m.glyphs.Collapsed
		if row.Node.Expanded {
			marker = m.glyphs.Expanded
		}
	}

	var icon string
	var style lipgloss.Style
	label := row.Node.Name
	switch row.Kind {
	case tree.RowDir:
		icon, style = m.glyphs.Dir, m.styles.Dir
	case tree.RowRepo:
		icon, style = m.glyphs.Repo, m.styles.Repo
	case tree.RowBranch:
		icon, style = m.glyphs.Branch, m.styles.Branch
		if row.Branch != nil {
			label = row.Branch.Name
		}
	}

	head := strings.Repeat("  ", row.Depth) + marker + " " + icon + " "
	room := max(w-lipgloss.Width(head), 1)
	return deco(m.styles.Base).Render(head) + deco(style).Render(pad(truncate(label, room), room))
}

// cells produces the five fixed columns for a row.
//
// A repository whose last fetch failed gets its sync state greyed out, whatever
// that state is: see staleSync.
func (m *Model) cells(row tree.Row) (branch, sync, upstream, dirty, age cell) {
	branch, sync, upstream, dirty, age = m.rawCells(row)
	if m.staleSync(row) {
		sync.style = m.styles.Stale
	}
	return branch, sync, upstream, dirty, age
}

// staleSync reports whether a row's sync state is a guess rather than a fact.
//
// ahead/behind is local refs against remote-tracking refs, and remote-tracking
// refs are precisely what a failed fetch did not update. A green tick next to a
// red "fetch failed" claims the branch was checked when it was not, so the
// value stays -- it is still the best answer available, as of whatever the age
// column says -- and the colour that asserts it was just verified goes.
//
// Branch rows carry their repository's node, so this covers them too.
func (m *Model) staleSync(row tree.Row) bool {
	if row.Kind == tree.RowDir {
		return false
	}
	_, failed := m.fetchErrs[row.Node.Path]
	return failed
}

func (m *Model) rawCells(row tree.Row) (branch, sync, upstream, dirty, age cell) {
	dim := m.styles.Dim
	switch row.Kind {
	case tree.RowDir:
		return cell{"", dim}, cell{"", dim}, cell{"", dim}, cell{"", dim}, cell{"", dim}

	case tree.RowBranch:
		b := row.Branch
		branch = cell{"", dim} // the label already sits in the tree column
		sync = cell{m.glyphs.syncText(b.Sync, b.Ahead, b.Behind), m.styles.syncStyle(b.Sync)}
		upstream = cell{b.Upstream, dim}
		dirty = m.dirtyCell(b.Status)
		age = cell{"", dim}
		return branch, sync, upstream, dirty, age

	default: // tree.RowRepo
		r := row.Node.Repo
		if r == nil {
			return cell{"…", dim}, cell{"", dim}, cell{"", dim}, cell{"", dim}, cell{"", dim}
		}
		if r.Err != nil {
			return cell{"error", m.styles.Error}, cell{"", dim}, cell{"", dim}, cell{"", dim}, cell{"", dim}
		}
		branch, sync, upstream = m.repoBranchCells(r)
		dirty = m.dirtyCell(&r.Status)
		age = cell{git.HumanAge(r.LastFetch), dim}
		// A failed fetch is reported against its own row rather than announced:
		// on a tree of any size, one unreachable remote must not produce a wall
		// of messages. It says why in words, because a marker means nothing
		// without a legend somewhere else to explain it.
		if err, failed := m.fetchErrs[r.Dir]; failed {
			age = cell{age.text + " (fetch failed: " + git.FetchFailure(err) + ")", m.styles.Error}
		}
		return branch, sync, upstream, dirty, age
	}
}

// repoBranchCells summarises the checked-out branch on the repository's own
// row, so a collapsed repository still says everything it has to say.
func (m *Model) repoBranchCells(r *git.Repo) (branch, sync, upstream cell) {
	dim := m.styles.Dim
	head := r.Head()
	switch {
	case head != nil:
		return cell{head.Name, m.styles.Branch},
			cell{m.glyphs.syncText(head.Sync, head.Ahead, head.Behind), m.styles.syncStyle(head.Sync)},
			cell{head.Upstream, dim}
	case r.Status.Unborn:
		// No commits yet, so there is no ref -- but the branch name is real.
		return cell{r.Status.Head, m.styles.Branch}, cell{"unborn", dim}, cell{"", dim}
	default:
		return cell{"(detached)", dim}, cell{"", dim}, cell{"", dim}
	}
}

// dirtyCell renders the working tree's state, or nothing at all for a branch
// that is not checked out anywhere and so cannot be dirty.
func (m *Model) dirtyCell(s *git.Status) cell {
	if s == nil {
		return cell{"", m.styles.Dim}
	}
	if !s.Dirty() {
		return cell{"clean", m.styles.Clean}
	}
	var parts []string
	for _, p := range []struct {
		n   int
		sym string
	}{{s.Staged, "+"}, {s.Modified, "~"}, {s.Untracked, "?"}, {s.Conflicted, "!"}} {
		if p.n > 0 {
			parts = append(parts, p.sym+itoa(p.n))
		}
	}
	return cell{strings.Join(parts, " "), m.styles.Dirty}
}

// pad right-pads to a display width, measured in cells rather than bytes so
// non-ASCII glyphs line up.
func pad(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// truncate shortens to a display width, marking the cut with an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	runes := []rune(s)
	for len(runes) > 0 {
		candidate := string(runes) + "…"
		if lipgloss.Width(candidate) <= w {
			return candidate
		}
		runes = runes[:len(runes)-1]
	}
	return "…"
}
