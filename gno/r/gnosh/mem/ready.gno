package mem

import (
	"strconv"
	"strings"
)

// What may be taken. Each todo keeps Avail, its children that may be
// offered now. A child is offered when it is open and either has nothing
// open under it and nobody holds it, or has an offered child of its own and
// is unheld or parallel; under a sequential todo only its first open child
// (its cursor) can be offered. When a child's standing changes, so does its
// parent's Avail, which may change the parent's own standing, and so on up
// to the root: at most maxDepth updates. Next walks down from its frame,
// taking the first offered child at each level, to a todo with nothing open
// under it: no call walks finished or held todos.

// call is one call's working state: its plan, the block height, the
// caller's agent, the todos it made offered (whose frames' waiters it may
// serve at its end), and how many waiters it served and queue entries it
// passed over.
type call struct {
	p       *Plan
	now     int64
	nowKey  string // now, as Due and Held write a height
	me      *Agent // the caller's agent, kept through the call
	touched []*Todo
	served  int
	skipped int
}

// begin starts a call that changes a plan: expired holds come back first.
func (p *Plan) begin() *call {
	c := &call{p: p, now: now()}
	c.nowKey = c.pad(c.now, 12)
	c.reclaim()
	return c
}

// end serves waiters the call's changes freed a todo for, and lets the
// caller's agent go if it holds nothing and waits nowhere.
func (c *call) end() {
	c.serve()
	if me := c.me; me != nil {
		c.me = nil
		c.forget(me)
	}
}

// pad writes n in width digits, zero-padded, so times sort as strings.
func (c *call) pad(n int64, width int) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < width {
		s = "0" + s
	}
	return s
}

// --- what may be offered ---

func (c *call) offerable(t *Todo) bool {
	if t.Status != "" {
		return false
	}
	if up := t.Up; up != nil && !up.Parallel && up.Cursor != t.Index {
		return false
	}
	if t.OpenKids == 0 {
		return t.Holder == ""
	}
	return t.Avail != "" && (t.Holder == "" || t.Parallel)
}

// update brings t's bit in its parent's Avail up to date, and so on up.
func (c *call) update(t *Todo) {
	for i := 0; i <= maxDepth+1 && t != nil; i++ {
		now := c.offerable(t)
		if now == t.Offered {
			return
		}
		t.Offered = now
		if now && len(c.touched) < maxTouched {
			c.touched = append(c.touched, t)
		}
		up := t.Up
		if up == nil {
			return
		}
		up.setAvail(t.Index, now)
		t = up
	}
}

// advance moves a sequential todo's cursor past its closed children.
func (c *call) advance(t *Todo) {
	for t.Cursor < len(t.Kids) && t.Kids[t.Cursor].Status != "" {
		t.Cursor++
	}
}

// closed follows t's closing (done or dropped): its hold ends, its parent
// has one open todo fewer, a sequential parent's next child may be
// offered, and a parent with nothing left open may be taken itself.
func (c *call) closed(t *Todo) {
	c.p.OpenN--
	if t.Holder != "" {
		c.letGo(t)
	}
	c.update(t)
	up := t.Up
	if up == nil {
		return
	}
	up.OpenKids--
	if !up.Parallel {
		c.advance(up)
		if up.Cursor < len(up.Kids) {
			c.update(up.Kids[up.Cursor])
		}
	}
	c.update(up)
}

// reopened follows t's reopening: a closed parent reopens too, and a
// sequential parent's cursor comes back to t when t is earlier.
func (c *call) reopened(t *Todo) {
	c.p.OpenN++
	up := t.Up
	if up == nil {
		c.update(t)
		return
	}
	if up.Status != "" {
		up.Status = ""
		up.Result = ""
		c.p.emit("reopened", up.ID, "why", "a todo under it was reopened")
		c.reopened(up)
	}
	up.OpenKids++
	if !up.Parallel && t.Index < up.Cursor {
		old := up.Cursor
		up.Cursor = t.Index
		if old < len(up.Kids) {
			c.update(up.Kids[old])
		}
	}
	c.update(t)
	c.update(up)
}

// walk finds, inside f (f included), a todo with nothing open under it
// that may be taken now, or nil.
func (c *call) walk(f *Todo) *Todo {
	node := f
	for i := 0; i <= maxDepth+1; i++ {
		if node.Status != "" {
			return nil
		}
		if node.OpenKids == 0 {
			if node.Holder == "" {
				return node
			}
			return nil
		}
		k := node.availFirst()
		if k < 0 {
			return nil
		}
		node = node.Kids[k]
	}
	return nil
}

// availFirst is the lowest index whose bit is set in t.Avail, or -1.
func (t *Todo) availFirst() int {
	s := t.Avail
	for i := 0; i < len(s); i++ {
		if b := s[i]; b != 0 {
			for j := 0; j < 8; j++ {
				if b&(1<<uint(j)) != 0 {
					return i*8 + j
				}
			}
		}
	}
	return -1
}

// setAvail sets or clears bit i of t.Avail. Trailing zero bytes go, so an
// empty set is "".
func (t *Todo) setAvail(i int, on bool) {
	b := []byte(t.Avail)
	for len(b) <= i/8 {
		b = append(b, 0)
	}
	m := byte(1) << uint(i%8)
	if on {
		b[i/8] |= m
	} else {
		b[i/8] &^= m
	}
	n := len(b)
	for n > 0 && b[n-1] == 0 {
		n--
	}
	t.Avail = string(b[:n])
}

// --- holds ---

// take gives t to a for a lease; kind is "held" (it asked) or "assigned"
// (it waited, and this call served it).
func (c *call) take(t *Todo, a *Agent, kind string) {
	p := c.p
	if len(a.Holds) >= maxHolds {
		panic(a.Holder + " holds " + strconv.Itoa(maxHolds) + " todos of plan " + p.ID + " already: pop, drop or release one first")
	}
	t.Holder = a.Holder
	t.Until = c.now + p.Lease
	t.Token = p.emit(kind, t.ID, "holder", a.Holder, "until", strconv.FormatInt(t.Until, 10), "title", t.Title)
	a.Holds = append(a.Holds, t)
	p.HeldN++
	c.held(t, true)
	c.update(t)
}

// letGo ends t's hold.
func (c *call) letGo(t *Todo) {
	c.held(t, false)
	if a := c.agent(t.Holder, false); a != nil {
		a.drop(t)
		c.forget(a)
	}
	c.p.HeldN--
	t.Holder, t.Token, t.Until = "", 0, 0
	c.update(t)
}

// extend renews t's hold for another lease.
func (c *call) extend(t *Todo) {
	c.held(t, false)
	t.Until = c.now + c.p.Lease
	c.held(t, true)
}

// held puts t's lease end in the expiring index (on), or takes it out: a
// record in its parent's Held, and the parent's line in the plan's Due
// with the earliest lease end among its held children. The root's hold is
// a Due line of its own.
func (c *call) held(t *Todo, on bool) {
	up := t.Up
	if up == nil {
		until := ""
		if on {
			until = c.pad(t.Until, 12)
		}
		c.due("", until)
		return
	}
	idx := c.pad(int64(t.Index), 4)
	s := up.Held
	for i := 0; i+heldRec <= len(s); i += heldRec {
		if s[i:i+4] == idx {
			s = s[:i] + s[i+heldRec:]
			break
		}
	}
	if on {
		s += idx + c.pad(t.Until, 12)
	}
	up.Held = s
	min := ""
	for i := 0; i+heldRec <= len(s); i += heldRec {
		if u := s[i+4 : i+heldRec]; min == "" || u < min {
			min = u
		}
	}
	c.due(up.ID, min)
}

// due sets id's line in the plan's Due to until, or drops it when until is
// "". Due stays sorted by lease end, earliest first.
func (c *call) due(id, until string) {
	p := c.p
	d := p.Due
	tail := " " + id + "\n"
	for i := 0; i < len(d); {
		j := strings.IndexByte(d[i:], '\n')
		if d[i+12:i+j+1] == tail {
			if d[i:i+12] == until {
				return
			}
			d = d[:i] + d[i+j+1:]
			break
		}
		i += j + 1
	}
	if until != "" {
		i := 0
		for i < len(d) && d[i:i+12] <= until {
			i += strings.IndexByte(d[i:], '\n') + 1
		}
		d = d[:i] + until + tail + d[i:]
	}
	p.Due = d
}

// reclaim takes back up to maxReclaim holds whose lease ran out, earliest
// first: Due's first lines name the todos whose Held to look in. A call
// with nothing expired reads only Due's first line.
func (c *call) reclaim() {
	p := c.p
	if p.Due == "" || p.Due[:12] > c.nowKey {
		return
	}
	var expired []*Todo
	d := p.Due
	for i := 0; i < len(d) && len(expired) < maxReclaim; {
		j := strings.IndexByte(d[i:], '\n')
		line := d[i : i+j]
		i += j + 1
		if line[:12] > c.nowKey {
			break
		}
		id := line[13:]
		if id == "" {
			expired = append(expired, p.Root)
			continue
		}
		up := p.todo(id)
		s := up.Held
		for k := 0; k+heldRec <= len(s) && len(expired) < maxReclaim; k += heldRec {
			if s[k+4:k+heldRec] <= c.nowKey {
				n, _ := strconv.Atoi(s[k : k+4])
				expired = append(expired, up.Kids[n])
			}
		}
	}
	for _, t := range expired {
		holder := t.Holder
		c.letGo(t)
		p.emit("lapsed", t.ID, "holder", holder)
	}
}

// --- agents ---

// agent is holder's agent in the plan, made when make is set; nil when it
// has none.
func (c *call) agent(holder string, make bool) *Agent {
	p := c.p
	b := hash(holder, agentBuckets)
	m := p.Agents[b]
	if m != nil {
		if a, ok := m[holder]; ok {
			return a
		}
	}
	if !make {
		return nil
	}
	a := &Agent{Holder: holder}
	if m == nil {
		m = map[string]*Agent{}
		p.Agents[b] = m
	}
	m[holder] = a
	return a
}

// forget lets a go when it holds nothing and waits nowhere, so idle agents
// take no room; the caller's own agent goes only at the call's end.
func (c *call) forget(a *Agent) {
	if a == nil || len(a.Holds) > 0 || a.Wait != 0 {
		return
	}
	if c.me != nil && c.me.Holder == a.Holder {
		return
	}
	delete(c.p.Agents[hash(a.Holder, agentBuckets)], a.Holder)
}

// holding is the todo id among a's holds, or nil.
func (a *Agent) holding(id string) *Todo {
	for _, t := range a.Holds {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// drop takes t out of a's holds.
func (a *Agent) drop(t *Todo) {
	var out []*Todo
	for _, h := range a.Holds {
		if h.ID != t.ID {
			out = append(out, h)
		}
	}
	a.Holds = out
}

// --- waiters ---

// queue is frame's queue, made when make is set; nil when nobody waits
// there.
func (c *call) queue(frame string, make bool) *Queue {
	p := c.p
	if p.Queues != nil {
		if q, ok := p.Queues[frame]; ok {
			return q
		}
	}
	if !make {
		return nil
	}
	q := &Queue{Pages: map[int]*Page{}}
	q.Self = q
	if p.Queues == nil {
		p.Queues = map[string]*Queue{}
	}
	p.Queues[frame] = q
	return q
}

// wait puts a in frame's queue, or renews its place there. An agent waits
// in one frame at a time: waiting in another moves it.
func (c *call) wait(a *Agent, frame string) {
	p := c.p
	until := c.now + p.Lease
	if a.Wait != 0 && a.Frame == frame {
		a.Until = until
		p.emit("waitrenewed", frame, "holder", a.Holder, "until", strconv.FormatInt(until, 10))
		return
	}
	c.unwait(a)
	seq := p.emit("waiting", frame, "holder", a.Holder, "until", strconv.FormatInt(until, 10))
	a.Page = c.queue(frame, true).push(strconv.FormatInt(seq, 10) + " " + a.Holder + "\n")
	a.Frame, a.Wait, a.Until = frame, seq, until
	p.WaitN++
}

// unwait takes a out of the queue it waits in, and lets an emptied queue
// go.
func (c *call) unwait(a *Agent) {
	if a.Wait == 0 {
		return
	}
	if q := c.queue(a.Frame, false); q != nil {
		q.remove(a.Page, strconv.FormatInt(a.Wait, 10)+" "+a.Holder+"\n")
		if q.N == 0 {
			c.dropQueue(a.Frame, q)
		}
	}
	a.Frame, a.Wait, a.Until, a.Page = "", 0, 0, 0
	c.p.WaitN--
}

// dropQueue lets an empty queue and its pages go.
func (c *call) dropQueue(frame string, q *Queue) {
	for _, pg := range q.Pages {
		pg.Self = nil
	}
	q.Pages = nil
	q.Self = nil
	delete(c.p.Queues, frame)
}

// head is the oldest agent waiting in q (frame's queue) whose place has
// not run out. Those met before it whose place ran out leave, at most
// maxSkip a call.
func (c *call) head(q *Queue, frame string) *Agent {
	for q.N > 0 && c.skipped < maxSkip {
		seq, holder := q.peek()
		a := c.agent(holder, false)
		if a == nil || a.Wait != seq || a.Frame != frame {
			// no waiter behind it: an agent's entry goes when it stops
			// waiting, so this is only a guard
			q.drop()
			c.skipped++
			continue
		}
		if a.Until < c.now {
			c.skipped++
			c.unwait(a)
			c.p.emit("waitlapsed", frame, "holder", holder)
			c.forget(a)
			continue
		}
		return a
	}
	return nil
}

// push adds an entry at the queue's end and says which page took it.
func (q *Queue) push(line string) int {
	pg := q.Pages[q.Tail]
	if pg != nil && pg.N >= pageLines {
		q.Tail++
		pg = nil
	}
	if pg == nil {
		pg = &Page{}
		pg.Self = pg
		q.Pages[q.Tail] = pg
	}
	pg.Lines += line
	pg.N++
	q.N++
	return q.Tail
}

// peek is the queue's oldest entry: its seq and holder.
func (q *Queue) peek() (int64, string) {
	s := q.Pages[q.Head].Lines[q.Pos:]
	line := s[:strings.IndexByte(s, '\n')]
	sp := strings.IndexByte(line, ' ')
	seq, _ := strconv.ParseInt(line[:sp], 10, 64)
	return seq, line[sp+1:]
}

// drop passes the oldest entry.
func (q *Queue) drop() {
	pg := q.Pages[q.Head]
	q.Pos += strings.IndexByte(pg.Lines[q.Pos:], '\n') + 1
	q.N--
	q.tidy()
}

// remove takes the entry line out of page pn, wherever it is in it.
func (q *Queue) remove(pn int, line string) {
	pg := q.Pages[pn]
	if pg == nil {
		return
	}
	i := 0
	if pn == q.Head {
		i = q.Pos
	}
	for i+len(line) <= len(pg.Lines) {
		j := strings.Index(pg.Lines[i:], line)
		if j < 0 {
			return
		}
		k := i + j
		if k == 0 || pg.Lines[k-1] == '\n' {
			pg.Lines = pg.Lines[:k] + pg.Lines[k+len(line):]
			q.N--
			q.tidy()
			return
		}
		i = k + 1
	}
}

// tidy lets the head go past pages read to their end; the tail page stays
// while it takes entries.
func (q *Queue) tidy() {
	for {
		pg := q.Pages[q.Head]
		if pg != nil && q.Pos < len(pg.Lines) {
			return
		}
		if q.Head == q.Tail && (pg == nil || pg.N < pageLines) {
			return
		}
		if pg != nil {
			pg.Self = nil
			delete(q.Pages, q.Head)
		}
		q.Head++
		q.Pos = 0
		if q.Tail < q.Head {
			q.Tail = q.Head
		}
	}
}

// serve gives the todos this call freed, and those earlier calls freed but
// could not serve (Pending), to the oldest waiters that can take them: for
// each, the oldest head of the queues of the frames holding it (the todo,
// its ancestors, the whole plan) walks its frame and takes what it finds.
// At most maxServe a call; what is left is Pending, for the next call
// (any call that changes the plan, or a watcher's Reclaim).
func (c *call) serve() {
	p := c.p
	if p.WaitN == 0 || p.Queues == nil {
		p.Pending = ""
		c.touched = nil
		return
	}
	cands := c.touched
	if p.Pending != "" {
		for _, id := range strings.Split(p.Pending, "\n") {
			if t := p.lookup(id); t != nil {
				cands = append(cands, t)
			}
		}
		p.Pending = ""
	}
	var left []string
	seen := map[string]bool{}
	for _, t := range cands {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		if c.served >= maxServe {
			if t.Offered && len(left) < maxTouched {
				left = append(left, t.ID)
			}
			continue
		}
		var best *Agent
		var bestF *Todo
		for f := t; ; f = f.Up {
			id := ""
			if f != nil {
				id = f.ID
			}
			if q := c.queue(id, false); q != nil {
				if a := c.head(q, id); a != nil && (best == nil || a.Wait < best.Wait) {
					best, bestF = a, f
				}
			}
			if f == nil {
				break
			}
		}
		if best == nil || len(best.Holds) >= maxHolds {
			continue
		}
		frame := bestF
		if frame == nil {
			frame = p.Root
		}
		w := c.walk(frame)
		if w == nil {
			continue
		}
		c.served++
		c.unwait(best)
		c.take(w, best, "assigned")
	}
	p.Pending = strings.Join(left, "\n")
	c.touched = nil
}

// --- Next and Pop, the steps PopNext joins ---

// next gives a its next todo in frame: see Next.
func (c *call) next(a *Agent, frame string) string {
	p := c.p
	if len(a.Holds) >= maxHolds {
		panic(a.Holder + " holds " + strconv.Itoa(maxHolds) + " todos of plan " + p.ID + " already: pop, drop or release one first")
	}
	f := p.Root
	if frame != "" {
		f = p.todo(frame)
	}
	if t := c.walk(f); t != nil {
		if a.Frame == frame {
			c.unwait(a)
		}
		c.take(t, a, "held")
		return "held\t" + t.ID + "\t" + strconv.FormatInt(t.Token, 10) + "\t" + t.Title
	}
	if f.Status == "" && f.OpenKids == 0 && f.Holder == a.Holder {
		if a.Frame == frame {
			c.unwait(a)
		}
		return "pop\t" + frame
	}
	if f.Status != "" || (frame == "" && p.OpenN == 0) {
		if a.Frame == frame {
			c.unwait(a)
		}
		return "nothing\t" + frame
	}
	c.wait(a, frame)
	return "waiting\t" + frame
}

// pop leaves id, which a holds by token, done with result: see Pop.
func (c *call) pop(a *Agent, id string, token int, result string) {
	var t *Todo
	if a != nil {
		t = a.holding(id)
	}
	if t == nil {
		panic("you do not hold " + id + ": your hold was lost (its lease ran out, or it was dropped or released)")
	}
	if t.Token != int64(token) {
		panic("your hold on " + t.ID + " was lost and taken again (token " + strconv.FormatInt(t.Token, 10) + ", not " + strconv.Itoa(token) + ")")
	}
	if t.OpenKids > 0 {
		panic(id + " has open todos")
	}
	r := cleanText("the result", result, maxResult)
	t.Status = "done"
	t.Result = sha256Hex(r)
	c.p.emit("done", id, "holder", a.Holder, "result", r)
	c.closed(t)
}

func depthOf(id string) int { return strings.Count(id, "/") }
