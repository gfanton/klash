package styles

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// cells draws a one-line string and returns its cells, one a column.
func cells(s string) []*uv.Cell {
	w := lipgloss.Width(s)
	scr := uv.NewScreenBuffer(w, 1)
	uv.NewStyledString(s).Draw(scr, scr.Bounds())
	out := make([]*uv.Cell, w)
	for x := range w {
		out[x] = scr.CellAt(x, 0)
	}
	return out
}

// A bold fade is bold in every cell, and a plain one in none. Charm's
// ForegroundGrad set Bold on a copy of the style that it then dropped, so
// GNOSH in the compact header was never bold.
func TestForegroundGradBold(t *testing.T) {
	s := GnoshCosmos()
	from, to := s.Header.LogoGradFromColor, s.Header.LogoGradToColor
	for _, word := range []string{"G", "GNOSH"} {
		for x, c := range cells(ApplyBoldForegroundGrad(lipgloss.NewStyle(), word, from, to)) {
			if c.Style.Attrs&uv.AttrBold == 0 {
				t.Errorf("%s: cell %d is not bold", word, x)
			}
		}
		for x, c := range cells(ApplyForegroundGrad(lipgloss.NewStyle(), word, from, to)) {
			if c.Style.Attrs&uv.AttrBold != 0 {
				t.Errorf("%s: cell %d of a plain fade is bold", word, x)
			}
		}
	}
}

// The five letters of the compact header's GNOSH show the whole nebula,
// each its own color, green first and gold last. lipgloss.Blend1D colored
// them green, purple, purple, pink, pink.
func TestShortWordmarkShowsTheWholeNebula(t *testing.T) {
	s := GnoshCosmos()
	cs := cells(ApplyBoldForegroundGrad(lipgloss.NewStyle(), "GNOSH", s.Header.LogoGradFromColor, s.Header.LogoGradToColor))
	got := make([]string, len(cs))
	for i, c := range cs {
		got[i] = *hex(c.Style.Fg)
	}
	if got[0] != *hex(GnoshGreen) || got[4] != *hex(GnoshGold) {
		t.Errorf("GNOSH runs %v, want it to start green %s and end gold %s", got, *hex(GnoshGreen), *hex(GnoshGold))
	}
	for i := range got {
		for j := range i {
			if got[i] == got[j] {
				t.Errorf("letters %d and %d are both %s: %v", j, i, got[i], got)
			}
		}
	}
}

// The lines after a one-line wordmark fade from hot pink beside the name
// to purple where Gnosh's logo fields fade, and keep the style's one color
// in a theme whose fields lie flat.
func TestWordmarkField(t *testing.T) {
	lines := strings.Repeat("╱", 30)

	k := GnoshCosmos()
	cs := cells(k.WordmarkField(k.Header.Diagonals, lines))
	if got, want := *hex(cs[0].Style.Fg), *hex(k.Logo.FieldColorB); got != want {
		t.Errorf("the first line is %s, want %s, the pink beside the name", got, want)
	}
	if got, want := *hex(cs[len(cs)-1].Style.Fg), *hex(k.Logo.FieldColor); got != want {
		t.Errorf("the last line is %s, want %s, the purple", got, want)
	}
	seen := map[string]bool{}
	for _, c := range cs {
		seen[*hex(c.Style.Fg)] = true
	}
	if len(seen) < len(cs)/2 {
		t.Errorf("30 lines show only %d colors; they should fade", len(seen))
	}

	c := CharmtonePantera()
	want := *hex(c.Header.Diagonals.GetForeground())
	for x, cell := range cells(c.WordmarkField(c.Header.Diagonals, lines)) {
		if got := *hex(cell.Style.Fg); got != want {
			t.Fatalf("Charmtone's line %d is %s, want the style's one color %s", x, got, want)
		}
	}
}
