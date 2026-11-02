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
func (f sessionFrames) frameRows(n *frameNode) []message.Message {
	return agent.FrameRows(f.rows, n.pushCall, n.popCall)
}

// heldRows is the rows frame n held in the model's context: once it is
// popped, those its own pop took out, whatever mark a parent's pop gave
// them since, and less what a slide took; while it is open, those still
// sent, which a pop now would take out.
func (f sessionFrames) heldRows(n *frameNode) []message.Message {
	var held []message.Message
	for _, r := range f.frameRows(n) {
		if strings.HasPrefix(r.Popped, slideMarkPrefix) || !n.popped && r.Popped != "" {
			continue
		}
		held = append(held, r)
	}
	return held
}

// tokensOf is about how many tokens rows hold.
func tokensOf(rows []message.Message) int64 {
	var n int64
	for _, r := range rows {
		n += rowTokens(r)
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
		live[r.ID] = rowTokens(r)
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
