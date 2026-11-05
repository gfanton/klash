// Package mem is gnosh's shared mem on gno.land: plans whose todos many
// gnosh sessions, on any number of machines, work at once (SWARM.md).
//
// A plan is a tree of todos. Next gives the caller the next free todo
// inside the frame it stands in, held by a lease, in the same order gnosh's
// local mem picks ("first free leaf, enter-mode in a frame"). PopNext pops
// the caller's todo and gives it the next one in the same call. Every call
// is one transaction and the chain runs them one at a time, so two callers
// never get the same todo. When nothing is free but some todos are still
// held, the caller waits in a queue on chain, and the realm hands the next
// todo that frees up to the oldest waiter of that frame, in the call that
// frees it. A hold whose lease runs out comes back on the next call. A
// lease counts blocks, not seconds, so a chain that stops for a while
// makes no hold run out (see now); every lease end, in the state, the
// views and the events' "until", is a block height.
//
// Nothing here is private: titles, holders and results are public. The
// realm keeps no log of changes; every change goes out as an event, which
// each machine's watcher reads from the block.
//
// # Storage, for gas
//
// On gno.land a call pays for every stored object it touches: 59,000 gas
// to read one (plus 17 a byte), 24,000 to write one (plus 14 a byte), and a
// call starts cold, so it reads each object it uses once. What is behind a
// pointer is two objects (a heap item and the struct); a nested struct, a
// slice, an array and a map are one each; strings and numbers sit inside
// their object. An avl.Tree costs two reads a level and copies its path on
// every change. So the hot path (Next, PopNext, Pop) uses no tree:
//
//   - a plan is found in a small map (plans, by a hash of its id);
//   - a todo's children are pointers (Kids) and it points up (Up), so a
//     walk down from the root, or up from a todo, is two reads a step;
//   - which children may be offered now is a bitset string (Avail);
//   - the holds run out in lease order, so the expiring index is a string
//     of held children on each parent (Held) and one line per such parent
//     on the plan (Due): nothing to read unless a lease has run out;
//   - each holder has an Agent (found in a small map by a hash of the
//     holder) listing what it holds, so Pop finds its todo there;
//   - a frame's waiters are pages of lines, read and written at their ends;
//     an agent's line goes when it stops waiting, and an empty queue goes.
//
// The plan's Todos maps, by id, serve the calls that name a todo (Take,
// Drop, Reopen, Renew, Release, AddTodos' parent); the paged view walks
// the tree. No avl.Tree at all: a call reads the type of every field of
// what it loads, and avl's types alone cost 700,000 gas a call.
//
// A plan, a queue and a page point at themselves (Self): an object
// referenced twice is stored on its own, so a change to it rewrites it
// (and its 20-byte hash in the state tree, 248,000 gas) alone, not the map
// it sits in and everything above. Todos are each referenced from the
// Todos maps and from their parent, so they are too.
package mem

import (
	"chain"
	"chain/runtime"
	"strconv"
	"strings"
)

// Bounds: everything a call walks is capped. What it reads grows only
// slowly with a plan: a bucket map holds a 32nd of the plan's todos or
// agents, and Due one line per todo whose children are held.
const (
	maxTitle     = 200  // bytes in a todo's title
	maxName      = 60   // bytes in a todo's name
	maxResult    = 4000 // bytes of a result, carried in its event (Emit caps a value at 4,096)
	maxAdd       = 64   // todos one AddTodos makes
	maxChildren  = 256  // todos under one todo
	maxDepth     = 8    // levels below a plan's root
	maxHolds     = 8    // todos one holder holds in a plan at once
	maxReclaim   = 4    // expired holds one call takes back
	maxServe     = 2    // waiters one call serves
	maxSkip      = 8    // stale or lapsed waiter entries one call passes over
	maxTouched   = 8    // todos made offered whose waiters a call may serve
	maxPage      = 200  // todos in one PlanPage
	maxLabel     = 64   // bytes in an agent label
	maxDropUnder = 32   // open todos one Drop closes with it
	maxParallel  = 16   // open todos under a todo when it is made parallel
	purgePage    = 200  // todos one Purge frees
	minLease     = 10   // blocks
	maxLease     = 3600 // blocks: three hours at gno.land's block a 3 s

	planBuckets  = 16 // maps the plans are spread over
	todoBuckets  = 32 // maps a plan's todos are spread over, by id
	agentBuckets = 32 // maps a plan's agents are spread over
	pageLines    = 32 // waiter entries in one page of a queue
	heldRec      = 16 // bytes of one Held record: 4-digit index, 12-digit lease end
)

// Plan is one shared plan. Its root todo is "t".
type Plan struct {
	Self    *Plan // stores the plan on its own: see Storage
	ID      string
	Owner   address
	Open    bool  // anyone may work it; else its members only
	Lease   int64 // blocks a hold lasts unless renewed
	Seq     int64 // events emitted so far
	Closed  bool
	Count   int // todos
	OpenN   int // todos still open
	HeldN   int // holds
	WaitN   int // holders waiting
	Root    *Todo
	Todos   [todoBuckets]map[string]*Todo // id → todo
	Members map[string]bool               // address string → true
	// Due is the expiring index's top: one line "<until> <id>\n" for each
	// todo with held children, with the earliest lease end among them (a
	// height in 12 digits, so lines compare as strings), sorted, earliest first;
	// "<until> \n" (no id) is the root's own hold.
	Due string
	// Pending is the todos calls freed but could not serve to waiters (at
	// most maxServe a call), one id a line: the next call tries them.
	Pending string
	Agents [agentBuckets]map[string]*Agent // holder → its agent
	Queues map[string]*Queue               // frame id ("" for the whole plan) → its waiters
}

// Todo is one todo of a plan, and its hold if someone holds it.
type Todo struct {
	ID       string // its parent's id, "/", and its name; the root is "t"
	Title    string
	Parent   string
	Up       *Todo   // its parent; nil for the root
	Index    int     // its place among its parent's Kids, from 0
	Kids     []*Todo // its todos, in the order they were added
	Cursor   int     // a sequential todo's first open child, by Index
	Parallel bool    // its todos may be done at once, by anyone
	Status   string  // "", "done" or "dropped"
	Result   string  // sha256 of the result, hex; the text is in the event
	OpenKids int     // children still open
	Avail    string  // bitset over Kids, by Index: the children that may be offered now ("" when none)
	Offered  bool    // its bit is set in its parent's Avail
	Holder   string  // address|session key|label; "" when nobody holds it
	Token    int64   // the seq of the event that gave the hold; Pop names it
	Until    int64   // the hold's lease end: the block height it runs out at
	Held     string  // its held children, one heldRec record each, in the order held
}

// Agent is one holder's standing in a plan: what it holds, and where it
// waits.
type Agent struct {
	Holder string
	Holds  []*Todo // in the order taken
	Frame  string  // the frame it waits in ("" for the whole plan), when Wait is set
	Wait   int64   // the seq of its "waiting" event: its place in Frame's queue; 0 when it does not wait
	Until  int64   // its place's lease end, a block height
	Page   int     // the page of Frame's queue its entry is in
}

// Queue is a frame's waiters, oldest first: entries "<seq> <holder>\n" in
// pages of up to pageLines, read from page Head at byte Pos and written to
// page Tail. An agent's entry goes when it stops waiting (served, its place
// ran out, it took a todo itself, left, or waits elsewhere now), and a
// queue with nobody in it goes.
type Queue struct {
	Self  *Queue
	Pages map[int]*Page
	Head  int
	Pos   int
	Tail  int
	N     int // entries
}

// Page is a run of a queue's entries.
type Page struct {
	Self  *Page
	Lines string
	N     int // entries ever written to it: a page takes pageLines
}

var plans [planBuckets]map[string]*Plan // id → plan, spread by a hash of the id

// holderOf is who calls: the user's address, the session key it signs with
// (none for the account's own key), and the agent's label. The label is the
// caller's word only: the address and session key authenticate; the label
// tells one account's agents apart.
func holderOf(cur realm, label string) string {
	prev := cur.Previous()
	if !prev.IsUser() {
		panic("only a user may call this, not another realm")
	}
	if len(label) == 0 || len(label) > maxLabel || strings.ContainsAny(label, "|\n") {
		panic("give an agent label of 1 to " + strconv.Itoa(maxLabel) + " bytes, without | or a newline")
	}
	session := "-"
	if key, _, _, isSession := runtime.GetSessionInfo(); isSession {
		session = key.String()
	}
	return prev.Address().String() + "|" + session + "|" + label
}

// hash spreads keys over n maps (FNV-1a).
func hash(s string, n int) int {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return int(h % uint32(n))
}

func getPlan(id string) *Plan {
	if m := plans[hash(id, planBuckets)]; m != nil {
		if p, ok := m[id]; ok {
			return p
		}
	}
	panic("no plan " + strconv.Quote(id))
}

func hasPlan(id string) bool {
	m := plans[hash(id, planBuckets)]
	if m == nil {
		return false
	}
	_, ok := m[id]
	return ok
}

func putPlan(p *Plan) {
	b := hash(p.ID, planBuckets)
	if plans[b] == nil {
		plans[b] = map[string]*Plan{}
	}
	plans[b][p.ID] = p
}

func (p *Plan) todo(id string) *Todo {
	if t := p.lookup(id); t != nil {
		return t
	}
	panic("no todo " + strconv.Quote(id) + " in plan " + p.ID)
}

// lookup is todo id, or nil.
func (p *Plan) lookup(id string) *Todo {
	if id == "t" {
		return p.Root
	}
	if m := p.Todos[hash(id, todoBuckets)]; m != nil {
		return m[id]
	}
	return nil
}

func (p *Plan) putTodo(t *Todo) {
	b := hash(t.ID, todoBuckets)
	if p.Todos[b] == nil {
		p.Todos[b] = map[string]*Todo{}
	}
	p.Todos[b][t.ID] = t
}

// after is the todo after t in the tree's order (a todo, then its todos,
// in the order added), or nil.
func (t *Todo) after() *Todo {
	if len(t.Kids) > 0 {
		return t.Kids[0]
	}
	for x := t; x.Up != nil; x = x.Up {
		if x.Index+1 < len(x.Up.Kids) {
			return x.Up.Kids[x.Index+1]
		}
	}
	return nil
}

// mayWork refuses a caller who is neither the owner nor a member of a plan
// that is not open.
func (p *Plan) mayWork(holder string) {
	if p.Closed {
		panic("plan " + p.ID + " is closed")
	}
	if p.Open {
		return
	}
	a := strings.SplitN(holder, "|", 2)[0]
	if a == p.Owner.String() {
		return
	}
	if !p.Members[a] {
		panic(a + " is not a member of plan " + p.ID)
	}
}

func (p *Plan) mustOwn(cur realm) {
	prev := cur.Previous()
	if !prev.IsUser() || prev.Address() != p.Owner {
		panic("only the plan's owner may do this")
	}
}

// emit sends one change out as an event and returns its seq.
func (p *Plan) emit(kind, id string, attrs ...string) int64 {
	p.Seq++
	all := []string{"plan", p.ID, "seq", strconv.FormatInt(p.Seq, 10), "kind", kind, "todo", id}
	chain.Emit("mem", append(all, attrs...)...)
	return p.Seq
}

func cleanText(what, s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		panic(what + " is empty")
	}
	if len(s) > max {
		panic(what + " is longer than " + strconv.Itoa(max) + " bytes")
	}
	for _, c := range s {
		if c < 0x20 && c != '\t' {
			panic(what + " holds a control character")
		}
	}
	return s
}

// resetPlans forgets every plan (tests: realm state outlives a test).
func resetPlans() { plans = [planBuckets]map[string]*Plan{} }

// now is the chain's height. Leases count blocks, not seconds: a chain
// that stops for a while (an upgrade, a halt) makes no blocks, so no hold
// runs out while it stands still, and the results finished meanwhile still
// count when it goes on. Block time would jump over the stop and take back
// every hold at once.
func now() int64 { return runtime.ChainHeight() }
