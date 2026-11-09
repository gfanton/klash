package model

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
)

// compactHeader draws the compact header of a session, width cells wide,
// and returns the five cells of its name and the cells of its diagonal
// lines.
func compactHeader(t *testing.T, h *header, width int) (name, lines []*uv.Cell) {
	t.Helper()
	scr := uv.NewScreenBuffer(width, 1)
	h.drawHeader(scr, scr.Bounds(), &session.Session{ID: "s1"}, true, false, width, 0, nil, "")
	var text strings.Builder
	for x := range width {
		c := scr.CellAt(x, 0)
		if c == nil {
			continue
		}
		text.WriteString(c.Content)
		switch {
		case c.Content == headerDiag:
			lines = append(lines, c)
		case c.Content == "G" && name == nil:
			name = append(name, c)
		case len(name) > 0 && len(name) < 5:
			name = append(name, c)
		}
	}
	var spelled strings.Builder
	for _, c := range name {
		spelled.WriteString(c.Content)
	}
	if spelled.String() != "GNOSH" || len(lines) < minHeaderDiags {
		t.Fatalf("the header reads %q", text.String())
	}
	return name, lines
}

func hexOf(c color.Color) string {
	if c == nil {
		return "none"
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// The compact header in Gnosh's theme. Jae, 2026-11-09: "the color of the
// letters GNOSH should blend better; and there's no fade in the ////////
// at all, even though clearly it is possible." With 24-bit color the name
// is bold and its five letters are five colors of the nebula, green first
// and gold last; its diagonal lines fade from hot pink beside the name to
// purple, at any width.
func TestCompactHeaderFades(t *testing.T) {
	s := styles.GnoshCosmos()
	com := &common.Common{Workspace: &testWorkspace{cfg: &config.Config{}}, Styles: &s, TrueColor: true}
	h := newHeader(com)

	for _, width := range []int{100, 70} {
		name, lines := compactHeader(t, h, width)
		colors := map[string]bool{}
		for i, c := range name {
			if c.Style.Attrs&uv.AttrBold == 0 {
				t.Errorf("width %d: letter %d of GNOSH is not bold", width, i)
			}
			colors[hexOf(c.Style.Fg)] = true
		}
		if len(colors) != 5 {
			t.Errorf("width %d: GNOSH's five letters show %d colors, want five", width, len(colors))
		}
		if got, want := hexOf(name[0].Style.Fg), hexOf(styles.GnoshGreen); got != want {
			t.Errorf("width %d: G is %s, want green %s", width, got, want)
		}
		if got, want := hexOf(name[4].Style.Fg), hexOf(styles.GnoshGold); got != want {
			t.Errorf("width %d: H is %s, want gold %s", width, got, want)
		}

		if got, want := hexOf(lines[0].Style.Fg), hexOf(s.Logo.FieldColorB); got != want {
			t.Errorf("width %d: the first line is %s, want the pink %s", width, got, want)
		}
		if got, want := hexOf(lines[len(lines)-1].Style.Fg), hexOf(s.Logo.FieldColor); got != want {
			t.Errorf("width %d: the last line is %s, want the purple %s", width, got, want)
		}
		shades := map[string]bool{}
		for _, c := range lines {
			shades[hexOf(c.Style.Fg)] = true
		}
		if len(shades) < len(lines)/2 {
			t.Errorf("width %d: %d lines show %d colors; they should fade", width, len(lines), len(shades))
		}
	}

	// Without 24-bit color, Gno is one green and the rest fades from
	// purple to pink, all of it bold; the lines still fade.
	com.TrueColor = false
	h.refresh()
	name, lines := compactHeader(t, h, 100)
	for i, c := range name {
		isGreen := hexOf(c.Style.Fg) == hexOf(styles.GnoshGreen)
		if i < 3 != isGreen {
			t.Errorf("without 24-bit color, letter %d is %s", i, hexOf(c.Style.Fg))
		}
		if c.Style.Attrs&uv.AttrBold == 0 {
			t.Errorf("without 24-bit color, letter %d is not bold", i)
		}
	}
	if hexOf(lines[0].Style.Fg) == hexOf(lines[len(lines)-1].Style.Fg) {
		t.Error("without 24-bit color, the lines no longer fade")
	}

	// A theme whose logo fields lie flat keeps the lines in one color, also
	// after the cached lines of the old theme.
	c := styles.CharmtonePantera()
	com.Styles = &c
	h.refresh()
	_, lines = compactHeader(t, h, 100)
	want := hexOf(c.Header.Diagonals.GetForeground())
	for i, l := range lines {
		if got := hexOf(l.Style.Fg); got != want {
			t.Fatalf("Charmtone's line %d is %s, want the one color %s", i, got, want)
		}
	}
}
