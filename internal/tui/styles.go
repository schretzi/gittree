package tui

import (
	"image/color"
	"os"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/schretzi/gittree/internal/git"
)

// Glyphs are the symbols used to draw the tree. Two sets exist because a
// terminal without a UTF-8 locale renders the unicode ones as mojibake, which
// is worse than plain ASCII.
type Glyphs struct {
	Expanded  string
	Collapsed string
	Leaf      string
	Repo      string
	Dir       string
	Branch    string
	InSync    string
	NoUp      string
	Ahead     string
	Behind    string
	Dirty     string
}

// UnicodeGlyphs is the default set.
func UnicodeGlyphs() Glyphs {
	return Glyphs{
		Expanded: "▾", Collapsed: "▸", Leaf: " ",
		Repo: "◆", Dir: "▪", Branch: "├",
		InSync: "✓", NoUp: "—", Ahead: "↑", Behind: "↓", Dirty: "●",
	}
}

// ASCIIGlyphs is the fallback for terminals that cannot render the above.
func ASCIIGlyphs() Glyphs {
	return Glyphs{
		Expanded: "v", Collapsed: ">", Leaf: " ",
		Repo: "*", Dir: "-", Branch: "|",
		InSync: "=", NoUp: "-", Ahead: "+", Behind: "-", Dirty: "*",
	}
}

// PickGlyphs chooses a glyph set. ascii forces the fallback; otherwise the
// locale decides, since that is what determines whether the terminal can
// render the unicode set at all.
func PickGlyphs(ascii bool) Glyphs {
	if ascii || !utf8Locale() {
		return ASCIIGlyphs()
	}
	return UnicodeGlyphs()
}

func utf8Locale() bool {
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG")} {
		if v == "" {
			continue
		}
		return strings.Contains(strings.ToUpper(v), "UTF-8") || strings.Contains(strings.ToUpper(v), "UTF8")
	}
	// No locale set at all: assume a modern terminal rather than degrading.
	return true
}

// Styles holds every style the UI paints with, resolved once for the terminal's
// background so nothing has to re-decide light or dark per frame.
type Styles struct {
	Base   lipgloss.Style // no colour of its own; the tree scaffolding and padding
	Dir    lipgloss.Style
	Repo   lipgloss.Style
	Branch lipgloss.Style
	Dim    lipgloss.Style
	Header lipgloss.Style
	Footer lipgloss.Style

	// CursorBG is a colour rather than a style because the cursor is mixed
	// into each cell's own style, not wrapped around the finished row. See
	// Model.renderRow.
	CursorBG color.Color

	InSync   lipgloss.Style
	NoUp     lipgloss.Style
	Gone     lipgloss.Style
	Ahead    lipgloss.Style
	Behind   lipgloss.Style
	Diverged lipgloss.Style
	// Stale is worn by a sync state that could not be refreshed. It deliberately
	// has no colour of its own: the point is to take the confident one away.
	Stale lipgloss.Style

	Clean lipgloss.Style
	Dirty lipgloss.Style
	Error lipgloss.Style
}

// NewStyles builds the palette. dark selects the variant that stays legible on
// the terminal's actual background.
func NewStyles(dark bool) Styles {
	ld := lipgloss.LightDark(dark)

	var (
		fg     = ld(lipgloss.Color("#1a1a1a"), lipgloss.Color("#e6e6e6"))
		muted  = ld(lipgloss.Color("#6b6b6b"), lipgloss.Color("#8a8a8a"))
		accent = ld(lipgloss.Color("#0059b3"), lipgloss.Color("#7aa6ff"))
		green  = ld(lipgloss.Color("#1c7c3c"), lipgloss.Color("#5fd787"))
		yellow = ld(lipgloss.Color("#8a6d00"), lipgloss.Color("#e5c07b"))
		cyan   = ld(lipgloss.Color("#00707a"), lipgloss.Color("#56c8d8"))
		red    = ld(lipgloss.Color("#b3261e"), lipgloss.Color("#ff6b6b"))
		purple = ld(lipgloss.Color("#7a3ba8"), lipgloss.Color("#c792ea"))
	)

	base := lipgloss.NewStyle()
	return Styles{
		Base:   base,
		Dir:    base.Foreground(accent).Bold(true),
		Repo:   base.Foreground(fg).Bold(true),
		Branch: base.Foreground(fg),
		Dim:    base.Foreground(muted),
		Header: base.Foreground(muted).Bold(true),
		Footer: base.Foreground(muted),

		CursorBG: ld(lipgloss.Color("#cfe0f5"), lipgloss.Color("#2c3e5c")),

		InSync:   base.Foreground(green),
		NoUp:     base.Foreground(muted),
		Gone:     base.Foreground(red).Bold(true),
		Ahead:    base.Foreground(yellow),
		Behind:   base.Foreground(cyan),
		Diverged: base.Foreground(purple).Bold(true),
		Stale:    base.Foreground(muted),

		Clean: base.Foreground(muted),
		Dirty: base.Foreground(yellow),
		Error: base.Foreground(red),
	}
}

// syncStyle maps a sync state onto the colour that carries its urgency.
func (s Styles) syncStyle(state git.SyncState) lipgloss.Style {
	switch state {
	case git.SyncInSync:
		return s.InSync
	case git.SyncAhead:
		return s.Ahead
	case git.SyncBehind:
		return s.Behind
	case git.SyncDiverged:
		return s.Diverged
	case git.SyncGone:
		return s.Gone
	case git.SyncNoUpstream, git.SyncUnknown:
		return s.NoUp
	default:
		return s.Dim
	}
}

// syncText renders a sync state compactly enough for a column.
func (g Glyphs) syncText(state git.SyncState, ahead, behind int) string {
	switch state {
	case git.SyncInSync:
		return g.InSync
	case git.SyncAhead:
		return g.Ahead + itoa(ahead)
	case git.SyncBehind:
		return g.Behind + itoa(behind)
	case git.SyncDiverged:
		return g.Ahead + itoa(ahead) + g.Behind + itoa(behind)
	case git.SyncGone:
		return "gone"
	case git.SyncNoUpstream:
		return g.NoUp
	case git.SyncUnknown:
		return "…"
	default:
		return "?"
	}
}

// itoa avoids pulling strconv into the hot render path for small numbers.
func itoa(n int) string {
	if n < 0 {
		return "0"
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
