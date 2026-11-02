package model

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/gnovm"
	gnovmtools "github.com/charmbracelet/crush/internal/gnovm/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/fade"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// sessionFrames is what the sidebar's Stack section knows of a session's
// frames in mem, built from the rows the UI is sent: each gnovm_run call
// that pushed or popped one of the session's frames, in the order its row
// came, and the rows a pop or a slide took out of the model's context.
type sessionFrames struct {
	calls []frameCall
	cut   map[string]cutRow
}

// frameCall is the frames one gnovm_run call pushed and popped, in order.
type frameCall struct {
	rowID, callID string
	// at is when its row was made, in Unix seconds.
	at int64
	// seen is when the UI was first sent its row as it happened; zero for
	// a row loaded with the session.
	seen   time.Time
	frames []gnovm.MemFrame
}

// cutRow is a row out of the model's context: its mark (message.Popped),
// the id of the frame whose pop took it out or a slide's mark, and about
// how many tokens it held.
type cutRow struct {
	mark   string
	tokens int64
}

// slideMarkPrefix starts the mark of a row a slide took out.
const slideMarkPrefix = "slide:"

func deriveFrames(sessionID string, rows []message.Message) sessionFrames {
	var f sessionFrames
	for _, row := range rows {
		f.observe(sessionID, row, time.Time{})
	}
	return f
}

// observe takes in row, new or updated, sent at now as it happened (zero
// for a row loaded with the session), and reports whether that changed
// what the section shows.
func (f *sessionFrames) observe(sessionID string, row message.Message, now time.Time) bool {
	changed := false
	for _, call := range frameCallsOf(sessionID, row) {
		i := slices.IndexFunc(f.calls, func(c frameCall) bool { return c.callID == call.callID })
		switch {
		case i < 0:
			call.seen = now
			f.calls = append(f.calls, call)
			changed = true
		case !slices.Equal(f.calls[i].frames, call.frames):
			call.seen = f.calls[i].seen
			f.calls[i] = call
			changed = true
		}
	}
	was, wasCut := f.cut[row.ID]
	switch {
	case row.Popped != "":
		if now := (cutRow{mark: row.Popped, tokens: rowTokens(row)}); !wasCut || was != now {
			if f.cut == nil {
				f.cut = map[string]cutRow{}
			}
			f.cut[row.ID] = now
			changed = true
		}
	case wasCut:
		delete(f.cut, row.ID)
		changed = true
	}
	return changed
}

// forget drops a deleted row, and reports whether the section showed
// anything of it.
func (f *sessionFrames) forget(rowID string) bool {
	n := len(f.calls)
	f.calls = slices.DeleteFunc(f.calls, func(c frameCall) bool { return c.rowID == rowID })
	_, wasCut := f.cut[rowID]
	delete(f.cut, rowID)
	return wasCut || len(f.calls) != n
}

// frameNode is one entry into a frame: its push, and its pop once the
// agent left it.
type frameNode struct {
	id, purpose string
	// since is when it was pushed, in Unix seconds.
	since  int64
	popped bool
	result string
	// poppedAt is when the UI saw its pop happen; zero for a pop loaded
	// with the session.
	poppedAt time.Time
	// freed is about how many tokens the rows still marked with the
	// frame's id held. A parent's pop marks its children's rows as its own.
	freed    int64
	children []*frameNode
}

// total is about how many tokens the frame's pop and its children's took
// out of the model's context.
func (n *frameNode) total() int64 {
	t := n.freed
	for _, c := range n.children {
		t += c.total()
	}
	return t
}

// open reports whether the frame, or one entered from it, is still on
// the stack.
func (n *frameNode) open() bool {
	return !n.popped || slices.ContainsFunc(n.children, (*frameNode).open)
}

// tree is the frames the session entered, each under the frame it was
// entered from, in order; and the stack, those not yet left, bottom
// first. A pop takes off the frame it names, wherever it stands, and a
// frame entered again is a new entry. A frame id's marks count for its
// last popped entry.
func (f sessionFrames) tree() (roots, stack []*frameNode) {
	owner := map[string]*frameNode{}
	for _, call := range f.calls {
		for _, ev := range call.frames {
			switch ev.Kind {
			case "push":
				n := &frameNode{id: ev.Id, purpose: ev.Purpose, since: call.at}
				if len(stack) == 0 {
					roots = append(roots, n)
				} else {
					parent := stack[len(stack)-1]
					parent.children = append(parent.children, n)
				}
				stack = append(stack, n)
			case "pop":
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i].id == ev.Id {
						stack[i].popped, stack[i].result, stack[i].poppedAt = true, ev.Result, call.seen
						owner[ev.Id] = stack[i]
						stack = slices.Delete(stack, i, i+1)
						break
					}
				}
			}
		}
	}
	for _, row := range f.cut {
		if n, ok := owner[row.mark]; ok {
			n.freed += row.tokens
		}
	}
	return roots, stack
}

// popFlash is how long a pop the UI saw happen stays lit, its freed
// tokens counting up.
const popFlash = 1500 * time.Millisecond

// flashed is how far through its flash, from 0 to 1, a pop seen at
// poppedAt is at now; 1 for a pop loaded with the session.
func flashed(poppedAt, now time.Time) float64 {
	if poppedAt.IsZero() || now.IsZero() {
		return 1
	}
	return min(max(float64(now.Sub(poppedAt))/float64(popFlash), 0), 1)
}

// animating reports whether the section moves at now: while the agent
// works (busy) in a frame, or while a pop it just saw is lit.
func (f sessionFrames) animating(now time.Time, busy bool) bool {
	roots, stack := f.tree()
	if busy && len(stack) > 0 {
		return true
	}
	var lit func([]*frameNode) bool
	lit = func(nodes []*frameNode) bool {
		for _, n := range nodes {
			if n.popped && flashed(n.poppedAt, now) < 1 || lit(n.children) {
				return true
			}
		}
		return false
	}
	return lit(roots)
}

// freed is about how many tokens the rows pops and slides took out of the
// model's context held.
func (f sessionFrames) freed() int64 {
	var tokens int64
	for _, r := range f.cut {
		tokens += r.tokens
	}
	return tokens
}

// slid is how many rows slides took out of the model's context, and about
// how many tokens they held.
func (f sessionFrames) slid() (rows int, tokens int64) {
	for _, r := range f.cut {
		if strings.HasPrefix(r.mark, slideMarkPrefix) {
			rows++
			tokens += r.tokens
		}
	}
	return rows, tokens
}

// frameCallsOf is the gnovm_run calls in row that pushed or popped a frame
// of the session. A frame another session made, in a row copied by a
// branch, is not the session's.
func frameCallsOf(sessionID string, row message.Message) []frameCall {
	if row.Role != message.Tool {
		return nil
	}
	var calls []frameCall
	for _, result := range row.ToolResults() {
		if result.Name != gnovmtools.RunToolName || result.IsError || result.Metadata == "" {
			continue
		}
		var meta gnovmtools.RunResponseMetadata
		if json.Unmarshal([]byte(result.Metadata), &meta) != nil {
			continue
		}
		own := slices.DeleteFunc(meta.Frames, func(ev gnovm.MemFrame) bool { return ev.Session != sessionID })
		if len(own) > 0 {
			calls = append(calls, frameCall{rowID: row.ID, callID: result.ToolCallID, at: row.CreatedAt, frames: own})
		}
	}
	return calls
}

// rowTokens is about how many tokens row holds for the model: 4 bytes a
// token, rounded up, per text.
func rowTokens(row message.Message) int64 {
	approx := func(s string) int64 { return int64((len(s) + 3) / 4) }
	var n int64
	for _, part := range row.Parts {
		switch p := part.(type) {
		case message.TextContent:
			n += approx(p.Text)
		case message.ReasoningContent:
			n += approx(p.Thinking)
		case message.ToolCall:
			n += approx(p.Name) + approx(p.Input)
		case message.ToolResult:
			n += approx(p.Content)
		}
	}
	return n
}

// maxFinishedTasks is how many finished top-level frames the section
// lists; the older ones are counted on one line.
const maxFinishedTasks = 3

// stackLook is what the section is drawn for: the time, and whether the
// agent is working, which sets the open frames' clocks running and the
// one it is in shimmering.
type stackLook struct {
	now  time.Time
	busy bool
}

// stackSection renders the sidebar's Stack section: the frames the session
// entered as a tree, a finished one with its result and about how many
// tokens its pop took out of the model's context, the frame the agent is
// in with its purpose; a finished top-level frame folds its children, and
// only the last few are listed. Then what slides took out. "" for a
// session with neither.
func stackSection(t *styles.Styles, f sessionFrames, width int, look stackLook) string {
	roots, stack := f.tree()
	slidRows, slidTokens := f.slid()
	if len(roots) == 0 && slidRows == 0 {
		return ""
	}
	r := stackRenderer{t: t, width: width, look: look}
	if len(stack) > 0 {
		r.top = stack[len(stack)-1]
	}
	var freed []string
	if total := f.freed(); total > 0 {
		freed = append(freed, t.Files.Additions.Render(shortTokens(total)+" tok freed"))
	}
	r.lines = []string{common.Section(t, "Stack", width, freed...), ""}
	finished := 0
	for _, n := range roots {
		if !n.open() {
			finished++
		}
	}
	skip := max(finished-maxFinishedTasks, 0)
	if skip > 0 {
		r.add(t.Files.TruncationHint.Render(fmt.Sprintf("…%d earlier", skip)))
	}
	for _, n := range roots {
		if !n.open() && skip > 0 {
			skip--
			continue
		}
		r.node(n, "", "", "")
	}
	if slidRows > 0 {
		unit := "msgs"
		if slidRows == 1 {
			unit = "msg"
		}
		r.lines = append(r.lines, "")
		r.add(t.Files.Path.Render("slid out of the model's context"))
		r.add("  " + t.Files.EmptyMessage.Render(fmt.Sprintf("%d %s · ≈%s tokens", slidRows, unit, shortTokens(slidTokens))))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(r.lines, "\n"))
}

type stackRenderer struct {
	t     *styles.Styles
	width int
	look  stackLook
	// top is the frame the agent is in; nil with an empty stack.
	top   *frameNode
	lines []string
}

// shimmerStep is how long the shimmer's colors stay before moving on by
// one cell.
const shimmerStep = 80 * time.Millisecond

// paint renders s in the theme's fade from its gradient's start to its end
// and back, shifted by phase cells, so that drawn at successive phases the
// colors run along it. Without the theme's colors it is base's alone.
func (r *stackRenderer) paint(base lipgloss.Style, s string, phase int) string {
	from, to := r.t.Header.LogoGradFromColor, r.t.Header.LogoGradToColor
	runes := []rune(s)
	if from == nil || to == nil || len(runes) == 0 {
		return base.Render(s)
	}
	period := max(2*len(runes), 8)
	ramp := lipgloss.Blend1D(period, fade.Stops(from, to, from)...)
	var b strings.Builder
	for i, c := range runes {
		b.WriteString(base.Foreground(ramp[(i+phase)%period]).Render(string(c)))
	}
	return b.String()
}

// runningFor writes d as a running clock: 45s, 1m05s, 1h02m.
func runningFor(d time.Duration) string {
	d = max(d, 0).Truncate(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

// add appends line, cut to the section's width.
func (r *stackRenderer) add(line string) {
	r.lines = append(r.lines, ansi.Truncate(line, r.width, "…"))
}

// node adds n's line under lead: its name, the last part of its id under
// its parent's, with when it was entered while it is open or what its
// pop freed once it is done; then, under cont, the purpose of the frame
// the agent is in or a finished frame's result, and, while n is open, the
// frames entered from it.
func (r *stackRenderer) node(n *frameNode, parent, lead, cont string) {
	t := r.t
	name := n.id
	if rest, ok := strings.CutPrefix(n.id, parent+"/"); ok && parent != "" {
		name = rest
	}
	open := n.open()
	working := r.look.busy && !r.look.now.IsZero()
	lit := flashed(n.poppedAt, r.look.now)
	marker, nameStyle, right := t.Files.Additions.Render("✓"), t.Files.Path, ""
	switch {
	case open && working:
		marker, nameStyle = t.LSP.WarningDiagnostic.Render("▸"), t.Files.ArtifactLabel
		right = t.Files.EmptyMessage.Render(runningFor(r.look.now.Sub(time.Unix(n.since, 0))))
	case open:
		marker, nameStyle = t.LSP.WarningDiagnostic.Render("▸"), t.Files.ArtifactLabel
		right = t.Files.EmptyMessage.Render(time.Unix(n.since, 0).Format("15:04"))
	default:
		// A pop just seen counts its freed tokens up, easing out.
		eased := 1 - (1-lit)*(1-lit)*(1-lit)
		if freed := int64(float64(n.total()) * eased); freed > 0 {
			right = t.Files.Additions.Render("−" + shortTokens(freed) + " tok")
		}
	}
	head := t.Files.TruncationHint.Render(lead) + marker + " "
	room := r.width - lipgloss.Width(head) - lipgloss.Width(right)
	if right != "" {
		room--
	}
	name = ansi.Truncate(name, max(room, 1), "…")
	pad := max(r.width-lipgloss.Width(head)-lipgloss.Width(name)-lipgloss.Width(right), 1)
	if right == "" {
		pad = 0
	}
	shown := nameStyle.Render(name)
	switch {
	case n == r.top && working:
		shown = r.paint(nameStyle, name, int(r.look.now.UnixMilli()/shimmerStep.Milliseconds()))
	case lit < 1:
		shown = r.paint(nameStyle, name, 0)
	}
	r.add(head + shown + strings.Repeat(" ", pad) + right)

	detail := t.Files.TruncationHint.Render(cont + "  ")
	switch {
	case n == r.top:
		r.add(detail + t.Files.EmptyMessage.Render(strconv.Quote(n.purpose)))
	case !open && n.result != "":
		r.add(detail + t.Files.EmptyMessage.Render(n.result))
	}
	if !open {
		return
	}
	for i, c := range n.children {
		branch, next := "├ ", "│ "
		if i == len(n.children)-1 {
			branch, next = "└ ", "  "
		}
		r.node(c, n.id, cont+branch, cont+next)
	}
}
