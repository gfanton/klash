package model

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/gnovm"
	gnovmtools "github.com/charmbracelet/crush/internal/gnovm/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/session"
)

// ---- Reading

// memPulseMsg carries what mem said goes on.
type memPulseMsg struct{ pulse gnovm.MemPulse }

// loadMemPulse reads what mem says goes on, off the update loop.
func (m *UI) loadMemPulse() tea.Cmd {
	return func() tea.Msg {
		pulse, err := m.com.Workspace.MemPulse(context.Background())
		if err != nil {
			slog.Debug("Could not read what mem says goes on", "error", err)
			return nil
		}
		return memPulseMsg{pulse}
	}
}

// callsMem reports whether row answers a gnovm_run call, after which mem
// may say something else.
func callsMem(row message.Message) bool {
	return row.Role == message.Tool && slices.ContainsFunc(row.ToolResults(), func(r message.ToolResult) bool {
		return r.Name == gnovmtools.RunToolName
	})
}

// waitsOnUser is what in mem waits on the user: its questions, and the
// steps that are the user's.
func (m *UI) waitsOnUser() []gnovm.MemPulseItem {
	waits := slices.Clone(m.memPulse.Asks)
	for _, g := range m.memPulse.Goals {
		for _, h := range g.Held {
			if h.Why == "user" && !slices.ContainsFunc(waits, func(w gnovm.MemPulseItem) bool { return w.Name == h.Name }) {
				waits = append(waits, gnovm.MemPulseItem{Name: h.Name, Title: h.Title})
			}
		}
	}
	return waits
}

// ---- Lanes

// The Pulse tab's lanes.
const (
	laneNow = iota
	laneDone
	laneNext
	laneCount
)

var laneNames = [laneCount]string{"NOW", "JUST FINISHED", "UP NEXT"}

// pulseItem is an entry of a lane: its line, with what ends it, and the
// line under it; frame is the session's frame it is, or nil.
type pulseItem struct {
	head, right, sub string
	frame            *frameNode
}

// shortSession is a session's id as the user reads it: its first 8
// bytes.
func shortSession(id string) string {
	return id[:min(len(id), 8)]
}

// stackLook is what the frames are drawn for now, still where the TUI's
// reduce_motion option says so.
func (m *UI) stackLook() stackLook {
	opts := m.com.Config().Options
	still := opts != nil && opts.TUI != nil && opts.TUI.ReduceMotion
	return stackLook{now: time.Now(), busy: m.isAgentBusy(), still: still}
}

// pulseRenderer draws the session's frames for the Pulse tab, as the
// Tree tab does.
func (m *UI) pulseRenderer() *stackRenderer {
	r := &stackRenderer{t: m.com.Styles, look: m.stackLook()}
	r.freedOf = func(n *frameNode) int64 { return tokensOf(m.sessionFrames.heldRows(n)) }
	if _, stack := m.sessionFrames.tree(); len(stack) > 0 {
		r.top = stack[len(stack)-1]
	}
	return r
}

// pulseLanes is what goes on now: the session's open frames, the frame at
// work first, and the frames other sessions work on; what the session
// finished, the last first; and what comes next: the session's todos
// not started and each goal's next step.
func (m *UI) pulseLanes(r *stackRenderer) [laneCount][]pulseItem {
	t := m.com.Styles
	muted, value := t.Files.EmptyMessage, t.Files.ArtifactLabel
	roots, stack := m.sessionFrames.tree()
	self := ""
	if m.session != nil {
		self = m.session.ID
	}
	var lanes [laneCount][]pulseItem

	for i := len(stack) - 1; i >= 0; i-- {
		n := stack[i]
		clock := "since " + time.Unix(n.since, 0).Format("15:04")
		if r.working() {
			clock = runningFor(r.look.now.Sub(time.Unix(n.since, 0)))
		}
		lanes[laneNow] = append(lanes[laneNow], pulseItem{
			head:  t.LSP.WarningDiagnostic.Render("●") + " " + r.name(n, value, n.id),
			sub:   muted.Render("this session · " + clock),
			frame: n,
		})
	}
	peers := map[string]bool{}
	peer := func(name, title, doing string) {
		if doing == "" || doing == self || peers[name] {
			return
		}
		peers[name] = true
		lanes[laneNow] = append(lanes[laneNow], pulseItem{
			head: t.Files.Path.Render("●") + " " + value.Render(name),
			sub:  muted.Render("session " + shortSession(doing) + " · " + title),
		})
	}
	for _, g := range m.memPulse.Goals {
		peer(g.Name, g.Title, g.Doing)
		for _, h := range g.Held {
			if h.Why == "session" {
				peer(h.Name, h.Title, h.Doing)
			}
		}
	}

	var done []*frameNode
	eachFrame(roots, func(n *frameNode) {
		if n.popped {
			done = append(done, n)
		}
	})
	slices.SortFunc(done, func(a, b *frameNode) int { return cmp.Compare(b.popIndex, a.popIndex) })
	for _, n := range done {
		lanes[laneDone] = append(lanes[laneDone], pulseItem{
			head:  t.Files.Additions.Render("✓") + " " + muted.Render(time.Unix(n.until, 0).Format("15:04")) + " " + r.name(n, t.Files.Path, n.id),
			right: r.entryRight(n),
			sub:   muted.Render(n.result),
			frame: n,
		})
	}

	if m.session != nil {
		for _, todo := range m.session.Todos {
			if todo.Status == session.TodoStatusPending {
				lanes[laneNext] = append(lanes[laneNext], pulseItem{head: muted.Render("○") + " " + value.Render(todo.Content), sub: muted.Render("this session's todo")})
			}
		}
	}
	for _, g := range m.memPulse.Goals {
		if g.Next != "" {
			lanes[laneNext] = append(lanes[laneNext], pulseItem{head: muted.Render("○") + " " + value.Render(g.Next), sub: muted.Render(g.NextTitle + " · " + g.Title)})
		}
	}
	return lanes
}

// ---- Drawing

// pulseTab is the Pulse tab's lines, inner wide and at most height tall:
// what waits on the user, where something does; the lanes, side by side
// where the explorer is wide, else one under the other; and a summary.
func (m *UI) pulseTab(inner, height int) []string {
	t := m.com.Styles
	muted := t.Files.EmptyMessage
	var lines []string
	waits := m.waitsOnUser()
	if len(waits) > 0 {
		lines = append(lines, t.LSP.WarningDiagnostic.Render(fmt.Sprintf("▲ WAITS ON YOU (%d)", len(waits))))
		for _, w := range waits {
			lines = append(lines, "   "+t.Files.ArtifactLabel.Render("`"+w.Name+"`")+"   "+muted.Render(w.Title))
		}
		lines = append(lines, "")
	}
	lanes := m.pulseLanes(m.pulseRenderer())
	room := max(height-len(lines)-2, 3)
	if inner >= listBesideWidth {
		lines = append(lines, m.lanesBeside(lanes, inner, room)...)
	} else {
		lines = append(lines, m.lanesUnder(lanes, inner, room)...)
	}

	summary := fmt.Sprintf("%d working · %d waits on you", len(lanes[laneNow]), len(waits))
	if at := m.memPulse.At; at > 0 {
		summary += " · updated " + updatedAgo(time.Now().Sub(time.Unix(at, 0)))
	}
	return append(lines, "", muted.Render(summary))
}

// updatedAgo says how long ago something changed: just now, 30s ago.
func updatedAgo(d time.Duration) string {
	if d < 5*time.Second {
		return "just now"
	}
	return runningFor(d) + " ago"
}

// laneGap is the room between lanes side by side.
const laneGap = 3

// lanesBeside draws the lanes side by side, inner wide, at most rows
// tall.
func (m *UI) lanesBeside(lanes [laneCount][]pulseItem, inner, rows int) []string {
	width := (inner - laneGap*(laneCount-1)) / laneCount
	var cols [laneCount][]string
	tallest := 0
	for i := range lanes {
		cols[i] = m.laneLines(i, lanes[i], width, rows)
		tallest = max(tallest, len(cols[i]))
	}
	lines := make([]string, tallest)
	for y := range lines {
		var b strings.Builder
		for i, col := range cols {
			line := ""
			if y < len(col) {
				line = col[y]
			}
			if i < laneCount-1 {
				line = fitLeft(line, width) + strings.Repeat(" ", laneGap)
			}
			b.WriteString(line)
		}
		lines[y] = b.String()
	}
	return lines
}

// lanesUnder draws the lanes one under the other, inner wide, sharing
// rows.
func (m *UI) lanesUnder(lanes [laneCount][]pulseItem, inner, rows int) []string {
	var lines []string
	for i := range lanes {
		lines = append(lines, m.laneLines(i, lanes[i], inner, max(rows/laneCount, 3))...)
	}
	return lines
}

// laneLines draws lane i, width wide and at most rows tall: its name and
// how many entries it has, then two lines an entry, the one selected
// marked, and how many more there are where they do not fit.
func (m *UI) laneLines(i int, items []pulseItem, width, rows int) []string {
	t := m.com.Styles
	muted := t.Files.EmptyMessage
	head := fmt.Sprintf("── %s (%d) ", laneNames[i], len(items))
	lines := []string{t.Files.TruncationHint.Render(head + strings.Repeat("─", max(width-lipgloss.Width(head), 0)))}
	if len(items) == 0 {
		return append(lines, "  "+muted.Render("nothing"))
	}
	sel := -1
	if m.explorer.lane == i {
		sel = min(max(m.explorer.pulse, 0), len(items)-1)
	}
	fits := max((rows-1)/2, 1)
	first := 0
	if len(items) > fits {
		fits = max((rows-2)/2, 1)
		first = min(max(sel-fits+1, 0), len(items)-fits)
	}
	gutter := lipgloss.NewStyle().Foreground(t.Header.LogoGradToColor)
	for j, item := range items[first:min(first+fits, len(items))] {
		mark := "  "
		if first+j == sel {
			mark = gutter.Render("›") + " "
		}
		line := mark + item.head
		if item.right != "" {
			line = fitLeft(line, width-lipgloss.Width(item.right)-1) + " " + item.right
		}
		lines = append(lines, line, "    "+item.sub)
	}
	if more := len(items) - fits; more > 0 {
		lines = append(lines, "  "+muted.Render(fmt.Sprintf("… %d more", more)))
	}
	return lines
}

// ---- Input

// movePulse moves the selection in its lane by delta.
func (m *UI) movePulse(delta int) {
	items := m.pulseLanes(m.pulseRenderer())[m.explorer.lane]
	m.explorer.pulse = min(max(m.explorer.pulse+delta, 0), max(len(items)-1, 0))
}

// changeLane moves the selection to the lane delta away.
func (m *UI) changeLane(delta int) {
	m.explorer.lane = min(max(m.explorer.lane+delta, 0), laneCount-1)
	m.explorer.pulse = 0
}

// openPulseFrame opens the selected entry in the Tree tab, where it is
// one of the session's frames.
func (m *UI) openPulseFrame() {
	items := m.pulseLanes(m.pulseRenderer())[m.explorer.lane]
	if len(items) == 0 {
		return
	}
	if n := items[min(max(m.explorer.pulse, 0), len(items)-1)].frame; n != nil {
		m.explorer.tab = explorerTree
		m.pauseExplorer(n.key)
	}
}
