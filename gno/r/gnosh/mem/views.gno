package mem

import (
	"strconv"
	"strings"
)

// Views are read with vm/qeval, free, at any height a node keeps. A
// machine's first sync reads PlanJSON and then PlanPage after PlanPage at
// one height, and follows the events from that height on.

// PlanJSON is plan's summary.
func PlanJSON(plan string) string {
	p := getPlan(plan)
	var b strings.Builder
	b.WriteString(`{"id":` + js(p.ID) + `,"owner":` + js(p.Owner.String()))
	b.WriteString(`,"open":` + strconv.FormatBool(p.Open) + `,"closed":` + strconv.FormatBool(p.Closed))
	b.WriteString(`,"lease":` + ji(p.Lease) + `,"seq":` + ji(p.Seq))
	b.WriteString(`,"todos":` + strconv.Itoa(p.Count) + `,"open_todos":` + strconv.Itoa(p.OpenN))
	b.WriteString(`,"held":` + strconv.Itoa(p.HeldN))
	b.WriteString(`,"waiting":` + strconv.Itoa(p.WaitN) + `}`)
	return b.String()
}

// PlanPage is up to limit of plan's todos after the todo after (from the
// root when after is ""), in the tree's order (a todo, then its todos in
// the order added), as a JSON array, each with its hold if it has one: a
// page of a snapshot.
func PlanPage(plan, after string, limit int) string {
	p := getPlan(plan)
	if limit <= 0 || limit > maxPage {
		limit = maxPage
	}
	var b strings.Builder
	b.WriteByte('[')
	t := p.Root
	if after != "" {
		t = p.todo(after).after()
	}
	for n := 0; t != nil && n < limit; n++ {
		if n > 0 {
			b.WriteByte(',')
		}
		b.WriteString(todoJSON(p, t))
		t = t.after()
	}
	b.WriteByte(']')
	return b.String()
}

// WaitingJSON is who waits in plan, by frame (in the order frames were
// first waited in), oldest first: at most maxPage.
func WaitingJSON(plan string) string {
	p := getPlan(plan)
	c := &call{p: p, now: now()}
	var b strings.Builder
	b.WriteByte('[')
	n := 0
	for frame, q := range p.Queues {
		pos := q.Pos
		for pg := q.Head; pg <= q.Tail && n < maxPage; pg++ {
			page := q.Pages[pg]
			if page == nil {
				continue
			}
			lines := page.Lines[pos:]
			pos = 0
			for lines != "" && n < maxPage {
				j := strings.IndexByte(lines, '\n')
				line := lines[:j]
				lines = lines[j+1:]
				sp := strings.IndexByte(line, ' ')
				seq, _ := strconv.ParseInt(line[:sp], 10, 64)
				a := c.agent(line[sp+1:], false)
				if a == nil || a.Wait != seq || a.Frame != frame {
					continue
				}
				if n > 0 {
					b.WriteByte(',')
				}
				b.WriteString(`{"frame":` + js(frame) + `,"holder":` + js(a.Holder) + `,"until":` + ji(a.Until) + `}`)
				n++
			}
		}
	}
	b.WriteByte(']')
	return b.String()
}

func todoJSON(p *Plan, t *Todo) string {
	var b strings.Builder
	b.WriteString(`{"id":` + js(t.ID) + `,"title":` + js(t.Title) + `,"parent":` + js(t.Parent))
	b.WriteString(`,"parallel":` + strconv.FormatBool(t.Parallel) + `,"status":` + js(t.Status))
	b.WriteString(`,"result":` + js(t.Result) + `,"offered":` + strconv.FormatBool(t.Offered))
	b.WriteString(`,"index":` + strconv.Itoa(t.Index) + `,"cursor":` + strconv.Itoa(t.Cursor) + `,"open_kids":` + strconv.Itoa(t.OpenKids))
	if t.Holder != "" {
		b.WriteString(`,"holder":` + js(t.Holder) + `,"token":` + ji(t.Token) + `,"until":` + ji(t.Until))
	}
	b.WriteByte('}')
	return b.String()
}

func js(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch {
		case c == '"':
			b.WriteString(`\"`)
		case c == '\\':
			b.WriteString(`\\`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		case c < 0x20 || c == 0x7f || c == 0x2028 || c == 0x2029:
			h := strconv.FormatInt(int64(c), 16)
			for len(h) < 4 {
				h = "0" + h
			}
			b.WriteString(`\u` + h)
		default:
			b.WriteRune(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func ji(n int64) string { return strconv.FormatInt(n, 10) }
