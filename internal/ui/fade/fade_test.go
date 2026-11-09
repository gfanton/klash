package fade

import (
	"image/color"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/lucasb-eyer/go-colorful"
)

var (
	green  = color.RGBA{G: 0xff, A: 0xff}
	purple = color.RGBA{R: 0x6b, G: 0x50, B: 0xff, A: 0xff}
	pink   = color.RGBA{R: 0xff, G: 0x60, B: 0xff, A: 0xff}
	gold   = color.RGBA{R: 0xfb, G: 0xd2, B: 0x37, A: 0xff}
)

func TestStops(t *testing.T) {
	end := &Via{Color: gold, Through: []color.Color{purple, pink}}
	for _, tc := range []struct {
		name  string
		stops []color.Color
		want  []color.Color
	}{
		{"plain colors pass as they are", []color.Color{green, gold}, []color.Color{green, gold}},
		{"arriving passes through in order", []color.Color{green, end}, []color.Color{green, purple, pink, gold}},
		{"leaving passes through in reverse", []color.Color{end, green}, []color.Color{gold, pink, purple, green}},
		{"in the middle, both ways", []color.Color{green, end, green}, []color.Color{green, purple, pink, gold, pink, purple, green}},
		{"alone, just its color", []color.Color{end}, []color.Color{gold}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Stops(tc.stops...)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d stops %v, want %d %v", len(got), got, len(tc.want), tc.want)
			}
			for i := range got {
				if !same(got[i], tc.want[i]) {
					t.Errorf("stop %d is %v, want %v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestViaIsItsColor(t *testing.T) {
	v := &Via{Color: gold, Through: []color.Color{purple}}
	if !same(v, gold) {
		t.Errorf("a *Via reads as %v, want its own color %v", v, gold)
	}
	// Comparing colors must not panic, as it would for a Via held by value.
	var a, b color.Color = v, &Via{Color: gold}
	if a == b {
		t.Error("two different *Via compared equal")
	}
}

func same(a, b color.Color) bool {
	r1, g1, b1, a1 := a.RGBA()
	r2, g2, b2, a2 := b.RGBA()
	return r1 == r2 && g1 == g2 && b1 == b2 && a1 == a2
}

// A short run over many stops still shows the whole fade, every cell in a
// color of its own. lipgloss.Blend1D drew GNOSH's five letters over the
// nebula green, purple, purple, pink, pink, and never reached gold.
func TestRampSpreadsTheFadeEvenly(t *testing.T) {
	nebula := []color.Color{green, &Via{Color: gold, Through: []color.Color{purple, pink}}}

	got := Ramp(5, nebula...)
	if len(got) != 5 {
		t.Fatalf("got %d colors, want 5", len(got))
	}
	if !same(got[0], green) || !same(got[4], gold) {
		t.Errorf("the ends are %v and %v, want green and gold", got[0], got[4])
	}
	for i := range got {
		for j := range i {
			if same(got[i], got[j]) {
				t.Errorf("cells %d and %d are both %v", j, i, got[i])
			}
		}
	}
	// The middle cell is halfway along, halfway from purple to pink.
	p, _ := colorful.MakeColor(purple)
	k, _ := colorful.MakeColor(pink)
	if want := p.BlendLab(k, 0.5).Clamped(); !same(got[2], want) {
		t.Errorf("the middle cell is %v, want %v, halfway from purple to pink", got[2], want)
	}

	// Seven cells give each of the three stretches two steps, so every
	// other cell is a stop.
	got = Ramp(7, nebula...)
	for i, want := range map[int]color.Color{0: green, 2: purple, 4: pink, 6: gold} {
		if !same(got[i], want) {
			t.Errorf("cell %d of 7 is %v, want the stop %v", i, got[i], want)
		}
	}
}

// Ramp changes no fade between two colors: those come out as
// lipgloss.Blend1D draws them.
func TestRampOfTwoColorsIsBlend1D(t *testing.T) {
	for n := 1; n <= 40; n++ {
		got, want := Ramp(n, purple, pink), lipgloss.Blend1D(n, purple, pink)
		if len(got) != len(want) {
			t.Fatalf("n=%d: got %d colors, want %d", n, len(got), len(want))
		}
		for i := range got {
			if !same8(got[i], want[i]) {
				t.Errorf("n=%d: color %d is %v, Blend1D's %v", n, i, got[i], want[i])
			}
		}
	}
}

func TestRampEdges(t *testing.T) {
	if got := Ramp(0, green, gold); got != nil {
		t.Errorf("no cells gave %v, want nil", got)
	}
	if got := Ramp(3); got != nil {
		t.Errorf("no stops gave %v, want nil", got)
	}
	if got := Ramp(1, green, gold); len(got) != 1 || !same(got[0], green) {
		t.Errorf("one cell gave %v, want the first stop", got)
	}
	// Missing stops are left out, as Blend1D leaves them out.
	for i, c := range Ramp(3, nil, green, nil) {
		if !same(c, green) {
			t.Errorf("cell %d is %v, want the one stop given", i, c)
		}
	}
}

// same8 compares two colors as a terminal is sent them, 8 bits a channel.
func same8(a, b color.Color) bool {
	r1, g1, b1, _ := a.RGBA()
	r2, g2, b2, _ := b.RGBA()
	return r1>>8 == r2>>8 && g1>>8 == g2>>8 && b1>>8 == b2>>8
}
