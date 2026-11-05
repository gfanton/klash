package mem

import (
	"strconv"
	"strings"
	"testing"

	"chain/runtime"

	"gno.land/p/nt/testutils/v0"
	"gno.land/p/nt/uassert/v0"
	"gno.land/p/nt/urequire/v0"
)

var (
	owner = testutils.TestAddress("owner")
	w1    = testutils.TestAddress("w1")
	w2    = testutils.TestAddress("w2")
	w3    = testutils.TestAddress("w3")
	other = testutils.TestAddress("other")
)

func user(a address) runtime.Realm { return testing.NewUserRealm(a) }

func holderFor(a address, label string) string { return a.String() + "|-|" + label }

// fresh makes a plan, open to anyone, with a 12-block lease (a minute at
// the tests' 5 s a block), after
// clearing every plan a test before made (realm state outlives a test).
func fresh(cur realm, t *testing.T, id string) {
	t.Helper()
	resetPlans()
	testing.SetRealm(user(owner))
	NewPlan(cross(cur), id, "the task", true, 12)
}

func add(cur realm, who address, plan, parent, names, titles string) string {
	testing.SetRealm(user(who))
	return AddTodos(cross(cur), plan, "w", parent, names, titles)
}

func next(cur realm, who address, plan, frame string) string {
	testing.SetRealm(user(who))
	return Next(cross(cur), plan, "w", frame)
}

func pop(cur realm, who address, plan, id string, token int, result string) {
	testing.SetRealm(user(who))
	Pop(cross(cur), plan, "w", id, token, result)
}

func par(cur realm, who address, plan, id string) {
	testing.SetRealm(user(who))
	Parallel(cross(cur), plan, "w", id)
}

// held reads "held\t<id>\t<token>\t<title>".
func held(t *testing.T, out string) (string, int) {
	t.Helper()
	parts := strings.Split(out, "\t")
	urequire.Equal(t, "held", parts[0], out)
	n := 0
	for _, c := range parts[2] {
		n = n*10 + int(c-'0')
	}
	return parts[1], n
}

func holderOfTodo(plan, id string) string { return getPlan(plan).todo(id).Holder }

// A sequential plan goes in order: b is not offered until a is done, and
// the session waiting for it gets it in the call that pops a.
func TestSequential(cur realm, t *testing.T) {
	fresh(cur, t, "seq")
	ids := add(cur, owner, "seq", "t", "a\nb", "write a\nwrite b")
	uassert.Equal(t, "t/a\nt/b", ids)
	id, tok := held(t, next(cur, w1, "seq", ""))
	uassert.Equal(t, "t/a", id)
	uassert.Equal(t, "waiting\t", next(cur, w2, "seq", ""))
	pop(cur, w1, "seq", "t/a", tok, "a is written")
	uassert.Equal(t, holderFor(w2, "w"), holderOfTodo("seq", "t/b"), "b was assigned to the waiter")
	uassert.Equal(t, "waiting\t", next(cur, w3, "seq", ""), "b is held: the rest wait")
}

// Order holds at every level: under a sequential root, the leaves of the
// second parallel group are not offered while the first group is open,
// and the first group's own check comes before them.
func TestNestedOrder(cur realm, t *testing.T) {
	fresh(cur, t, "nest")
	add(cur, owner, "nest", "t", "a\nb", "group a\ngroup b")
	par(cur, owner, "nest", "t/a")
	par(cur, owner, "nest", "t/b")
	add(cur, owner, "nest", "t/a", "a1\na2", "a one\na two")
	add(cur, owner, "nest", "t/b", "b1\nb2", "b one\nb two")
	a1, tok1 := held(t, next(cur, w1, "nest", ""))
	a2, tok2 := held(t, next(cur, w2, "nest", ""))
	uassert.Equal(t, "t/a/a1", a1)
	uassert.Equal(t, "t/a/a2", a2)
	uassert.Equal(t, "waiting\t", next(cur, w3, "nest", ""), "b's leaves wait for group a")
	pop(cur, w1, "nest", a1, tok1, "done")
	uassert.Equal(t, "", holderOfTodo("nest", "t/b/b1"))
	pop(cur, w2, "nest", a2, tok2, "done")
	// group a has nothing open under it: its check goes to the waiter first
	uassert.Equal(t, holderFor(w3, "w"), holderOfTodo("nest", "t/a"))
	// b's leaves still wait: a is held for its check, not done
	uassert.Equal(t, "waiting\t", next(cur, w1, "nest", ""))
	testing.SetRealm(user(w3))
	Pop(cross(cur), "nest", "w", "t/a", int(getPlan("nest").todo("t/a").Token), "group a checked")
	uassert.Equal(t, holderFor(w1, "w"), holderOfTodo("nest", "t/b/b1"), "a done: b's first leaf went to the waiter")
	b2, _ := held(t, next(cur, w2, "nest", ""))
	uassert.Equal(t, "t/b/b2", b2)
}

// A parallel parent offers all its todos at once; a frame's holder is told
// to pop it when nothing is left open under it.
func TestParallelAndPopFrame(cur realm, t *testing.T) {
	fresh(cur, t, "par")
	testing.SetRealm(user(w1))
	id, tok := held(t, Take(cross(cur), "par", "w", "t"))
	uassert.Equal(t, "t", id)
	par(cur, w1, "par", "t")
	add(cur, w1, "par", "t", "x\ny", "x\ny")
	x, tx := held(t, next(cur, w2, "par", ""))
	y, ty := held(t, next(cur, w3, "par", ""))
	uassert.Equal(t, "t/x", x)
	uassert.Equal(t, "t/y", y)
	pop(cur, w2, "par", x, tx, "x done")
	pop(cur, w3, "par", y, ty, "y done")
	uassert.Equal(t, "pop\tt", next(cur, w1, "par", "t"))
	pop(cur, w1, "par", "t", tok, "all done")
	uassert.Equal(t, "nothing\t", next(cur, w2, "par", ""))
}

// A hold whose lease ran out comes back on the next call; the old holder's
// Pop names a stale token and is refused.
func TestLeaseLapses(cur realm, t *testing.T) {
	fresh(cur, t, "lease")
	add(cur, owner, "lease", "t", "a", "a")
	_, tok := held(t, next(cur, w1, "lease", ""))
	testing.SkipHeights(13) // 13 blocks: past the 12-block lease
	id, tok2 := held(t, next(cur, w2, "lease", ""))
	uassert.Equal(t, "t/a", id)
	uassert.NotEqual(t, tok, tok2)
	uassert.AbortsContains(t, cur, "you do not hold t/a", func() {
		pop(cur, w1, "lease", "t/a", tok, "late")
	})
	pop(cur, w2, "lease", "t/a", tok2, "done")
}

// Purge frees a closed plan a page of todos at a time, then the plan.
func TestPurge(cur realm, t *testing.T) {
	fresh(cur, t, "pg")
	for c := 0; c < 4; c++ {
		var names, titles []string
		for i := 0; i < 64 && c*64+i < 250; i++ {
			names = append(names, "a"+strconv.Itoa(c*64+i))
			titles = append(titles, "todo "+strconv.Itoa(c*64+i))
		}
		add(cur, owner, "pg", "t", strings.Join(names, "\n"), strings.Join(titles, "\n"))
	}
	held(t, next(cur, w1, "pg", "")) // an agent with a hold, and so a todo it points at
	// The caller is the realm set before AbortsContains, not inside it.
	testing.SetRealm(user(owner))
	uassert.AbortsContains(t, cur, "plan pg is open: Close it first", func() { Purge(cross(cur), "pg") })
	testing.SetRealm(user(owner))
	Close(cross(cur), "pg")
	testing.SetRealm(user(w1))
	uassert.AbortsContains(t, cur, "only the plan's owner may do this", func() { Purge(cross(cur), "pg") })
	testing.SetRealm(user(owner))
	uassert.Equal(t, 51, Purge(cross(cur), "pg"), "251 todos, 200 a call")
	uassert.True(t, hasPlan("pg"), "the plan stays until its last todo goes")
	uassert.Equal(t, 0, Purge(cross(cur), "pg"))
	uassert.False(t, hasPlan("pg"), "the plan is gone")
}

// Renew keeps a hold past its first lease.
func TestRenew(cur realm, t *testing.T) {
	fresh(cur, t, "renew")
	add(cur, owner, "renew", "t", "a", "a")
	_, tok := held(t, next(cur, w1, "renew", ""))
	testing.SkipHeights(8)
	testing.SetRealm(user(w1))
	Renew(cross(cur), "renew", "w", "t/a", "", false)
	testing.SkipHeights(8)
	uassert.Equal(t, "waiting\t", next(cur, w2, "renew", ""), "renewed: still held")
	pop(cur, w1, "renew", "t/a", tok, "done")
}

// Waiters are served oldest first, one todo each.
func TestWaitersFIFO(cur realm, t *testing.T) {
	fresh(cur, t, "fifo")
	par(cur, owner, "fifo", "t")
	add(cur, owner, "fifo", "t", "a", "a")
	_, tok := held(t, next(cur, w1, "fifo", ""))
	uassert.Equal(t, "waiting\t", next(cur, w2, "fifo", ""))
	uassert.Equal(t, "waiting\t", next(cur, w3, "fifo", ""))
	add(cur, owner, "fifo", "t", "b", "b")
	uassert.Equal(t, holderFor(w2, "w"), holderOfTodo("fifo", "t/b"), "the oldest waiter")
	add(cur, owner, "fifo", "t", "c", "c")
	uassert.Equal(t, holderFor(w3, "w"), holderOfTodo("fifo", "t/c"))
	pop(cur, w1, "fifo", "t/a", tok, "done")
}

// Drop closes a todo with its open todos; Reopen brings one back, and its
// closed parent with it.
func TestDropReopen(cur realm, t *testing.T) {
	fresh(cur, t, "dr")
	add(cur, owner, "dr", "t", "a\nb", "a\nb")
	add(cur, owner, "dr", "t/a", "a1", "a1")
	testing.SetRealm(user(owner))
	Drop(cross(cur), "dr", "w", "t/a", "not needed")
	uassert.Equal(t, "dropped", getPlan("dr").todo("t/a/a1").Status)
	id, tok := held(t, next(cur, w1, "dr", ""))
	uassert.Equal(t, "t/b", id)
	pop(cur, w1, "dr", id, tok, "b")
	_, tokT := held(t, next(cur, w1, "dr", ""))
	pop(cur, w1, "dr", "t", tokT, "all")
	testing.SetRealm(user(owner))
	Reopen(cross(cur), "dr", "w", "t/a/a1", "needed after all")
	uassert.Equal(t, "", getPlan("dr").todo("t").Status, "the root reopened with it")
	uassert.Equal(t, "", getPlan("dr").todo("t/a").Status)
	got, _ := held(t, next(cur, w2, "dr", ""))
	uassert.Equal(t, "t/a/a1", got)
}

// Refusals: names in use, a closed plan, non-members, too many holds, an
// oversized result.
func TestRefusals(cur realm, t *testing.T) {
	fresh(cur, t, "ref")
	add(cur, owner, "ref", "t", "a", "a")
	uassert.AbortsContains(t, cur, "a todo named t/a exists", func() { add(cur, owner, "ref", "t", "a", "again") })
	uassert.AbortsContains(t, cur, "as many names as titles", func() { add(cur, owner, "ref", "t", "x\ny", "x") })
	testing.SetRealm(user(owner))
	NewPlan(cross(cur), "closed-plan", "members only", false, 60)
	uassert.AbortsContains(t, cur, "is not a member", func() { next(cur, other, "closed-plan", "") })
	testing.SetRealm(user(owner))
	AddMember(cross(cur), "closed-plan", other.String())
	uassert.Equal(t, "held\tt\t", next(cur, other, "closed-plan", "")[:7])
	_, tok := held(t, next(cur, w1, "ref", ""))
	uassert.AbortsContains(t, cur, "longer than 4000", func() {
		pop(cur, w1, "ref", "t/a", tok, strings.Repeat("x", 4001))
	})
}

func TestViews(cur realm, t *testing.T) {
	fresh(cur, t, "view")
	add(cur, owner, "view", "t", "a\nb", "a \"quoted\"\nb")
	held(t, next(cur, w1, "view", ""))
	j := PlanJSON("view")
	uassert.True(t, strings.Contains(j, `"todos":3`), j)
	uassert.True(t, strings.Contains(j, `"held":1`), j)
	page := PlanPage("view", "", 2)
	uassert.True(t, strings.HasPrefix(page, `[{"id":"t"`), page)
	uassert.True(t, strings.Contains(page, `\"quoted\"`), page)
	rest := PlanPage("view", "t/a", 10)
	uassert.True(t, strings.HasPrefix(rest, `[{"id":"t/b"`), rest)
}

// labelled calls: one address, many agents.
func nextAs(cur realm, who address, label, plan, frame string) string {
	testing.SetRealm(user(who))
	return Next(cross(cur), plan, label, frame)
}

func popNextAs(cur realm, who address, label, plan, id string, token int, frame string) string {
	testing.SetRealm(user(who))
	return PopNext(cross(cur), plan, label, id, token, label+" did "+id, frame)
}

// PopNext pops and takes the next todo in one call; with nothing free it
// waits, and what a pop frees goes to the oldest waiter first.
func TestPopNext(cur realm, t *testing.T) {
	fresh(cur, t, "pn")
	par(cur, owner, "pn", "t")
	add(cur, owner, "pn", "t", "a\nb\nc", "a\nb\nc")
	a, ta := held(t, nextAs(cur, w1, "x", "pn", ""))
	uassert.Equal(t, "t/a", a)
	b, tb := held(t, popNextAs(cur, w1, "x", "pn", a, ta, ""))
	uassert.Equal(t, "t/b", b)
	c, tc := held(t, nextAs(cur, w2, "y", "pn", ""))
	uassert.Equal(t, "t/c", c)
	uassert.Equal(t, "waiting\t", popNextAs(cur, w2, "y", "pn", c, tc, ""), "b is held: y waits")
	j := PlanJSON("pn")
	uassert.True(t, strings.Contains(j, `"held":1`), j)
	uassert.True(t, strings.Contains(j, `"waiting":1`), j)
	// b's pop leaves the root with nothing open: its check goes to y, the
	// waiter, before x looks for more
	uassert.Equal(t, "waiting\t", popNextAs(cur, w1, "x", "pn", b, tb, ""))
	uassert.Equal(t, w2.String()+"|-|y", holderOfTodo("pn", "t"))
	testing.SetRealm(user(w2))
	Pop(cross(cur), "pn", "y", "t", int(getPlan("pn").Root.Token), "all checked")
	uassert.Equal(t, "nothing\t", nextAs(cur, w1, "x", "pn", ""))
	j = PlanJSON("pn")
	uassert.True(t, strings.Contains(j, `"open_todos":0`), j)
	uassert.True(t, strings.Contains(j, `"held":0`), j)
	uassert.True(t, strings.Contains(j, `"waiting":0`), j)
}

// PopNext refuses what Pop refuses: a hold the caller does not have, a
// stale token.
func TestPopNextRefusals(cur realm, t *testing.T) {
	fresh(cur, t, "pnr")
	add(cur, owner, "pnr", "t", "a\nb", "a\nb")
	a, ta := held(t, nextAs(cur, w1, "x", "pnr", ""))
	uassert.AbortsContains(t, cur, "you do not hold t/a", func() { popNextAs(cur, w2, "y", "pnr", a, ta, "") })
	uassert.AbortsContains(t, cur, "lost and taken again", func() { popNextAs(cur, w1, "x", "pnr", a, ta+1, "") })
	b, _ := held(t, popNextAs(cur, w1, "x", "pnr", a, ta, ""))
	uassert.Equal(t, "t/b", b, "a sequential root's next todo")
}

// Avail is a bitset: twenty children span three bytes, are offered in
// order, and one released goes back to the waiter.
func TestManyChildren(cur realm, t *testing.T) {
	fresh(cur, t, "many")
	par(cur, owner, "many", "t")
	var names, titles []string
	for i := 0; i < 20; i++ {
		names = append(names, "k"+strconv.Itoa(i))
		titles = append(titles, "kid "+strconv.Itoa(i))
	}
	add(cur, owner, "many", "t", strings.Join(names, "\n"), strings.Join(titles, "\n"))
	toks := map[string]int{}
	for i := 0; i < 20; i++ {
		id, tok := held(t, nextAs(cur, w1, "a"+strconv.Itoa(i), "many", ""))
		uassert.Equal(t, "t/k"+strconv.Itoa(i), id)
		toks[id] = tok
	}
	uassert.Equal(t, "waiting\t", nextAs(cur, w2, "late", "many", ""))
	testing.SetRealm(user(w1))
	Release(cross(cur), "many", "a12", "t/k12", "")
	uassert.Equal(t, w2.String()+"|-|late", holderOfTodo("many", "t/k12"), "the released todo went to the waiter")
	for i := 0; i < 20; i++ {
		if i == 12 {
			continue
		}
		id := "t/k" + strconv.Itoa(i)
		testing.SetRealm(user(w1))
		Pop(cross(cur), "many", "a"+strconv.Itoa(i), id, toks[id], "done")
	}
	testing.SetRealm(user(w2))
	Pop(cross(cur), "many", "late", "t/k12", int(getPlan("many").todo("t/k12").Token), "done")
	id, _ := held(t, nextAs(cur, w3, "z", "many", ""))
	uassert.Equal(t, "t", id, "all twenty done: the root's check")
}

// Leases run out in every group: each call takes back up to four, and the
// expiring index empties.
func TestLapseAcrossGroups(cur realm, t *testing.T) {
	fresh(cur, t, "lap")
	par(cur, owner, "lap", "t")
	add(cur, owner, "lap", "t", "g1\ng2", "g1\ng2")
	par(cur, owner, "lap", "t/g1")
	par(cur, owner, "lap", "t/g2")
	add(cur, owner, "lap", "t/g1", "a\nb\nc", "a\nb\nc")
	add(cur, owner, "lap", "t/g2", "a\nb\nc", "a\nb\nc")
	for i := 0; i < 6; i++ {
		held(t, nextAs(cur, w1, "h"+strconv.Itoa(i), "lap", ""))
	}
	uassert.True(t, strings.Contains(PlanJSON("lap"), `"held":6`))
	testing.SkipHeights(13) // 13 blocks: every lease has run out
	got, _ := held(t, nextAs(cur, w2, "new", "lap", ""))
	uassert.Equal(t, "t/g1/a", got, "the first lapsed todo is free again")
	j := PlanJSON("lap")
	uassert.True(t, strings.Contains(j, `"held":3`), "four lapsed, one taken: "+j)
	testing.SetRealm(user(w2))
	Reclaim(cross(cur), "lap", "new")
	j = PlanJSON("lap")
	uassert.True(t, strings.Contains(j, `"held":1`), "the other two lapsed: "+j)
	uassert.Equal(t, "", getPlan("lap").todo("t/g2").Held)
	uassert.Equal(t, 1, strings.Count(getPlan("lap").Due, "\n"), "only g1 holds a lease: "+getPlan("lap").Due)
}

// One agent holds at most maxHolds todos.
func TestMaxHolds(cur realm, t *testing.T) {
	fresh(cur, t, "cap")
	par(cur, owner, "cap", "t")
	var names []string
	for i := 0; i < 10; i++ {
		names = append(names, "k"+strconv.Itoa(i))
	}
	add(cur, owner, "cap", "t", strings.Join(names, "\n"), strings.Join(names, "\n"))
	for i := 0; i < 8; i++ {
		held(t, nextAs(cur, w1, "greedy", "cap", ""))
	}
	uassert.AbortsContains(t, cur, "holds 8 todos", func() { nextAs(cur, w1, "greedy", "cap", "") })
	held(t, nextAs(cur, w1, "other", "cap", ""))
}

// A waiter whose place ran out is passed over; the next one gets the todo.
func TestWaiterLapses(cur realm, t *testing.T) {
	fresh(cur, t, "wl")
	par(cur, owner, "wl", "t")
	add(cur, owner, "wl", "t", "a", "a")
	_, tok := held(t, nextAs(cur, w1, "x", "wl", ""))
	uassert.Equal(t, "waiting\t", nextAs(cur, w2, "early", "wl", ""))
	testing.SkipHeights(8) // 8 blocks
	testing.SetRealm(user(w1))
	Renew(cross(cur), "wl", "x", "t/a", "", false)
	uassert.Equal(t, "waiting\t", nextAs(cur, w3, "late", "wl", ""))
	testing.SkipHeights(8) // the early waiter's place has run out, not the late one's
	add(cur, owner, "wl", "t", "b", "b")
	uassert.Equal(t, w3.String()+"|-|late", holderOfTodo("wl", "t/b"))
	uassert.True(t, strings.Contains(PlanJSON("wl"), `"waiting":0`))
	testing.SetRealm(user(w1))
	Pop(cross(cur), "wl", "x", "t/a", tok, "done")
}

// Waiters past one page of the queue are served in order.
func TestQueuePages(cur realm, t *testing.T) {
	fresh(cur, t, "qp")
	par(cur, owner, "qp", "t")
	add(cur, owner, "qp", "t", "first", "first")
	held(t, nextAs(cur, w1, "x", "qp", ""))
	for i := 0; i < 40; i++ {
		uassert.Equal(t, "waiting\t", nextAs(cur, w2, "q"+strconv.Itoa(i), "qp", ""))
	}
	w := WaitingJSON("qp")
	uassert.Equal(t, 40, strings.Count(w, `"holder"`), w)
	for i := 0; i < 40; i++ {
		id := "t/n" + strconv.Itoa(i)
		add(cur, owner, "qp", "t", "n"+strconv.Itoa(i), "n")
		uassert.Equal(t, w2.String()+"|-|q"+strconv.Itoa(i), holderOfTodo("qp", id))
	}
	uassert.True(t, strings.Contains(PlanJSON("qp"), `"waiting":0`))
	uassert.Equal(t, "[]", WaitingJSON("qp"))
}

// Drop lets go of the holds under what it drops.
func TestDropHeld(cur realm, t *testing.T) {
	fresh(cur, t, "dh")
	par(cur, owner, "dh", "t")
	add(cur, owner, "dh", "t", "g", "g")
	par(cur, owner, "dh", "t/g")
	add(cur, owner, "dh", "t/g", "a\nb", "a\nb")
	held(t, nextAs(cur, w1, "x", "dh", ""))
	held(t, nextAs(cur, w2, "y", "dh", ""))
	testing.SetRealm(user(owner))
	Drop(cross(cur), "dh", "w", "t/g", "not needed")
	j := PlanJSON("dh")
	uassert.True(t, strings.Contains(j, `"held":0`), j)
	uassert.Equal(t, "", getPlan("dh").Due)
	id, _ := held(t, nextAs(cur, w1, "x", "dh", ""))
	uassert.Equal(t, "t", id, "only the root is left")
}

// Waiters who left (Esc) take their entries with them: the waiter behind
// them is served in the call that frees a todo (review, 2026-11-04).
func TestLeftWaitersDoNotStarve(cur realm, t *testing.T) {
	fresh(cur, t, "st")
	par(cur, owner, "st", "t")
	add(cur, owner, "st", "t", "a", "a")
	held(t, nextAs(cur, w1, "x", "st", ""))
	for i := 0; i < 12; i++ {
		l := "s" + strconv.Itoa(i)
		uassert.Equal(t, "waiting\t", nextAs(cur, w2, l, "st", ""))
		testing.SetRealm(user(w2))
		Release(cross(cur), "st", l, "", "")
	}
	uassert.Equal(t, "waiting\t", nextAs(cur, w3, "late", "st", ""))
	add(cur, owner, "st", "t", "b", "b")
	uassert.Equal(t, w3.String()+"|-|late", holderOfTodo("st", "t/b"))
}

// The same across queues: a deeper frame's leavers do not stop the whole
// plan's waiter from being served.
func TestServeAcrossQueues(cur realm, t *testing.T) {
	fresh(cur, t, "cq")
	par(cur, owner, "cq", "t")
	add(cur, owner, "cq", "t", "g", "g")
	par(cur, owner, "cq", "t/g")
	add(cur, owner, "cq", "t/g", "a", "a")
	held(t, nextAs(cur, w1, "x", "cq", ""))
	for i := 0; i < 12; i++ {
		l := "s" + strconv.Itoa(i)
		uassert.Equal(t, "waiting\tt/g", nextAs(cur, w2, l, "cq", "t/g"))
		testing.SetRealm(user(w2))
		Release(cross(cur), "cq", l, "", "t/g")
	}
	uassert.Equal(t, "waiting\t", nextAs(cur, w3, "late", "cq", ""))
	add(cur, owner, "cq", "t/g", "b", "b")
	uassert.Equal(t, w3.String()+"|-|late", holderOfTodo("cq", "t/g/b"))
}

// A frame to wait in must be a todo, and a queue nobody waits in goes.
func TestQueuesGo(cur realm, t *testing.T) {
	fresh(cur, t, "qg")
	par(cur, owner, "qg", "t")
	add(cur, owner, "qg", "t", "a", "a")
	_, tok := held(t, nextAs(cur, w1, "x", "qg", ""))
	uassert.AbortsContains(t, cur, "no todo", func() {
		testing.SetRealm(user(w2))
		Renew(cross(cur), "qg", "g", "", "no-such-frame", true)
	})
	uassert.Equal(t, "waiting\tt", nextAs(cur, w2, "y", "qg", "t"))
	uassert.Equal(t, "waiting\t", nextAs(cur, w3, "z", "qg", ""))
	uassert.Equal(t, 2, len(getPlan("qg").Queues))
	testing.SetRealm(user(w2))
	Release(cross(cur), "qg", "y", "", "t")
	uassert.Equal(t, 1, len(getPlan("qg").Queues), "t's queue went with its last waiter")
	testing.SetRealm(user(w1))
	Pop(cross(cur), "qg", "x", "t/a", tok, "done")
	uassert.Equal(t, w3.String()+"|-|z", holderOfTodo("qg", "t"), "the root's check went to the waiter")
	uassert.Equal(t, 0, len(getPlan("qg").Queues))
	uassert.Equal(t, 0, getPlan("qg").WaitN)
}

// A call serves at most two waiters; the todos it freed beyond that are
// served by the calls after it, a Reclaim too.
func TestPendingServed(cur realm, t *testing.T) {
	fresh(cur, t, "pd")
	par(cur, owner, "pd", "t")
	add(cur, owner, "pd", "t", "a", "a")
	held(t, nextAs(cur, w1, "x", "pd", ""))
	for i := 0; i < 5; i++ {
		uassert.Equal(t, "waiting\t", nextAs(cur, w2, "q"+strconv.Itoa(i), "pd", ""))
	}
	add(cur, owner, "pd", "t", "b\nc\nd\ne\nf", "b\nc\nd\ne\nf")
	uassert.True(t, strings.Contains(PlanJSON("pd"), `"waiting":3`), PlanJSON("pd"))
	testing.SetRealm(user(w1))
	Reclaim(cross(cur), "pd", "x")
	uassert.True(t, strings.Contains(PlanJSON("pd"), `"waiting":1`), PlanJSON("pd"))
	Reclaim(cross(cur), "pd", "x")
	uassert.True(t, strings.Contains(PlanJSON("pd"), `"waiting":0`), PlanJSON("pd"))
	for i, id := range []string{"t/b", "t/c", "t/d", "t/e", "t/f"} {
		uassert.Equal(t, w2.String()+"|-|q"+strconv.Itoa(i), holderOfTodo("pd", id), "oldest first")
	}
	uassert.Equal(t, "", getPlan("pd").Pending)
}

// Due stays sorted, so the earliest lease to run out comes back first.
func TestDueSorted(cur realm, t *testing.T) {
	fresh(cur, t, "ds")
	par(cur, owner, "ds", "t")
	add(cur, owner, "ds", "t", "g1\ng2\ng3", "g1\ng2\ng3")
	for _, g := range []string{"t/g1", "t/g2", "t/g3"} {
		add(cur, owner, "ds", g, "a", "a")
	}
	// g1's leaf first, g2's 10 s later, g3's 10 s after that; then g1's
	// hold renewed, so it runs out last
	got, _ := held(t, nextAs(cur, w1, "h1", "ds", ""))
	uassert.Equal(t, "t/g1/a", got)
	testing.SkipHeights(2)
	got, _ = held(t, nextAs(cur, w1, "h2", "ds", ""))
	uassert.Equal(t, "t/g2/a", got)
	testing.SkipHeights(2)
	got, _ = held(t, nextAs(cur, w1, "h3", "ds", ""))
	uassert.Equal(t, "t/g3/a", got)
	testing.SkipHeights(2)
	testing.SetRealm(user(w1))
	Renew(cross(cur), "ds", "h1", "t/g1/a", "", false)
	d := getPlan("ds").Due
	lines := strings.Split(strings.TrimSpace(d), "\n")
	uassert.Equal(t, 3, len(lines), d)
	uassert.True(t, strings.HasSuffix(lines[0], " t/g2") && strings.HasSuffix(lines[1], " t/g3") && strings.HasSuffix(lines[2], " t/g1"), "sorted by lease end: "+d)
	// 55 s on: g2's and g3's leases ran out, g1's renewed one did not
	testing.SkipHeights(11)
	testing.SetRealm(user(w2))
	Reclaim(cross(cur), "ds", "y")
	uassert.Equal(t, "", holderOfTodo("ds", "t/g2/a"))
	uassert.Equal(t, "", holderOfTodo("ds", "t/g3/a"))
	uassert.Equal(t, w1.String()+"|-|h1", holderOfTodo("ds", "t/g1/a"))
	uassert.True(t, strings.HasSuffix(strings.TrimSpace(getPlan("ds").Due), " t/g1"), getPlan("ds").Due)
}

// Parallel on a todo already parallel still serves what its call took
// back.
func TestParallelAgainServes(cur realm, t *testing.T) {
	fresh(cur, t, "pa")
	par(cur, owner, "pa", "t")
	add(cur, owner, "pa", "t", "a", "a")
	held(t, nextAs(cur, w1, "x", "pa", ""))
	uassert.Equal(t, "waiting\t", nextAs(cur, w2, "y", "pa", ""))
	testing.SkipHeights(8)
	testing.SetRealm(user(w2))
	Renew(cross(cur), "pa", "y", "", "", true)
	testing.SkipHeights(6) // x's lease ran out; y's place did not
	par(cur, owner, "pa", "t")
	uassert.Equal(t, w2.String()+"|-|y", holderOfTodo("pa", "t/a"))
}
