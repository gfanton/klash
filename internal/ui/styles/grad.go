package styles

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/fade"
	"github.com/rivo/uniseg"
)

// ForegroundGrad returns a slice of strings representing the input string
// rendered with a horizontal gradient foreground from color1 to color2. Each
// string in the returned slice corresponds to a grapheme cluster in the input
// string. If bold is true, the rendered strings will be bolded.
//
// The fade is spread evenly over the clusters (fade.Ramp), so even a short
// word shows every color of a fade through a *fade.Via.
func ForegroundGrad(base lipgloss.Style, input string, bold bool, color1, color2 color.Color) []string {
	if input == "" {
		return []string{""}
	}
	if bold {
		base = base.Bold(true)
	}
	if len(input) == 1 {
		return []string{base.Foreground(color1).Render(input)}
	}
	var clusters []string
	gr := uniseg.NewGraphemes(input)
	for gr.Next() {
		clusters = append(clusters, string(gr.Runes()))
	}

	for i, c := range fade.Ramp(len(clusters), color1, color2) {
		clusters[i] = base.Foreground(c).Render(clusters[i])
	}
	return clusters
}

// ApplyForegroundGrad renders a given string with a horizontal gradient
// foreground.
func ApplyForegroundGrad(base lipgloss.Style, input string, color1, color2 color.Color) string {
	if input == "" {
		return ""
	}
	var o strings.Builder
	clusters := ForegroundGrad(base, input, false, color1, color2)
	for _, c := range clusters {
		fmt.Fprint(&o, c)
	}
	return o.String()
}

// ApplyBoldWordmark renders the name in bold: with gno set, its first three
// letters, Gno, in that one color and the rest in a fade from color1 to
// color2; with gno nil, all of it in the fade.
func ApplyBoldWordmark(base lipgloss.Style, name string, gno, color1, color2 color.Color) string {
	if gno == nil || len(name) <= 3 {
		return ApplyBoldForegroundGrad(base, name, color1, color2)
	}
	return base.Bold(true).Foreground(gno).Render(name[:3]) + ApplyBoldForegroundGrad(base, name[3:], color1, color2)
}

// Wordmark returns how a wordmark that fades from color1 to color2 is drawn
// on a terminal that shows 24-bit color (trueColor) or does not. With
// 24-bit color, or a theme without Logo.GnoColor, the fade runs over the
// whole name and gno is nil; without, Gno is gno and the rest of the name
// fades from Logo.GnoRestColorA to Logo.GnoRestColorB.
func (s *Styles) Wordmark(trueColor bool, color1, color2 color.Color) (gno, from, to color.Color) {
	if trueColor || s.Logo.GnoColor == nil {
		return nil, color1, color2
	}
	return s.Logo.GnoColor, s.Logo.GnoRestColorA, s.Logo.GnoRestColorB
}

// WordmarkField colors the diagonal lines that follow a one-line wordmark,
// in the compact header and the sidebar's small logo. In a theme whose logo
// fields fade (Logo.FieldColorB set), these fade too: from FieldColorB
// beside the name to Logo.FieldColor at the far end. Otherwise they are
// all in base's color.
func (s *Styles) WordmarkField(base lipgloss.Style, lines string) string {
	if s.Logo.FieldColorB == nil {
		return base.Render(lines)
	}
	return ApplyForegroundGrad(base, lines, s.Logo.FieldColorB, s.Logo.FieldColor)
}

// ApplyBoldForegroundGrad renders a given string with a horizontal gradient
// foreground.
func ApplyBoldForegroundGrad(base lipgloss.Style, input string, color1, color2 color.Color) string {
	if input == "" {
		return ""
	}
	var o strings.Builder
	clusters := ForegroundGrad(base, input, true, color1, color2)
	for _, c := range clusters {
		fmt.Fprint(&o, c)
	}
	return o.String()
}
