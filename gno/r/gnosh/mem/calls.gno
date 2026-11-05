package mem

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// Every call that changes a plan is one transaction. gnosh sends one
// input's shared calls as one transaction holding several calls, which the
// chain runs in order, all or nothing.

// NewPlan makes plan id with its root todo "t" titled title, worked by its
// owner and members, or by anyone if open; a hold lasts lease blocks unless
// renewed (gno.land makes a block every ~3 s, so 20 is about a minute).
func NewPlan(cur realm, id, title string, open bool, lease int) string {
	prev := cur.Previous()
	if !prev.IsUser() {
		panic("only a user may make a plan")
	}
	if !validName(id, 40) {
		panic("a plan id is 1 to 40 of a-z, 0-9 and -")
	}
	if hasPlan(id) {
		panic("plan " + id + " exists")
	}
	if lease < minLease || lease > maxLease {
		panic("a lease is " + strconv.Itoa(minLease) + " to " + strconv.Itoa(maxLease) + " blocks")
	}
	p := &Plan{ID: id, Owner: prev.Address(), Open: open, Lease: int64(lease)}
	p.Self = p
	root := &Todo{ID: "t", Title: cleanText("the title", title, maxTitle)}
	p.Root = root
	p.putTodo(root)
	p.Count, p.OpenN = 1, 1
	putPlan(p)
	c := &call{p: p, now: now()}
	p.emit("plan", root.ID, "owner", p.Owner.String(), "open", strconv.FormatBool(open), "lease", strconv.Itoa(lease), "title", root.Title)
	c.update(root)
	return id
}

// AddMember lets addr work plan; only its owner may.
func AddMember(cur realm, plan, addr string) {
	p := getPlan(plan)
	p.mustOwn(cur)
	a := address(addr)
	if !a.IsValid() {
		panic("not an address: " + addr)
	}
	if p.Members == nil {
		p.Members = map[string]bool{}
	}
	p.Members[a.String()] = true
	p.emit("member", "", "address", a.String())
}

// AddTodos adds todos under parent, one for each line of names and of
// titles, in order: names are the ones gnosh made (a-z, 0-9, -), so later
// calls of the same transaction can name them; a name in use is refused.
// It returns their ids, one per line.
func AddTodos(cur realm, plan, label, parent, names, titles string) string {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	pt := p.todo(parent)
	if pt.Status != "" {
		panic(parent + " is " + pt.Status + "; reopen it first")
	}
	if depthOf(parent) >= maxDepth {
		panic("todos go at most " + strconv.Itoa(maxDepth) + " levels deep")
	}
	ns := strings.Split(strings.TrimSpace(names), "\n")
	ts := strings.Split(strings.TrimSpace(titles), "\n")
	if len(ns) != len(ts) {
		panic("give as many names as titles, one per line")
	}
	if len(ns) > maxAdd {
		panic("add at most " + strconv.Itoa(maxAdd) + " todos in one call")
	}
	if len(pt.Kids)+len(ns) > maxChildren {
		panic("a todo holds at most " + strconv.Itoa(maxChildren) + " todos")
	}
	var added []*Todo
	ids := make([]string, 0, len(ns))
	for i, name := range ns {
		if !validName(name, maxName) {
			panic("a todo name is 1 to " + strconv.Itoa(maxName) + " of a-z, 0-9 and -: " + strconv.Quote(name))
		}
		id := pt.ID + "/" + name
		if p.lookup(id) != nil {
			panic("a todo named " + id + " exists")
		}
		t := &Todo{ID: id, Parent: pt.ID, Up: pt, Index: len(pt.Kids), Title: cleanText("a title", ts[i], maxTitle)}
		p.putTodo(t)
		pt.Kids = append(pt.Kids, t)
		pt.OpenKids++
		p.Count++
		p.OpenN++
		p.emit("added", id, "parent", pt.ID, "title", t.Title)
		added = append(added, t)
		ids = append(ids, id)
	}
	if !pt.Parallel {
		c.advance(pt)
	}
	for _, t := range added {
		c.update(t)
	}
	c.update(pt)
	c.end()
	return strings.Join(ids, "\n")
}

// Parallel lets id's todos be done at once, by anyone. Mark a todo parallel
// before adding many todos under it (at most maxParallel open ones).
func Parallel(cur realm, plan, label, id string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	t := p.todo(id)
	if t.Parallel {
		c.end()
		return
	}
	if t.OpenKids > maxParallel {
		panic(id + " has " + strconv.Itoa(t.OpenKids) + " open todos; mark a todo parallel before adding more than " + strconv.Itoa(maxParallel))
	}
	t.Parallel = true
	p.emit("parallel", id)
	for _, k := range t.Kids {
		c.update(k)
	}
	c.update(t)
	c.end()
}

// Next gives the caller the next free todo in frame (a todo id, or "" for
// the whole plan), held for a lease: "held\t<id>\t<token>\t<title>". When
// nothing is free but something in frame is still held, the caller waits
// in frame's queue: "waiting\t<frame>"; a todo it gets later comes as an
// "assigned" event. When the frame the caller holds has nothing open under
// it, it is the caller's to pop: "pop\t<frame>". With nothing open in
// frame: "nothing\t<frame>". A caller waits in one frame at a time.
func Next(cur realm, plan, label, frame string) string {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	c.me = c.agent(holder, true)
	out := c.next(c.me, frame)
	c.end()
	return out
}

// PopNext pops id as Pop does, then gives the caller its next todo in
// frame as Next does, in one call: a worker's whole turn between two
// todos. Waiters are served what the pop freed before the caller looks
// for more, so the oldest waiter gets a todo first. It answers as Next.
func PopNext(cur realm, plan, label, id string, token int, result, frame string) string {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	c.me = c.agent(holder, false)
	c.pop(c.me, id, token, result)
	c.serve()
	out := c.next(c.me, frame)
	c.end()
	return out
}

// Take holds the named todo, open and held by nobody (gnosh's
// mem.Frame(id).Push() on a shared plan): "held\t<id>\t<token>\t<title>".
func Take(cur realm, plan, label, id string) string {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	t := p.todo(id)
	if t.Status != "" {
		panic(id + " is " + t.Status)
	}
	if t.Holder != "" {
		panic(id + " is held by " + t.Holder)
	}
	c.me = c.agent(holder, true)
	c.take(t, c.me, "held")
	c.end()
	return "held\t" + t.ID + "\t" + strconv.FormatInt(t.Token, 10) + "\t" + t.Title
}

// Pop leaves id done with result: only its holder, naming the token of its
// hold, so a hold that lapsed and was taken again cannot be popped. The
// result goes out in the event; its sha256 stays.
func Pop(cur realm, plan, label, id string, token int, result string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	c.me = c.agent(holder, false)
	c.pop(c.me, id, token, result)
	c.end()
}

// Drop closes id as not to be done, with why, and its open todos with it
// (at most maxDropUnder): the plan's owner, or the holder of id.
func Drop(cur realm, plan, label, id, why string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	t := p.todo(id)
	if t.Status != "" {
		panic(id + " is " + t.Status + " already")
	}
	if strings.SplitN(holder, "|", 2)[0] != p.Owner.String() && t.Holder != holder {
		panic("only the plan's owner or the holder of " + id + " may drop it")
	}
	w := cleanText("why", why, maxResult)
	// its open todos, each before those under it; closed ones are not
	// entered
	var under []*Todo
	stack := []*Todo{t}
	for len(stack) > 0 {
		x := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if x.OpenKids == 0 {
			continue
		}
		for k := len(x.Kids) - 1; k >= 0; k-- {
			if kid := x.Kids[k]; kid.Status == "" {
				under = append(under, kid)
				if len(under) > maxDropUnder {
					panic(id + " has more than " + strconv.Itoa(maxDropUnder) + " open todos: drop some of them first")
				}
				stack = append(stack, kid)
			}
		}
	}
	for i := len(under) - 1; i >= 0; i-- { // the deepest first
		u := under[i]
		u.Status = "dropped"
		p.emit("dropped", u.ID, "why", "dropped with "+id)
		c.closed(u)
	}
	t.Status = "dropped"
	p.emit("dropped", id, "holder", holder, "why", w)
	c.closed(t)
	c.end()
}

// Reopen opens id again, done or dropped, with why; a closed parent
// reopens with it.
func Reopen(cur realm, plan, label, id, why string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	t := p.todo(id)
	if t.Status == "" {
		panic(id + " is open")
	}
	w := cleanText("why", why, maxResult)
	t.Status = ""
	t.Result = ""
	p.emit("reopened", id, "holder", holder, "why", w)
	c.reopened(t)
	c.end()
}

// Renew extends the caller's holds on ids (space-separated) for another
// lease, and, when waiting, its place in frame's queue: gnosh's renewer,
// for each session whose turn goes on, names the todos on its stack. Each
// renewed hold goes out as a "renewed" event with its new lease end (a
// place as "waitrenewed"), so a watcher knows when it runs out.
func Renew(cur realm, plan, label, ids, frame string, waiting bool) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	for _, id := range fields(ids) {
		t := p.todo(id)
		if t.Holder != holder {
			panic("you do not hold " + id)
		}
		c.extend(t)
		p.emit("renewed", id, "holder", holder, "until", strconv.FormatInt(t.Until, 10))
	}
	if waiting {
		if frame != "" {
			p.todo(frame)
		}
		c.me = c.agent(holder, true)
		c.wait(c.me, frame)
	}
	c.end()
}

// Release gives up the caller's holds on ids (space-separated) and its
// place in frame's queue: a session that quits, or pressed Esc while it
// waited. Its todos go free, to a waiter first.
func Release(cur realm, plan, label, ids, frame string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	c.me = c.agent(holder, false)
	if c.me != nil && c.me.Wait != 0 && c.me.Frame == frame {
		c.unwait(c.me)
	}
	for _, id := range fields(ids) {
		t := p.todo(id)
		if t.Holder != holder {
			continue
		}
		c.letGo(t)
		p.emit("released", id, "holder", holder)
	}
	c.end()
}

// Reclaim takes back expired holds and serves waiters, with no other
// change: a machine's watcher sends it when a hold its sessions wait on
// has run out and no other call would come.
func Reclaim(cur realm, plan, label string) {
	holder := holderOf(cur, label)
	p := getPlan(plan)
	p.mayWork(holder)
	c := p.begin()
	c.end()
}

// Close ends plan: nothing more may change in it, and Purge may free it.
// Only its owner.
func Close(cur realm, plan string) {
	p := getPlan(plan)
	p.mustOwn(cur)
	p.Closed = true
	p.emit("closed", "")
}

// Purge frees a closed plan, up to purgePage of its todos a call, so the
// storage it took is freed and the deposit that paid for it comes back (on
// gno.land a byte locks 100 ugnot, and a todo takes about 2.3 KB); the call
// that frees the last todos deletes the plan itself. It answers how many
// todos are left: 0 when the plan is gone. Only its owner, once closed.
func Purge(cur realm, plan string) int {
	p := getPlan(plan)
	p.mustOwn(cur)
	if !p.Closed {
		panic("plan " + plan + " is open: Close it first")
	}
	freed := 0
	for b := 0; b < todoBuckets && freed < purgePage; b++ {
		m := p.Todos[b]
		if m == nil {
			continue
		}
		var ids []string
		for id := range m {
			if freed+len(ids) >= purgePage {
				break
			}
			ids = append(ids, id)
		}
		for _, id := range ids {
			t := m[id]
			// A todo and its parent point at each other (Up, Kids), and
			// counted references never free a cycle: cut both ends.
			t.Up, t.Kids = nil, nil
			delete(m, id)
		}
		freed += len(ids)
		if len(m) == 0 {
			p.Todos[b] = nil
		}
	}
	p.Count -= freed
	if p.Count > 0 {
		p.emit("purging", "", "left", strconv.Itoa(p.Count))
		return p.Count
	}
	// The last todos are gone: the rest of the plan goes with them, its
	// self pointers cut first (an object that points at itself is a cycle).
	p.Root = nil
	for b := range p.Agents {
		for _, a := range p.Agents[b] {
			a.Holds = nil
		}
		p.Agents[b] = nil
	}
	var frames []string
	for frame := range p.Queues {
		frames = append(frames, frame)
	}
	for _, frame := range frames {
		q := p.Queues[frame]
		for _, pg := range q.Pages {
			pg.Self = nil
		}
		q.Pages, q.Self = nil, nil
		delete(p.Queues, frame)
	}
	p.Queues, p.Members = nil, nil
	p.emit("purged", "")
	p.Self = nil
	delete(plans[hash(p.ID, planBuckets)], p.ID)
	return 0
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func fields(s string) []string {
	out := strings.Fields(s)
	if len(out) > maxHolds {
		panic("name at most " + strconv.Itoa(maxHolds) + " todos")
	}
	return out
}

func validName(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
