package model

import (
	"cmp"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent"
	"github.com/charmbracelet/crush/internal/message"
)

// eachFrame calls fn with every frame entry in nodes and under them, in
// the order they were entered.
func eachFrame(nodes []*frameNode, fn func(*frameNode)) {
	for _, n := range nodes {
		fn(n)
		eachFrame(n.children, fn)
	}
}

// frameRows is the rows frame n holds: those its pop took out of the
// model's context, or, while it is open, those a pop now would take.
// FrameRows is given the rows from n's push on: a draw looks up every
// frame in view, and from the first row each costs the whole session.
func (f sessionFrames) frameRows(n *frameNode) []message.Message {
	return agent.FrameRows(f.rows[f.pushRound(n):], n.pushCall, n.popCall)
}

// pushRound is where the row making n's push call stands among the rows,
// found from its result's row back; 0 where it is not there.
func (f sessionFrames) pushRound(n *frameNode) int {
	i, ok := f.at[n.pushRow]
	if !ok {
		return 0
	}
	for ; i >= 0; i-- {
		if r := f.rows[i]; r.Role == message.Assistant && slices.ContainsFunc(r.ToolCalls(), func(c message.ToolCall) bool { return c.ID == n.pushCall }) {
			return i
		}
	}
	return 0
}

// heldRows is the rows frame n held in the model's context: once it is
// popped, those its own pop took out, whatever mark a parent's pop gave
// them since, and less what a slide took; while it is open, those still
// sent, which a pop now would take out.
func (f sessionFrames) heldRows(n *frameNode) []message.Message {
	return heldOf(n, f.frameRows(n))
}

// heldOf is heldRows of rows, frame n's.
func heldOf(n *frameNode, rows []message.Message) []message.Message {
	var held []message.Message
	for _, r := range rows {
		if strings.HasPrefix(r.Popped, slideMarkPrefix) || !n.popped && r.Popped != "" {
			continue
		}
		held = append(held, r)
	}
	return held
}

// tokensOf is about how many tokens rows, the session's, hold.
func (f sessionFrames) tokensOf(rows []message.Message) int64 {
	var n int64
	for _, r := range rows {
		n += f.rowTokens(r)
	}
	return n
}

// toolUse is how many calls rows made to the tool name.
type toolUse struct {
	name string
	n    int
}

// frameWork is the tools rows called, the most used first, then in the
// order first used.
func frameWork(rows []message.Message) []toolUse {
	var uses []toolUse
	for _, r := range rows {
		for _, c := range r.ToolCalls() {
			if i := slices.IndexFunc(uses, func(u toolUse) bool { return u.name == c.Name }); i >= 0 {
				uses[i].n++
			} else {
				uses = append(uses, toolUse{name: c.Name, n: 1})
			}
		}
	}
	slices.SortStableFunc(uses, func(a, b toolUse) int { return cmp.Compare(b.n, a.n) })
	return uses
}

// frameEdits is the files rows' calls changed, relative to workingDir
// where under it, as a pop names them to the model.
func frameEdits(rows []message.Message, workingDir string) []string {
	calls := map[string]message.ToolCall{}
	for _, r := range rows {
		for _, c := range r.ToolCalls() {
			calls[c.ID] = c
		}
	}
	return agent.EditedFiles(rows, calls, workingDir)
}

// frameWorkCache is a frame entry's work and edits, and what they were
// worked out from: its key and how many rows, calls and results it held.
type frameWorkCache struct {
	key                  string
	rows, calls, results int
	work                 []toolUse
	edits                []string
}

// frameWorkOf is the tools rows, frame n's, called and the files they
// changed. They are worked out again only once n's rows, calls or results
// change: reading what an edit changed decodes the whole file it held.
func (m *UI) frameWorkOf(n *frameNode, rows []message.Message) ([]toolUse, []string) {
	calls, results := 0, 0
	for _, r := range rows {
		calls += len(r.ToolCalls())
		results += len(r.ToolResults())
	}
	c := &m.explorer.work
	if c.key != n.key || c.rows != len(rows) || c.calls != calls || c.results != results {
		*c = frameWorkCache{key: n.key, rows: len(rows), calls: calls, results: results,
			work: frameWork(rows), edits: frameEdits(rows, m.com.Workspace.WorkingDir())}
	}
	return c.work, c.edits
}

// contextPoint is about how many tokens the model was sent once the row
// of Unix second at came.
type contextPoint struct {
	at     int64
	tokens int64
}

// contextSeries is about how many tokens the model was sent after each of
// the session's rows, rebuilt from the rows alone: a row adds what it
// holds; a pop takes out what its frame held as its row comes; a slide
// takes out its rows as the row after the last of them comes, since its
// mark says what left and not when; a summary leaves only itself.
func (f sessionFrames) contextSeries() []contextPoint {
	roots, _ := f.tree()
	popped := map[string][]*frameNode{}
	eachFrame(roots, func(n *frameNode) {
		if n.popCall != "" {
			popped[n.popCall] = append(popped[n.popCall], n)
		}
	})
	slidAt := map[int][]string{}
	for mark, end := range f.slideEnds() {
		slidAt[end] = append(slidAt[end], mark)
	}

	live := map[string]int64{}
	var total int64
	drop := func(id string) {
		total -= live[id]
		delete(live, id)
	}
	series := make([]contextPoint, 0, len(f.rows))
	for i, r := range f.rows {
		for _, mark := range slidAt[i] {
			for _, s := range f.rows[:i] {
				if s.Popped == mark {
					drop(s.ID)
				}
			}
		}
		if r.IsSummaryMessage {
			clear(live)
			total = 0
		}
		live[r.ID] = f.rowTokens(r)
		total += live[r.ID]
		for _, result := range r.ToolResults() {
			for _, n := range popped[result.ToolCallID] {
				for _, held := range f.frameRows(n) {
					drop(held.ID)
				}
			}
		}
		series = append(series, contextPoint{at: r.CreatedAt, tokens: total})
	}
	return series
}
