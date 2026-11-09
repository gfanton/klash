package logo

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/crush/internal/version"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestWordUnstretchedAndMarkUnder(t *testing.T) {
	purple := color.RGBA{R: 0x6b, G: 0x50, B: 0xff, A: 0xff}
	o := Opts{FieldColor: purple, TitleColorA: purple, TitleColorB: purple, CharmColor: purple, VersionColor: purple, Width: 120}
	// The word's first row exactly as letterforms.go draws it, no letter widened.
	const firstRow = "\u2584\u2580\u2580\u2580\u2580 \u2588\u2584  \u2588 \u2584\u2580\u2580\u2580\u2584 \u2584\u2580\u2580\u2580\u2580 \u2588   \u2588"
	for _, compact := range []bool{true, false} {
		for range 3 {
			lines := strings.Split(ansi.Strip(Render(lipgloss.NewStyle(), "v9.9", compact, o)), "\n")
			word, mark := -1, -1
			for i, l := range lines {
				if word < 0 && strings.Contains(l, firstRow) {
					word = i
				}
				if strings.Contains(l, Mark) {
					mark = i
				}
			}
			if word < 0 {
				t.Fatalf("compact=%v: the word is missing or stretched:\n%s", compact, strings.Join(lines, "\n"))
			}
			if mark != word+3 {
				t.Fatalf("compact=%v: %s is on line %d, want %d, under the word's three rows", compact, Mark, mark, word+3)
			}
		}
	}
}

func TestTitleRowGnoIsOneColor(t *testing.T) {
	green := color.RGBA{R: 0x00, G: 0xff, B: 0xb2, A: 0xff}
	purple := color.RGBA{R: 0x6b, G: 0x50, B: 0xff, A: 0xff}
	pink := color.RGBA{R: 0xff, G: 0x60, B: 0xff, A: 0xff}
	o := Opts{GnoColor: green, TitleColorA: purple, TitleColorB: pink}
	row := strings.Split(renderWord(1, -1, LetterG, LetterN, LetterO, LetterSAlt, LetterH), "\n")[0]
	gnoWidth := lipgloss.Width(renderWord(1, -1, LetterG, LetterN, LetterO))

	scr := uv.NewScreenBuffer(lipgloss.Width(row), 1)
	uv.NewStyledString(titleRow(lipgloss.NewStyle(), row, gnoWidth, o)).Draw(scr, scr.Bounds())
	for x := range lipgloss.Width(row) {
		cell := scr.CellAt(x, 0)
		if cell == nil || strings.TrimSpace(cell.Content) == "" {
			continue
		}
		isGreen := colorEqual(cell.Style.Fg, green)
		if x < gnoWidth && !isGreen {
			t.Errorf("cell %d, in G, N and O, is %v, want the one green", x, cell.Style.Fg)
		}
		if x >= gnoWidth && isGreen {
			t.Errorf("cell %d, in S and H, is the green of Gno", x)
		}
	}
}

func colorEqual(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	return ar == br && ag == bg && ab == bb
}

// The wide logo's rows all end in the same column: the field to the right
// of the word does not step in row by row.
func TestWideLogoRightEdgeIsStraight(t *testing.T) {
	purple := color.RGBA{R: 0x6b, G: 0x50, B: 0xff, A: 0xff}
	o := Opts{FieldColor: purple, TitleColorA: purple, TitleColorB: purple, CharmColor: purple, VersionColor: purple, Width: 120}
	lines := strings.Split(strings.TrimRight(ansi.Strip(Render(lipgloss.NewStyle(), "v9.9", false, o)), "\n"), "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue // the blank row under the logo
		}
		if w := lipgloss.Width(strings.TrimRight(l, " ")); w != lipgloss.Width(strings.TrimRight(lines[0], " ")) {
			t.Fatalf("row %d ends at column %d, row 0 at %d:\n%s", i, w, lipgloss.Width(lines[0]), strings.Join(lines, "\n"))
		}
	}
}

// The sidebar's small logo fades its lines from hot pink beside the name to
// purple in Gnosh's theme, as the compact header does, and keeps them one
// color in Charmtone.
func TestSmallLogoLinesFade(t *testing.T) {
	linesOf := func(st *styles.Styles) []color.Color {
		s := SmallRender(st, 40, Opts{})
		scr := uv.NewScreenBuffer(lipgloss.Width(s), 1)
		uv.NewStyledString(s).Draw(scr, scr.Bounds())
		var fg []color.Color
		for x := range lipgloss.Width(s) {
			if c := scr.CellAt(x, 0); c != nil && c.Content == diag {
				fg = append(fg, c.Style.Fg)
			}
		}
		if len(fg) < 20 {
			t.Fatalf("the small logo has %d lines: %q", len(fg), ansi.Strip(s))
		}
		return fg
	}

	k := styles.GnoshCosmos()
	fg := linesOf(&k)
	if !colorEqual(fg[0], k.Logo.FieldColorB) || !colorEqual(fg[len(fg)-1], k.Logo.FieldColor) {
		t.Errorf("Gnosh's lines run %v to %v, want %v to %v", fg[0], fg[len(fg)-1], k.Logo.FieldColorB, k.Logo.FieldColor)
	}

	c := styles.CharmtonePantera()
	for i, f := range linesOf(&c) {
		if !colorEqual(f, c.Logo.SmallDiagonals.GetForeground()) {
			t.Fatalf("Charmtone's line %d is %v, want its one color", i, f)
		}
	}
}

// The sidebar's small logo spells the name in capitals, as the big logo and
// the compact header do.
func TestSmallLogoNameIsCapitals(t *testing.T) {
	st := styles.CharmtonePantera()
	want := strings.ToUpper(version.Name) + " "
	if got := ansi.Strip(SmallRender(&st, 40, Opts{})); !strings.HasPrefix(got, want) {
		t.Fatalf("the small logo is %q, want it to start with %q", got, want)
	}
}
