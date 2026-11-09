// Package fade lets a color carry the colors a fade passes through on its
// way to it, so a theme can make every two-color fade in the UI a longer
// one without each place that draws a fade knowing about it. Ramp spreads
// such a fade evenly over a row of cells.
package fade

import (
	"image/color"
	"slices"

	"github.com/lucasb-eyer/go-colorful"
)

// Via is a color that brings company: a fade toward it passes through
// Through first, in order, and a fade away from it passes through them in
// reverse. Anywhere else it is just its Color.
//
// Use a *Via, so the color stays comparable.
type Via struct {
	color.Color
	Through []color.Color
}

// Stops returns the stops of a fade through the given colors, with every
// *Via's company put in: before it when a fade arrives at it, and after it,
// reversed, when a fade leaves it. Colors that are not a *Via pass as they
// are.
func Stops(stops ...color.Color) []color.Color {
	out := make([]color.Color, 0, len(stops))
	for i, c := range stops {
		v, ok := c.(*Via)
		if !ok || v == nil {
			out = append(out, c)
			continue
		}
		if i > 0 {
			out = append(out, v.Through...)
		}
		out = append(out, v.Color)
		if i < len(stops)-1 {
			for j := len(v.Through) - 1; j >= 0; j-- {
				out = append(out, v.Through[j])
			}
		}
	}
	return out
}

// Ramp returns n colors spread evenly along a fade through the given
// colors, every *Via's company put in (see Stops): the first is the first
// stop, the last is the last stop, and each stretch between two stops gets
// an equal share of the way. The colors blend in CIELAB, as
// lipgloss.Blend1D blends them, and a fade between two colors comes out as
// Blend1D draws it.
//
// Ramp exists because Blend1D gives each stretch a whole number of the
// colors, each stretch starting and ending on its stops, so a short fade
// over many stops shows little but the stops: over green, purple, hot pink
// and gold, the five letters of GNOSH came out green, purple, purple, pink,
// pink, and gold not at all.
func Ramp(n int, stops ...color.Color) []color.Color {
	stops = slices.DeleteFunc(Stops(stops...), func(c color.Color) bool { return c == nil })
	if n <= 0 || len(stops) == 0 {
		return nil
	}
	out := make([]color.Color, n)
	if n == 1 || len(stops) == 1 {
		for i := range out {
			out[i] = stops[0]
		}
		return out
	}
	lab := make([]colorful.Color, len(stops))
	for i, c := range stops {
		// A transparent stop reads as black, as it does in Blend1D.
		lab[i], _ = colorful.MakeColor(c)
	}
	last := len(stops) - 1
	for i := range out {
		// How far along the fade the color is, counted in stretches: 0 at
		// the first stop, last at the last one.
		at := float64(i*last) / float64(n-1)
		k := min(int(at), last-1)
		switch t := at - float64(k); t {
		case 0:
			out[i] = stops[k]
		case 1:
			out[i] = stops[k+1]
		default:
			out[i] = lab[k].BlendLab(lab[k+1], t).Clamped()
		}
	}
	return out
}
