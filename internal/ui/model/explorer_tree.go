package model

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---- Columns

// treeColumns is how the Tree tab splits its width after the selection's
// gutter: the tree's labels, the bars on the time axis, and the column of
// what each pop freed. bars is 0 where they would get fewer than
// minTreeBars columns.
type treeColumns struct {
	label, bars, right int
}

const (
	minTreeBars  = 16
	minTreeLabel = 18
	maxTreeLabel = 36
	// treeRight is as wide as "−13.9K tok".
	treeRight = 10
	// treeGutter is the selection's mark and its space.
	treeGutter = 2
)

// treeColumnsFor splits inner columns for rows: the labels as wide as the
// widest needs, within bounds, and the bars what is left.
func treeColumnsFor(rows []explorerRow, inner int) treeColumns {
	widest := 0
	for _, r := range rows {
		widest = max(widest, lipgloss.Width(r.lead)+2+lipgloss.Width(r.name()))
	}
	label := min(max(widest+1, minTreeLabel), maxTreeLabel)
	bars := inner - treeGutter - label - 1 - treeRight
	if bars < minTreeBars {
		return treeColumns{}
	}
	return treeColumns{label: label, bars: bars, right: treeRight}
}

// ---- Time axis

// timeAxis maps Unix seconds from..to onto width columns.
type timeAxis struct {
	from, to int64
	width    int
}

// treeAxis is the time axis of the frames under roots: from the first push
// to the last row, or to now while the agent works.
func (f sessionFrames) treeAxis(roots []*frameNode, look stackLook, width int) timeAxis {
	a := timeAxis{width: width}
	if len(roots) > 0 {
		a.from = roots[0].since
	}
	a.to = a.from
	eachFrame(roots, func(n *frameNode) { a.to = max(a.to, n.since, n.until) })
	for _, r := range f.rows {
		a.to = max(a.to, r.CreatedAt)
	}
	if look.busy && !look.now.IsZero() {
		a.to = max(a.to, look.now.Unix())
	}
	if a.to <= a.from {
		a.to = a.from + 1
	}
	return a
}

// x is the column of Unix second t.
func (a timeAxis) x(t int64) int {
	x := int(math.Round(float64(t-a.from) * float64(a.width-1) / float64(a.to-a.from)))
	return min(max(x, 0), a.width-1)
}

// at is the Unix second column x stands for.
func (a timeAxis) at(x int) int64 {
	if a.width <= 1 {
		return a.to
	}
	return a.from + int64(math.Round(float64(x)*float64(a.to-a.from)/float64(a.width-1)))
}

// tickSteps is the spans between the axis's labels, in seconds, the
// shortest first.
var tickSteps = []int64{1, 2, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400}

// minTickGap is the fewest columns between two labels' starts.
const minTickGap = 10

// labels is the axis's labels, width wide: under a minute apart, the time
// since its start (0s, 10s); else the time of day, on round minutes.
func (a timeAxis) labels() string {
	step := tickSteps[len(tickSteps)-1]
	for _, s := range tickSteps {
		if float64(s)*float64(a.width-1)/float64(a.to-a.from) >= minTickGap {
			step = s
			break
		}
	}
	line := []rune(strings.Repeat(" ", a.width))
	first := a.from
	if step >= 60 {
		first = (a.from + step - 1) / step * step
	}
	free := 0
	for t := first; t <= a.to; t += step {
		label := time.Unix(t, 0).Format("15:04")
		if step < 60 {
			label = runningFor(time.Duration(t-a.from) * time.Second)
		}
		x := a.x(t)
		if x < free || x+len(label) > a.width {
			continue
		}
		copy(line[x:], []rune(label))
		free = x + len(label) + 1
	}
	return string(line)
}

// ---- Slides

// frameSlide is a slide: the frame whose rounds it took out of the
// model's context, about when, and what it took.
type frameSlide struct {
	mark, frame string
	// at is when the first row after those it took came, or the last of
	// them where none did: its mark says what left and not when.
	at     int64
	rows   int
	tokens int64
}

// slideEnds is, per slide mark, where the first row after those it took
// stands among the session's rows.
func (f sessionFrames) slideEnds() map[string]int {
	ends := map[string]int{}
	for i, r := range f.rows {
		if strings.HasPrefix(r.Popped, slideMarkPrefix) {
			ends[r.Popped] = i + 1
		}
	}
	return ends
}

// slides is the session's slides, the earliest first.
func (f sessionFrames) slides() []frameSlide {
	var slides []frameSlide
	for mark, end := range f.slideEnds() {
		frame, _, _ := strings.Cut(strings.TrimPrefix(mark, slideMarkPrefix), ":")
		s := frameSlide{mark: mark, frame: frame, at: f.rows[min(end, len(f.rows)-1)].CreatedAt}
		for _, r := range f.rows[:end] {
			if r.Popped == mark {
				s.rows++
				s.tokens += rowTokens(r)
			}
		}
		slides = append(slides, s)
	}
	slices.SortFunc(slides, func(a, b frameSlide) int { return cmp.Compare(a.at, b.at) })
	return slides
}

// ---- Layout

// treeLayout is how the Tree tab spends an explorer's rows and columns.
type treeLayout struct {
	cols treeColumns
	// chart is the rows of the context chart; head the rows above the
	// tree: the chart, the marks and the axis.
	chart, head int
	// tree and detail are the rows of the tree and of the detail pane.
	tree, detail int
}

const (
	// tallExplorer is the shortest explorer that draws the context chart.
	tallExplorer  = 36
	contextRows   = 4
	minTreeInView = 3
)

// treeLayoutFor lays the Tree tab out for rows in an explorer inner wide
// and height tall: the marks and the axis come with the bars, the chart
// only when the explorer is tall, each only while the tree keeps
// minTreeInView rows; the detail pane takes the rows the tree does not
// need.
func treeLayoutFor(rows []explorerRow, inner, height int) treeLayout {
	l := treeLayout{cols: treeColumnsFor(rows, inner)}
	l.tree, l.detail = explorerTreeHeights(height)
	if l.cols.bars > 0 {
		switch {
		case height >= tallExplorer && l.tree-contextRows-2 >= minTreeInView:
			l.chart, l.head = contextRows, contextRows+2
		case l.tree-2 >= minTreeInView:
			l.head = 2
		}
		l.tree -= l.head
	}
	if spare := l.tree - max(len(rows), minTreeInView); l.detail > 0 && spare > 0 {
		l.tree -= spare
		l.detail += spare
	}
	return l
}

// ---- Drawing

// fitLeft is s cut or padded to width.
func fitLeft(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return s + strings.Repeat(" ", max(width-lipgloss.Width(s), 0))
}

// fitRight is s cut to width, padded on its left.
func fitRight(s string, width int) string {
	s = ansi.Truncate(s, width, "…")
	return strings.Repeat(" ", max(width-lipgloss.Width(s), 0)) + s
}

// treeHead is the lines above the tree, after the gutter: the context
// chart where there is room, the pops and slides on the axis, and the
// axis.
func (m *UI) treeHead(l treeLayout, axis timeAxis, roots []*frameNode, slides []frameSlide) []string {
	if l.head == 0 {
		return nil
	}
	t := m.com.Styles
	muted := t.Files.EmptyMessage
	gutter := strings.Repeat(" ", treeGutter)
	var lines []string
	if l.chart > 0 {
		series := m.contextOverTime()
		var top, now int64
		for _, p := range series {
			top = max(top, p.tokens)
		}
		if len(series) > 0 {
			now = series[len(series)-1].tokens
		}
		for i, row := range contextChart(series, axis.from, axis.to, axis.width, l.chart, top) {
			label, right := "", ""
			switch i {
			case 0:
				label = muted.Render("model context") + fitRight(muted.Render(shortTokens(top)), l.cols.label-len("model context")-1)
			case l.chart - 1:
				right = muted.Render(shortTokens(now) + " now")
			}
			lines = append(lines, gutter+fitLeft(label, l.cols.label)+t.Files.Path.Render(row)+" "+fitLeft(right, l.cols.right))
		}
	}
	marks, labels := m.axisRows(axis, roots, slides)
	lines = append(lines, gutter+fitLeft(muted.Render("▴ pop  ⟲ slide"), l.cols.label)+marks)
	return append(lines, gutter+fitLeft(muted.Render("frame"), l.cols.label)+labels+" "+fitRight(muted.Render("freed"), l.cols.right))
}

// axisRows is, axis wide, the pops marked ▴ and the slides ⟲ at their
// times, and the axis's labels.
func (m *UI) axisRows(axis timeAxis, roots []*frameNode, slides []frameSlide) (marks, labels string) {
	t := m.com.Styles
	cells := make([]string, axis.width)
	for i := range cells {
		cells[i] = " "
	}
	eachFrame(roots, func(n *frameNode) {
		if n.popped {
			cells[axis.x(n.until)] = t.Files.Additions.Render("▴")
		}
	})
	for _, s := range slides {
		cells[axis.x(s.at)] = t.Files.TruncationHint.Render("⟲")
	}
	return strings.Join(cells, ""), t.Files.EmptyMessage.Render(axis.labels())
}

// contextOverTime is contextSeries scaled so that its last point is what
// the provider counted for the session's last request, where it did.
func (m *UI) contextOverTime() []contextPoint {
	series := m.sessionFrames.contextSeries()
	if len(series) == 0 {
		return series
	}
	counted, last := m.countedTokens(), series[len(series)-1].tokens
	if counted <= 0 || last <= 0 {
		return series
	}
	for i := range series {
		series[i].tokens = series[i].tokens * counted / last
	}
	return series
}

// treeLine is row's line after the gutter, width wide: its label, then,
// with bars, its span on axis, and what it freed or its clock.
func (r *stackRenderer) treeLine(row explorerRow, cols treeColumns, axis timeAxis, width int) string {
	t := r.t
	if s := row.slide; s != nil {
		label := t.Files.TruncationHint.Render(row.lead + "⟲ " + row.name())
		freed := t.Files.Additions.Render("−" + shortTokens(s.tokens) + " tok")
		if cols.bars == 0 {
			return fitLeft(label, width-lipgloss.Width(freed)-1) + " " + freed
		}
		x := axis.x(s.at)
		bar := strings.Repeat(" ", x) + t.Files.TruncationHint.Render("┃") + strings.Repeat(" ", axis.width-1-x)
		return fitLeft(label, cols.label) + bar + " " + fitRight(freed, cols.right)
	}
	n := row.node
	if cols.bars == 0 {
		return r.entry(n, row.name(), row.lead, width)
	}
	return fitLeft(r.entryHead(n, row.name(), row.lead, cols.label-1), cols.label) + r.bar(n, axis) + " " + fitRight(r.entryRight(n), cols.right)
}

// bar is n's span on axis: from its push to its pop, green, once it is
// popped; to the axis's end, ending in ▶, while it is on the stack.
func (r *stackRenderer) bar(n *frameNode, axis timeAxis) string {
	start := axis.x(n.since)
	if !n.popped {
		style := lipgloss.NewStyle().Foreground(r.t.Header.LogoGradToColor)
		return strings.Repeat(" ", start) + style.Render(strings.Repeat("█", axis.width-1-start)+"▶")
	}
	end := axis.x(n.until)
	return strings.Repeat(" ", start) + r.t.Files.Additions.Render(strings.Repeat("█", end-start+1)) + strings.Repeat(" ", axis.width-1-end)
}

// ---- Context chart

// chartLevels is a cell filled by eighths from the bottom.
var chartLevels = []rune(" ▁▂▃▄▅▆▇█")

// contextChart draws series, the tokens the model was sent over time,
// height rows by width columns from Unix second from to to, top at its
// top: each column the last point at or before its time.
func contextChart(series []contextPoint, from, to int64, width, height int, top int64) []string {
	a := timeAxis{from: from, to: to, width: width}
	heights := make([]int, width)
	for x := range heights {
		t, tokens := a.at(x), int64(0)
		for _, p := range series {
			if p.at > t {
				break
			}
			tokens = p.tokens
		}
		if top > 0 {
			heights[x] = int(math.Round(float64(tokens) * float64(height*8) / float64(top)))
		}
	}
	rows := make([]string, height)
	for r := range rows {
		line := make([]rune, width)
		for x, h := range heights {
			line[x] = chartLevels[min(max(h-(height-1-r)*8, 0), 8)]
		}
		rows[r] = string(line)
	}
	return rows
}
