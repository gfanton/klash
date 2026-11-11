// Copyright (C) 2026 Jae Kwon
//
// This file is part of gnosh's GnoVM machine subsystem. It is licensed under
// the GNO Network General Public License, version 6, as published by
// NewTendermint, LLC or its successor, or any later version of that License
// that the Licensor accepts as the proxy under its section 14, by a statement
// published in the canonical copy of internal/gnovm/LICENSE.md; together with
// the Additional Terms (Strong Attribution) in that file.

package gnomem

import (
	"strconv"
	"strings"
)

// Frames (FRAMES.md): the task tree seen through a stack. A Frame is a
// piece of work in mem, a todo; entering one (Push) marks
// where gnosh will cut the model's context, and leaving it with a result
// (Pop) cuts everything since. A thought is a labelled line kept on
// the frame the model stands in: it costs nothing, cuts nothing, and is
// how anything worth keeping outlives a pop. The stack itself is gnosh's,
// derived from the session's own pushes and pops; the realm is told it
// through frames(), and tells gnosh each push and pop through pushed() and
// popped().

// Frame is a piece of work in mem, seen through the stack. It is a
// snapshot, read-only; mem's calls change mem.
type Frame struct {
	Id       string    // its path of names: `fix-flaky`, `fix-flaky/find-cause`; "" for the root
	Purpose  string    // what it is for; "" for a step made from a thought with no reason given
	Result   string    // what came of it, once popped
	State    string    // "open", "doing", "waiting", "asked", "done" or "dropped": what NextTodo reads
	Doing    string    // the session working it, when State is "doing"
	Todos    []*Frame  // its todos, in order, one level
	Thoughts []Thought // its thoughts, oldest first
	m        *Mem
}

// Thought is a labelled line kept on a frame, and the step it was promoted
// into, if any. Its name is given to Think, or made from its text.
type Thought struct {
	Text  string
	Frame *Frame // the step it was promoted into; nil until then
	name  string // its label, unique among mem's thoughts
	frame string // the id of the frame it is on
	m     *Mem
}

// Fact is a fact as Recall returns it.
type Fact struct {
	Body string
	name string
}

// Namable is what a Frame, a Thought and a Fact have in common: a piece
// of memory under a name, given or made, which Recall returns. *Frame,
// Thought and Fact implement it; a type switch tells them apart.
type Namable interface {
	Name() string   // how a call names it: a frame's Id, a thought's name, a fact's name
	Kind() string   // "frame", "thought" or "fact"
	Says() string   // a frame's purpose, a thought's text, a fact's body
	String() string // how it is shown
}

// maxRecall is how many Namables Recall returns at most.
const maxRecall = 5

func (f *Frame) Name() string { return f.Id }
func (f *Frame) Kind() string { return "frame" }
func (f *Frame) Says() string { return f.Purpose }

// String shows the frame as mem.Show would: the root as the whole outline.
func (f *Frame) String() string {
	switch {
	case f == nil:
		return "no frame\n"
	case f.m == nil:
		return "frame `" + f.Id + "` " + quote(f.Purpose) + "\n"
	case f.Id == "":
		return f.m.Show()
	}
	return f.m.Show(f.Id)
}

// NextTodo is the todo mem.Next() would step into from f: its first todo,
// in the order of its Todos, whose State is "open", or "doing" by this
// session; with other sessions working, a free todo under one of theirs
// when none beside it is free (PARALLEL.md). nil when there is none, and
// at the unbound root, where a todo is entered by id or made and pushed.
func (f *Frame) NextTodo() *Frame {
	if f == nil || f.m == nil {
		return nil
	}
	return f.m.nextTodo(f.Id)
}

func (t Thought) Name() string { return t.name }
func (t Thought) Kind() string { return "thought" }
func (t Thought) Says() string { return t.Text }

func (t Thought) String() string {
	s := "thought `" + t.name + "` on `" + t.frame + "`: " + t.Text
	if t.Frame != nil {
		s += " (now the todo `" + t.Frame.Id + "`)"
	}
	return s + "\n"
}

func (f Fact) Name() string   { return f.name }
func (f Fact) Kind() string   { return "fact" }
func (f Fact) Says() string   { return f.Body }
func (f Fact) String() string { return "fact `" + f.name + "` = " + f.Body + "\n" }

// Frame is the present frame: the top of this session's stack, or, with
// the stack empty, the root: the goal the user's /goal bound the session
// to, or everything open. Given an id, it is that frame:
// mem.Frame("fix-flaky").Push() steps into a todo by id.
func (m *Mem) Frame(id ...string) *Frame {
	switch {
	case len(id) > 1:
		panic("Frame takes one id, or none for the present frame")
	case len(id) == 0:
		return m.frame(m.present())
	}
	n := m.resolve(id[0])
	if n == "" {
		return m.frame(bound())
	}
	if it := m.get(n); it == nil || it.Status == "forgotten" {
		panic(m.noFrame(n))
	}
	return m.frame(n)
}

// present is the frame this session is in: the top of its stack, or the
// root: the goal /goal bound it to, or "" for the unbound root.
func (m *Mem) present() string {
	if s := frames(); len(s) > 0 {
		return s[len(s)-1]
	}
	return bound()
}

// frame is frame id, with its todos one level deep and its thoughts; the
// unbound root's todos are the open top-level ones, in the order made.
func (m *Mem) frame(id string) *Frame {
	f := m.shallow(id)
	if id == "" {
		m.eachTask(func(task string, _ *Todo) bool {
			f.Todos = append(f.Todos, m.shallow(task))
			return false
		})
		return f
	}
	it := m.get(id)
	for _, s := range it.Steps {
		step := id + "/" + s
		if m.get(step).Status != "forgotten" {
			f.Todos = append(f.Todos, m.shallow(step))
		}
	}
	for _, n := range it.Notes {
		f.Thoughts = append(f.Thoughts, m.thoughtOf(id, n))
	}
	return f
}

// shallow is frame id without its todos and thoughts.
func (m *Mem) shallow(id string) *Frame {
	if id == "" {
		return &Frame{Purpose: "everything open", State: "open", m: m}
	}
	it := m.get(id)
	return &Frame{Id: id, Purpose: it.Title, Result: it.Result, State: m.state(id, it), Doing: m.doer(id, it), m: m}
}

// thoughtOf is thought n of frame id, as a Thought.
func (m *Mem) thoughtOf(id string, n note) Thought {
	t := Thought{name: n.Name, Text: n.Text, frame: id, m: m}
	if n.Step != "" {
		if s := m.get(n.Step); s != nil && s.Status != "forgotten" {
			t.Frame = m.shallow(n.Step)
		}
	}
	return t
}

// state is item it's State: its status once closed; else "asked" for the
// user, "waiting" for a day or words (its own or an item's above it),
// "doing" for a session's, or "open".
func (m *Mem) state(id string, it *Todo) string {
	switch {
	case it.Status != "":
		return it.Status
	case it.For != "":
		return "asked"
	case m.waitsAbove(id):
		return "waiting"
	case m.doer(id, it) != "":
		return "doing"
	}
	return "open"
}

// waitsAbove reports whether item id waits, itself or through an item
// above it.
func (m *Mem) waitsAbove(id string) bool {
	for n := id; n != ""; n = parentOf(n) {
		if m.waiting(m.get(n)) {
			return true
		}
	}
	return false
}

// nextTodo is Frame.NextTodo for frame id: the pick, entering a todo with
// todos under it (peers.gno).
func (m *Mem) nextTodo(id string) *Frame {
	if id == "" || m.get(id) == nil {
		return nil
	}
	if n := m.pickFor(id, caller(), true, nil); n != "" {
		return m.shallow(n)
	}
	return nil
}

// noFrame says that id names no frame, and what it might have been.
func (m *Mem) noFrame(id string) string {
	msg := "there is no frame " + named(id)
	low := strings.ToLower(strings.TrimSpace(id))
	switch {
	case low != id && m.get(low) != nil:
		return msg + ": names are lowercase, typed as printed: `" + low + "`"
	case m.formed(id) != "":
		return msg + ": did you mean `" + m.formed(id) + "`?"
	case m.titled(id) != "":
		return msg + ": that is a task; the todo is `" + m.titled(id) + "`"
	}
	return msg + "; mem.Show() lists them"
}

// Todo adds a todo and returns it: under the frame you are in, after its
// other todos, or, at the unbound root, a top-level one. Its name is made
// from the task: gnosh's small model's label of it, or the task's words,
// with -2, -3 when that name is taken. A task an open todo beside it
// already has is refused. Todos given after the task are added under it,
// in order, so a plan is one call: mem.Todo("make the port configurable",
// "add a --port flag", "document it", "test its default").Push().
func (m *Mem) Todo(task string, todos ...string) *Frame {
	return m.plan(m.present(), task, todos)
}

// Todo adds a todo under frame f, as mem.Todo does under the present frame,
// with any todos given after the task under it: a plan is made without
// entering it, mem.Todo("…").Todo("…").
func (f *Frame) Todo(task string, todos ...string) *Frame {
	if f == nil || f.m == nil {
		panic("no frame to add a todo to: mem.Todo(task) makes one")
	}
	return f.m.plan(f.Id, task, todos)
}

// Later adds a top-level todo from anywhere and returns it, with any todos
// given after the task under it, leaving the frame you are in alone: work
// found on the way that is not this todo's. Next from the root takes the
// top-level todos in the order they were made, so it comes after what is
// open now.
func (m *Mem) Later(task string, todos ...string) *Frame {
	return m.plan("", task, todos)
}

// plan adds a todo for task under parent ("" for the root), and todos under
// it, in order. Steps the user listed became todos nested one under another
// when they took four calls and a push (QA, 2026-11-04).
func (m *Mem) plan(parent, task string, todos []string) *Frame {
	f := m.todoUnder(parent, task)
	if len(todos) == 0 {
		return f
	}
	for _, t := range todos {
		m.todoUnder(f.Id, t)
	}
	return m.frame(f.Id)
}

// Wait holds the todo back until a day, or until what the words say (see
// Mem.Wait): mem.Todo("renew the TLS cert").Wait("2026-12-01").
func (f *Frame) Wait(until string) *Frame {
	if f == nil || f.m == nil || f.Id == "" {
		panic("no todo to wait: mem.Todo(task) makes one")
	}
	f.m.Wait(f.Id, until)
	return f
}

// Ask asks the user something, as a todo under f (see Mem.Ask).
func (f *Frame) Ask(question string) *Frame {
	if f == nil || f.m == nil || f.Id == "" {
		panic("no todo to ask under: mem.Todo(task) makes one, or mem.Ask(\"\", question) asks at the root")
	}
	f.m.Ask(f.Id, question)
	return f
}

// todoUnder adds a todo for task under parent ("" for the root).
func (m *Mem) todoUnder(parent, task string) *Frame {
	task = clip(text("the task", task), maxTitle)
	if task == "" {
		panic("a todo needs a task: mem.Todo(\"what to get done\")")
	}
	if validName(task) && strings.Contains(task, "-") {
		// A model that writes mem.Todo("fix-flaky") means the todo of that
		// name, as it would have given one; a nested todo named from it is
		// not what it meant.
		panic("`" + task + "` is a name, not a task: mem.Frame(" + quote(task) + ") is the todo of that name, and .Todo(\"what to get done\") on it adds under it; a task is words")
	}
	if parent == "" {
		m.refuseOpenTitle(task)
		id := m.freeName(m.madeName(task, "todo"), func(n string) bool { return m.freeTop(n) })
		m.do(Op{Kind: "add", Name: id, New: task, By: caller()})
		println("added " + ref(id, task))
		return m.frame(id)
	}
	p := m.openItem(parent)
	m.eachStep(parent, func(_ string, s *Todo) bool {
		if s.Status == "" && sameTitle(s.Title) == sameTitle(task) {
			panic(ref(parent, p.Title) + " already has an open todo " + strconv.Quote(task))
		}
		return false
	})
	id := parent + "/" + m.freeStep(parent, m.madeName(task, "todo"))
	m.addStep(parent, id, task)
	println("added " + ref(id, task))
	return m.frame(id)
}

// addStep adds step id, titled title, under open item parent.
func (m *Mem) addStep(parent, id, title string) {
	p := m.openItem(parent)
	if p.For != "" {
		panic(ref(parent, p.Title) + " is for the user: it has no todos")
	}
	if len(p.Steps) >= maxSteps {
		panic(ref(parent, p.Title) + " has " + strconv.Itoa(maxSteps) + " todos, the most an item may have")
	}
	m.do(Op{Kind: "add", Name: id, New: title, By: caller()})
}

// Next steps into the next todo, which becomes this session's, the top of
// its stack, and from where the context will be cut when it is popped:
// under the frame you stand in, the first open todo (or one this session
// is doing), as Frame().NextTodo() gives it; at the unbound root, the
// first todo this session can do now, going through the top-level todos in
// the order they were made, and under each, in outline order, so a
// session taking up another's work needs no name. From the root it enters
// the todos above that one too, from the top, so that popping it leaves
// the session in its parent, as in a plan it pushed itself; it does not
// hold them, so another session can take up the plan once this one stops.
// Under a parallel todo, whose todos are any session's, it enters the todo
// alone. When there is none it is a kept no-op that says what to do
// instead. A particular frame is entered with .Push() on it:
// mem.Todo("…").Push(), mem.Frame(id).Push(), mem.Thought("port").Push().
//
// With other sessions working on the plan, a Next that has nothing for you
// now waits for them when it can (only a bare mem.Next() ending the input),
// when you would have a todo, or could pop the frame you stand in, once
// they finish: it prints nothing, and gnosh runs it again when mem changes
// (PARALLEL.md).
func (m *Mem) Next() {
	p := m.present()
	if p == "" {
		if n := m.nextAtRoot(); n != "" {
			m.pushChain(n, "")
			return
		}
	} else if f := m.nextTodo(p); f != nil {
		m.push(f.Id, "")
		return
	}
	if mayWait() {
		me := caller()
		var held []string
		switch {
		case p == "":
			m.eachTask(func(task string, g *Todo) bool {
				if g.For != "" || m.waiting(g) {
					return false // as nextAtRoot passes it over
				}
				held = m.waitsFor(task, me, false)
				return len(held) > 0
			})
		case m.get(p).OpenSteps > 0:
			held = m.waitsFor(p, me, true)
		}
		if len(held) > 0 {
			for _, n := range held {
				waitOn(n, m.holder(n, m.get(n), me))
			}
			pushed("", "")
			return
		}
	}
	pushed("", "")
	println(m.nothingNext())
}

// nextAtRoot is the todo Next steps into from the unbound root: going
// through the open top-level todos in the order they were made, passing
// over one that waits or is for the user, the first todo this session can
// do now under one, in outline order, or that one itself when it is free
// and has no open todo; "" when there is none. Models made a plan's steps
// as top-level todos, in order, and the newest was taken first, Median
// before Max (QA, 2026-11-04).
func (m *Mem) nextAtRoot() string {
	me := caller()
	found := ""
	m.eachTask(func(task string, g *Todo) bool {
		if g.For != "" || m.waiting(g) {
			return false
		}
		if found = m.next(task, me); found == "" && g.OpenSteps == 0 && m.free(task, g, me) {
			found = task
		}
		return found != ""
	})
	return found
}

// eachTask calls fn on each open top-level todo, in the order they were
// made, until it returns true.
func (m *Mem) eachTask(fn func(name string, it *Todo) bool) {
	m.order.Iterate("", "", func(_ string, v any) bool {
		n := v.(string)
		return fn(n, m.get(n))
	})
}

// pushChain enters todo id from the root together with the todos above it,
// from the top, so that the stack matches the plan: popping id leaves the
// session in its parent. A model that popped its last todo and then "the
// parent", never on its stack, lost both pops (QA, 2026-11-04). The todos
// above are entered on the way (Via), not held: they are this session's
// while it works, and anyone's once it stops, so a plan carried into a new
// session can still be closed. A todo above that another session holds, or
// that a working session entered on its way, is passed over. Under a
// parallel todo, whose todos are any session's and whose own work is
// whoever's comes when they are done, id is entered alone (PARALLEL.md).
// Next from the root, and a thought promoted from the root into a todo
// under its open frame, take this way.
func (m *Mem) pushChain(id, thought string) {
	var above []string
	for p := parentOf(id); p != ""; p = parentOf(p) {
		if m.get(p).Parallel {
			m.push(id, thought)
			return
		}
		above = append([]string{p}, above...)
	}
	me := caller()
	for _, p := range above {
		if it := m.get(p); it.Status == "" && m.holder(p, it, me) == "" {
			m.enter(p, "", true)
		}
	}
	m.push(id, thought)
}

// Push steps into the frame: it becomes this session's, the top of its
// stack, and from here the context will be cut when it is popped. It
// returns the frame, so it chains: mem.Frame(id).Push().Ask("…").
func (f *Frame) Push() *Frame {
	if f == nil || f.m == nil {
		panic("no frame to push: mem.Frame(id) or mem.Todo(task) gives one")
	}
	f.m.push(f.Id, "")
	return f.m.frame(f.Id)
}

// Done closes the todo with what came of it, as mem.Done does by its name:
// mem.Frame("release/tag").Done("tagged v2.0.0 at 3f2a1c").
func (f *Frame) Done(result ...string) {
	f.todo("to close").m.Done(f.Id, result...)
}

// Drop closes the todo as not to be done, with why, as mem.Drop does by
// its name.
func (f *Frame) Drop(why string) {
	f.todo("to drop").m.Drop(f.Id, why)
}

// Reopen opens the todo again, done or dropped, with why, as mem.Reopen
// does by its name.
func (f *Frame) Reopen(why ...string) {
	f.todo("to reopen").m.Reopen(f.Id, why...)
}

// Note keeps a finding on the todo as a thought named from its words, as
// mem.Note does by its name: mem.Todo("rotate the staging key").Note("it
// expires Friday").
func (f *Frame) Note(text string) {
	f.todo("to note on").m.Note(f.Id, text)
}

// Think keeps a thought on the todo, as mem.Think does on the present
// frame: a note for a later todo, read when it is entered.
// mem.Frame("release/tag").Think("sign with the 2026 key").
func (f *Frame) Think(args ...string) Thought {
	return f.todo("to think on").m.thinkOn(f.Id, args...)
}

// todo is f, refusing the root and a frame from no mem.
func (f *Frame) todo(what string) *Frame {
	if f == nil || f.m == nil || f.Id == "" {
		panic("no todo " + what + ": mem.Todo(task) makes one, and mem.Frame(name) gives one by its name")
	}
	return f
}

// Leave steps out of the present frame and leaves its todo open, for later
// or for another session: gnosh cuts the context as a pop does, and nobody
// holds the todo then. why, if given, is kept on it as a thought. A todo
// entered by mistake, or one the user wants left for later, is left; one
// that is done is popped, and one given up is dropped. Pop records the todo
// done and Drop given up, so a model that meant neither gave up a todo the
// user wanted kept (QA, 2026-11-04).
func (m *Mem) Leave(why ...string) {
	stack := frames()
	if len(stack) == 0 {
		panic("nothing to leave: you are at the root, in no todo")
	}
	id := top(stack)
	it := m.item(id)
	if it.Status != "" {
		m.popClosed(id, it)
		return
	}
	reason := clip(text("why", strings.Join(why, " ")), maxText)
	if reason != "" {
		m.do(Op{Kind: "note", Name: id + "#" + m.freeThought(nameOf(reason, "note")), New: "left open: " + reason})
	}
	me := caller()
	if it.Doing == me {
		m.do(Op{Kind: "set", Name: id, Field: "doing", Old: me})
	}
	if it.Via == me {
		m.do(Op{Kind: "set", Name: id, Field: "via", Old: me})
	}
	result := "left open"
	if reason != "" {
		result += ": " + reason
	}
	popped(id, it.Title, result, len(it.Notes))
	msg := "left " + ref(id, it.Title) + " open: it stays a todo, held by nobody, and is off your stack"
	if p := parentOf(id); p != "" && onStack(frames(), p) {
		msg += "; to take another todo of `" + p + "`: mem.Frame(\"name\").Push(), mem.Show(\"" + p + "\") lists them (mem.Next() would take this one again)"
	}
	println(msg)
}

// push enters frame id. thought is the text of the thought the frame was
// made from, when the push follows its making.
func (m *Mem) push(id, thought string) {
	m.enter(id, thought, false)
}

// enter enters frame id, as push; above, a todo entered on the way to one
// under it (pushChain), is not held but marked as entered on the way (Via),
// and says only where the model now also stands.
func (m *Mem) enter(id, thought string, above bool) {
	stack := frames()
	switch {
	case id == "":
		panic("the root is not a frame to enter: push a todo, or make one: mem.Todo(\"…\").Push(); to add a todo at the top from inside a frame: mem.Frame(\"\").Todo(\"…\")")
	case id == bound():
		panic("you are in `" + id + "` already: it is this session's /goal; push its todos")
	}
	it := m.openItem(id)
	if it.For != "" {
		panic(ref(id, it.Title) + " is asked of the user: end your turn so they can answer, or mem.Drop(\"" + id + "\", \"why\")")
	}
	m.refuseWait(id)
	me := caller()
	switch {
	case m.holder(id, it, me) != "":
		panic(m.heldText(id, it))
	case onStack(stack, id):
		panic("`" + id + "` is already on your stack")
	}
	if s := deepestUnder(stack, id); s != "" {
		panic("`" + id + "` cannot go above its own todo `" + s + "`, which is on your stack: pop or drop that first")
	}
	if above {
		if it.Via != me {
			m.do(Op{Kind: "set", Name: id, Field: "via", Old: it.Via, New: me})
		}
		pushed(id, it.Title)
		println("in " + ref(id, it.Title) + ", the todo above the next one")
		return
	}
	if it.Doing != me {
		m.do(Op{Kind: "set", Name: id, Field: "doing", Old: it.Doing, New: me})
	}
	pushed(id, it.Title)
	println(m.pushText(id, it, thought))
	// The thoughts already on it, as a note left for it by another frame or
	// session (mem.Note), or what it said when it was closed before: they
	// reach the model as it starts the todo, with no search (QA,
	// 2026-11-04: a test spec kept for this todo sat in mem while the next
	// session wrote a different test).
	if kept := thoughtLines(it.Notes, maxShownThoughts); kept != "" {
		println("thoughts on it: " + kept)
	}
	if others := m.doneByOthers(id, me); others != "" {
		print(others)
	}
	if w := m.workers(taskOf(id), me); w != "" && !subAgent() {
		println(w + oneOrMany(w, " also works", " also work") + " on this plan now: keep to this todo's files, and take the next todo with mem.Next().")
	}
}

// oneOrMany is one or many, for a list joinList made.
func oneOrMany(list, one, many string) string {
	if strings.Contains(list, " and ") {
		return many
	}
	return one
}

// taskOf is the top-level todo id is under, or id itself.
func taskOf(id string) string {
	if i := strings.IndexByte(id, '/'); i >= 0 {
		return id[:i]
	}
	return id
}

// Parallel says the todos under the todo may be done at once, in any order,
// by any session; what must follow them is the todo's own work, done before
// it is popped. One session takes them in order either way. It goes on the
// todo they are under, not on each of them. It returns the todo, so it
// chains: mem.Todo("build the shapes", "circle", "square").Parallel().
func (f *Frame) Parallel() *Frame {
	if f == nil || f.m == nil || f.Id == "" {
		panic("no todo to make parallel: mem.Todo(task) makes one")
	}
	f.m.parallel(f.Id)
	return f
}

// parallel marks open todo id's todos as ones that may be done at once.
func (m *Mem) parallel(id string) {
	it := m.openItem(id)
	if it.For != "" {
		panic(ref(id, it.Title) + " is asked of the user: it has no todos")
	}
	if it.Parallel {
		println("`" + id + "`'s todos may already be done at once")
		return
	}
	m.do(Op{Kind: "set", Name: id, Field: "parallel", New: "yes"})
	println("`" + id + "`'s todos may be done at once, by any session")
}

// pushText is what a push of todo id, it, says: where the model is, what
// to do there, and what a pop will do. A todo with nothing under it is
// done now, with tools, and its result comes from what they returned:
// told only "when it is done, pop it", a model with no thinking wrote the
// result before the work (QA, 2026-11-04, A20). The todos of one with
// todos under it are its work, so Next is named: told to do it, models did
// its first todo's work in the task's own frame (A13).
func (m *Mem) pushText(id string, it *Todo, thought string) string {
	head := "in " + ref(id, it.Title)
	if it.Title == "" && thought != "" {
		head = "in `" + id + "` (thought: " + quote(line(thought, 80)) + ")"
	}
	what := ": do it now, with your tools; once what they return shows it done, pop it with that and what the next todo needs. "
	switch nt := m.nextTodo(id); {
	case it.OpenSteps > 0 && nt != nil:
		what = ": its todos are its work, mem.Next() steps into " + ref(nt.Id, nt.Purpose) + "; once they are closed, pop it with what came of them and what the next todo needs. "
	case it.OpenSteps > 0:
		what = ": its todos are its work, and none can be done now (mem.Show(\"" + id + "\") says why); once they are closed, pop it with what came of them and what the next todo needs. "
	case m.workedUnder(id, it):
		what = ": its todos are done; pop it with what came of them and what the next todo needs. "
	}
	return head + what +
		"Everything from here until the pop is removed from your context then, what you were told included; " +
		"what matters after, keep as a thought first. Your result stays, and nothing you did is undone."
}

// nothingNext is what Next() with nothing to step into says: what there is
// to do instead, where the model stands.
func (m *Mem) nothingNext() string {
	p := m.present()
	if p == "" {
		var parts []string
		n := 0
		m.eachTask(func(task string, g *Todo) bool {
			if n++; n <= maxList {
				parts = append(parts, m.notNow(task, g))
			}
			return false
		})
		switch {
		case n == 0:
			return "no next todo: nothing is open: make a todo, mem.Todo(\"…\").Push()."
		case n > maxList:
			parts = append(parts, "and "+strconv.Itoa(n-maxList)+" more, which mem.Show() lists")
		}
		return "no next todo: " + strings.Join(parts, "; ") + "; or make a todo, mem.Todo(\"…\").Push()."
	}
	it := m.get(p)
	if it.OpenSteps > 0 {
		return "no next todo: nothing in `" + p + "` can be done now: " + m.holdsOf(p) + "."
	}
	if len(frames()) == 0 {
		return "no next todo: `" + p + "` is this session's /goal and has no open todo: mem.Todo(\"…\") to plan more, or mem.Done when it holds."
	}
	if it.Status != "" {
		return "no next todo: `" + p + "` is " + it.Status + " already" + arrow(it.Result) + ": mem.Pop(\"…\") takes it off your stack."
	}
	if h := m.holder(p, it, caller()); h != "" {
		return "no next todo: `" + p + "` has no open todo, and " + who(h) + " holds it now and pops it: mem.Leave() steps out of it."
	}
	return m.doneByOthers(p, caller()) + "no next todo: `" + p + "` has no open todo: pop it with its result."
}

// notNow says why Next at the root found nothing to do in open top-level
// todo task, g.
func (m *Mem) notNow(task string, g *Todo) string {
	msg := ref(task, g.Title)
	switch h := m.holder(task, g, caller()); {
	case g.For != "":
		return msg + " is asked of the user"
	case g.OpenSteps > 0:
		return msg + ": nothing in it can be done now: " + m.holdsOf(task)
	case h != "":
		return msg + " has no open todo and is held by " + who(h) + ", which pops it with its result"
	case m.waiting(g):
		return msg + " has no open todo and" + waitText(g.Wait)
	}
	return msg + " has no open todo"
}

// holdsOf says what holds back each open todo of frame id that NextTodo
// passed over: asked of the user, waiting, or held by another session.
func (m *Mem) holdsOf(id string) string {
	var parts []string
	m.eachStep(id, func(step string, s *Todo) bool {
		switch m.state(step, s) {
		case "asked":
			parts = append(parts, "`"+step+"` is asked of the user: end your turn so they can answer, or mem.Drop(\""+step+"\", \"why\")")
		case "waiting":
			parts = append(parts, "`"+step+"`"+waitText(m.waitOn(step)))
		case "doing":
			parts = append(parts, "`"+step+"` is held by "+who(m.doer(step, s)))
		}
		return false
	})
	return joinList(parts)
}

// heldText refuses a call on a frame another session holds; when the frame
// has a todo this session could enter, it says so, since a todo another
// session holds, ended or not, does not hold the free ones under it.
func (m *Mem) heldText(id string, it *Todo) string {
	h := m.holder(id, it, caller())
	if h == "" {
		h = m.doing(id, it, caller())
	}
	msg := "`" + id + "` is held by " + who(h) + ": only it can pop, close or drop it, and the user can resume that session if it has ended. Do not look for it yourself: go on with what is free, or tell the user"
	if top(frames()) == id {
		msg += "; it is on your stack from before it took it: mem.Leave() steps out of it"
	}
	if nt := m.nextTodo(id); nt != nil {
		msg += "; its todo " + ref(nt.Id, nt.Purpose) + " is free: mem.Frame(" + quote(nt.Id) + ").Push() steps into it"
	}
	return msg
}

// heldByAny reports whether item id is held by any session, or an open
// step under it is.
func (m *Mem) heldByAny(id string, it *Todo) bool {
	held := it.Status == "" && m.doer(id, it) != ""
	m.eachBelow(id, false, func(step string, s *Todo) {
		held = held || s.Status == "" && m.doer(step, s) != ""
	})
	return held
}

// refuseHeld refuses to close, drop, forget or start item id while another
// session holds it, or holds an open todo under it.
func (m *Mem) refuseHeld(id string, it *Todo) {
	me := caller()
	if m.holder(id, it, me) != "" {
		panic(m.heldText(id, it))
	}
	m.eachBelow(id, false, func(step string, s *Todo) {
		if s.Status == "" && m.holder(step, s, me) != "" {
			panic(m.heldText(step, s))
		}
	})
}

// refuseInside refuses to close or drop item name while this session is
// in a frame under it, or has it on its stack below the top: the frame
// above must be popped or dropped first.
func (m *Mem) refuseInside(stack []string, name string) {
	if len(stack) == 0 || name == stack[len(stack)-1] {
		return
	}
	in := deepestUnder(stack, name)
	if in == "" && onStack(stack, name) {
		in = stack[len(stack)-1]
	}
	if in != "" {
		panic("you are still in `" + in + "`: pop or drop it before `" + name + "`")
	}
}

// refuseWait refuses to enter or start item id while it, or an item above
// it, waits.
func (m *Mem) refuseWait(id string) {
	for p := id; p != ""; p = parentOf(p) {
		if w := m.get(p); m.waiting(w) {
			until := "until " + w.Wait
			if !isDay(w.Wait) {
				until = "for " + strconv.Quote(w.Wait)
			}
			panic(ref(p, w.Title) + " waits " + until + ": start it then, or clear the wait with mem.Wait(\"" + p + "\", \"\")")
		}
	}
}

// onStack reports whether id is on stack.
func onStack(stack []string, id string) bool {
	for _, s := range stack {
		if s == id {
			return true
		}
	}
	return false
}

// deepestUnder is the last frame of stack under id, a step of it or of a
// step of it; "" for none.
func deepestUnder(stack []string, id string) string {
	found := ""
	for _, s := range stack {
		if strings.HasPrefix(s, id+"/") {
			found = s
		}
	}
	return found
}

// top is the top of stack, or "".
func top(stack []string) string {
	if len(stack) == 0 {
		return ""
	}
	return stack[len(stack)-1]
}

// Pop leaves the present frame done with its result, and pops it: gnosh
// then removes everything since the push from the model's context. result
// is the model's own words, one string, kept whole up to mem's limit; it
// is what stays. A frame entered in the model's round before this one,
// with nothing in between (fresh), is not popped: its todo was never done.
// mem.Pop(name, result), the present frame's name first, is the same pop.
func (m *Mem) Pop(result ...string) {
	stack := frames()
	if len(stack) == 0 {
		panic("nothing to pop: you are at the root, in no todo; from here a todo is closed by its name: mem.Done(\"name\", \"…\")")
	}
	id := top(stack)
	it := m.item(id)
	if it.Status != "" {
		m.popClosed(id, it)
		return
	}
	var r string
	switch len(result) {
	case 0:
		panic("say what came of `" + id + "`, even \"nothing found\": mem.Pop(\"…\"); to step out of it and leave it open: mem.Leave(\"why\")")
	case 1:
		r = result[0]
	case 2:
		// The shape of Done, its name first: models write it (QA,
		// 2026-11-04).
		if n := result[0]; m.resolve(n) != id && n != stepName(id) && sameTitle(n) != sameTitle(it.Title) {
			panic("you are in `" + id + "`, not " + named(result[0]) + ": mem.Pop(\"…\") closes `" + id + "`, and mem.Done(\"name\", \"…\") closes a todo by its name")
		}
		r = result[1]
	default:
		panic("Pop takes what came of the todo you are in: mem.Pop(\"…\")")
	}
	m.refuseTaken(id, it)
	m.refuseFresh(id, it)
	m.pop(id, it, r, false)
}

// taskNamed is the open todo whose task text is: one at the top, or one on
// the stack, or under a frame on it, the top's first; "" for none.
// Done(text) then means that todo, not a result for the present one.
func (m *Mem) taskNamed(text string) string {
	if n := m.titled(text); n != "" {
		return n
	}
	want := sameTitle(text)
	stack := frames()
	if b := bound(); b != "" {
		stack = append([]string{b}, stack...)
	}
	for i := len(stack) - 1; i >= 0; i-- {
		f := stack[i]
		it := m.get(f)
		if it == nil || it.Status != "" {
			continue
		}
		if sameTitle(it.Title) == want {
			return f
		}
		found := ""
		m.eachStep(f, func(step string, s *Todo) bool {
			if s.Status == "" && sameTitle(s.Title) == want {
				found = step
			}
			return found != ""
		})
		if found != "" {
			return found
		}
	}
	return ""
}

// refuseTaken refuses to close frame id, on this session's stack, when
// another session holds it now: one this session entered on the way to a
// todo under it holds nothing, and another session may have taken it, or
// be working in it, while this one was not working.
func (m *Mem) refuseTaken(id string, it *Todo) {
	if m.holder(id, it, caller()) != "" {
		panic(m.heldText(id, it))
	}
}

// refuseFresh refuses to leave frame id done when the session entered it
// in its round before this one with nothing in between (fresh()): its todo
// was never done. Pop and Done of the present frame both ask.
//
// A todo whose own todos are all closed, some done, is not fresh: its work
// was done in them, though it was entered only after (QA, 2026-11-04: a
// parent entered with its jobs all done was refused, and the model re-ran
// the tests only to have something between).
func (m *Mem) refuseFresh(id string, it *Todo) {
	if id == fresh() && !(m.workedUnder(id, it) && it.OpenSteps == 0) {
		panic("not popped: nothing came between your entering " + ref(id, it.Title) + " and this pop, so it is not done. Do it, or check that it is done, then pop it.")
	}
}

// popClosed pops a frame of the stack that mem already holds closed: one
// closed by an undo, or kept on the stack by a rewind that lost its pop.
// Nothing changes in mem; the stack is told.
func (m *Mem) popClosed(id string, it *Todo) {
	popped(id, it.Title, it.Result, len(it.Notes))
	println("`" + id + "` was already " + it.Status + arrow(it.Result) + ": it is off your stack now")
	m.printKept(it)
}

// pop leaves frame id, the top of the stack: done with result, or, with
// dropped, dropped with result as why, its open todos dropped first.
func (m *Mem) pop(id string, it *Todo, result string, dropped bool) {
	r := clip(text("the result", result), maxText)
	if r == "" {
		if dropped {
			panic("say why: mem.Drop(\"" + id + "\", \"why\")")
		}
		panic("say what came of `" + id + "`, even \"nothing found\"")
	}
	if dropped {
		steps := m.drop(id, it, r)
		popped(id, it.Title, "dropped: "+r, len(it.Notes))
		msg := "dropped " + ref(id, it.Title)
		if len(steps) > 0 {
			msg += ", with its open todos " + names(outlineOrder(steps))
		}
		println(msg)
		m.printKept(it)
		return
	}
	if it.OpenSteps > 0 {
		if w := m.workers(id, caller()); w != "" {
			panic(ref(id, it.Title) + " has open todos " + m.refs(m.openSteps(id)) + ", and " + w + " works on some of them now: mem.Next() steps into a free one, or waits for them")
		}
		panic(ref(id, it.Title) + " has open todos " + m.refs(m.openSteps(id)) + ": push and pop them, or drop them, first")
	}
	if !m.workedUnder(id, it) && it.For == "" {
		closing(id, it.Title, r, thoughtTexts(it.Notes))
	}
	if it.Wait != "" {
		m.do(Op{Kind: "set", Name: id, Field: "wait", Old: it.Wait})
	}
	if it.Doing != "" {
		m.do(Op{Kind: "set", Name: id, Field: "doing", Old: it.Doing})
	}
	m.do(Op{Kind: "set", Name: id, Field: "status", New: "done", By: caller()})
	m.do(Op{Kind: "set", Name: id, Field: "result", New: r})
	popped(id, it.Title, r, len(it.Notes))
	println("done " + ref(id, it.Title) + " → " + line(r, maxText))
	m.printKept(it)
	if parentOf(id) == "" {
		// The close that ends a plan cuts the plan's work from the
		// context; with none of it in sight, models took the plan for
		// undone and started it again (QA, 2026-11-04).
		println("That closes `" + id + "`, the whole plan: its work is done and stays done, though it is out of your context now. Tell the user what came of it from the results above.")
	}
}

// printKept hands a popped frame's thoughts back in the receipt, word for
// word: the pop takes everything else since the push out of the context,
// and a thought kept for a later todo was otherwise never read again (QA,
// 2026-11-04: "README must say --shout was added in version 1.4" was kept
// before the pop and left out of the README).
func (m *Mem) printKept(it *Todo) {
	if kept := thoughtLines(it.Notes, maxShownThoughts); kept != "" {
		println("its thoughts, kept: " + kept)
	}
}

// maxShownThoughts is how many of a frame's thoughts a push or pop receipt
// lists, and maxShownThought how much of each.
const (
	maxShownThoughts = 5
	maxShownThought  = 600
)

// workedUnder reports whether a todo under todo id, it, was done: its work
// was done in its todos, whose closes were checked. An answer the user gave
// to a question under it is no work done there (it keeps For "user").
func (m *Mem) workedUnder(id string, it *Todo) bool {
	if it.DoneSteps == 0 {
		return false
	}
	for _, s := range it.Steps {
		if st := m.get(id + "/" + s); st != nil && st.Status == "done" && st.For == "" {
			return true
		}
	}
	return false
}

// thoughtTexts is the thoughts kept on a todo, one a line, as gnosh's
// check of its close reads them (closing).
func thoughtTexts(notes []note) string {
	var lines []string
	for _, n := range notes {
		lines = append(lines, n.Name+": "+n.Text)
	}
	return strings.Join(lines, "\n")
}

// thoughtLines is notes as a receipt lists them, oldest first: "`name`:
// text", and the todo a thought became; at most max, the rest counted; ""
// for none.
func thoughtLines(notes []note, max int) string {
	var parts []string
	for i, n := range notes {
		if i == max {
			parts = append(parts, "and "+strconv.Itoa(len(notes)-max)+" more, which mem.Show lists")
			break
		}
		p := "`" + n.Name + "`: " + line(n.Text, maxShownThought)
		if n.Step != "" {
			p += " (now the todo `" + n.Step + "`)"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "; ")
}

// NextHint is what to do after frame id was popped, for gnosh to end the
// receipt with when no push follows in the same input; "" for nothing.
// Gnosh calls it with the stack as it stands after the pop.
func (m *Mem) NextHint(id string) string {
	parent := parentOf(id)
	if it := m.get(id); parent == "" || it == nil || it.Status == "" {
		// Left open (Leave): its receipt says what to do.
		return ""
	}
	p := m.get(parent)
	if p == nil || p.Status != "" {
		return ""
	}
	present := m.present()
	// With other sessions working on the plan, only Next knows what is
	// left for this one, here or where it steps from; never a name to push,
	// which another may take. A sub-agent works for its parent and is not
	// sent on.
	if !subAgent() {
		w := m.workers(parent, caller())
		switch {
		case w != "":
		case present != "":
			w = m.workers(present, caller())
		default:
			m.eachTask(func(task string, _ *Todo) bool {
				w = m.workers(task, caller())
				return w != ""
			})
		}
		if w != "" {
			// By itself: a model that called it beside views of the todo it
			// guessed was next worked that todo, which another session held,
			// and closed the one Next gave it with that work (bench,
			// 2026-11-11).
			return "next: mem.Next(), by itself, which says which todo is yours while " + w + oneOrMany(w, " works", " work") + " on this plan: it may not be the one after this, so read which before you open a file"
		}
	}
	if nt := m.nextTodo(parent); nt != nil {
		if parent == present || present == "" && m.nextAtRoot() == nt.Id {
			return "next: " + ref(nt.Id, nt.Purpose) + ": mem.Next()"
		}
		return "next: " + ref(nt.Id, nt.Purpose) + ": mem.Frame(" + quote(nt.Id) + ").Push()"
	}
	switch {
	case p.OpenSteps > 0:
		return "next: nothing in `" + parent + "` can be done now: " + m.holdsOf(parent)
	case parent == present && onStack(frames(), parent) && m.holder(parent, p, caller()) != "":
		return "next: `" + parent + "` is held by " + who(m.holder(parent, p, caller())) + " now, which pops it: mem.Leave() steps out of it"
	case parent == present && onStack(frames(), parent):
		return m.doneByOthers(parent, caller()) + "next: pop `" + parent + "` with the evidence that it holds"
	}
	return "next: mem.Done(" + quote(parent) + ", \"…\") once it holds"
}

// Think keeps a thought on the present frame and returns it: one journal
// line, no cut, nothing on the server. Think(text) names it for you, from
// the text (gnosh's small model, or its words), with -2, -3 when mem has
// that name; Think(name, text) names it yourself. Thoughts are what a
// frame keeps besides its result, and the one way anything a frame learned
// or was told outlives its pop: they stay in mem under the frame, popped
// or not. At the unbound root there is no frame to think on.
func (m *Mem) Think(args ...string) Thought {
	p := m.present()
	if p == "" {
		panic("you are at the root, in no frame, so a thought has no frame to go on: mem.Frame(\"todo-name\").Think(\"…\") keeps it on a todo, and mem.Fact(\"name\", \"…\") keeps project knowledge")
	}
	return m.thinkOn(p, args...)
}

// thinkOn keeps a thought on todo p, as Think does on the present frame.
func (m *Mem) thinkOn(p string, args ...string) Thought {
	it := m.item(p)
	var name, thought string
	switch len(args) {
	case 1:
		thought = clip(text("the thought", args[0]), maxText)
		if thought == "" {
			panic("a thought needs words: mem.Think(\"what you found\")")
		}
		name = m.freeThought(m.madeName(thought, "note"))
	case 2:
		name = m.thoughtName(p, it, args[0])
		thought = clip(text("the thought", args[1]), maxText)
		if thought == "" {
			panic("a thought needs words: mem.Think(" + quote(name) + ", \"what you found\")")
		}
	default:
		panic("Think takes a thought, or a name and a thought: mem.Think(\"port-env\", \"the port is read from an env var\")")
	}
	m.do(Op{Kind: "note", Name: p + "#" + name, New: thought})
	println("thought `" + name + "` kept on `" + p + "`")
	return Thought{name: name, Text: thought, frame: p, m: m}
}

// madeName is the name mem makes for text when the agent gives none:
// gnosh's small model's, when it answered one that is a name, else the
// text's words (nameOf, fallback for a text with none).
func (m *Mem) madeName(text, fallback string) string {
	if n := nameMade(text); n != "" && validName(n) && !strings.Contains(n, "/") && len(n) <= maxGoalName {
		return n
	}
	return nameOf(text, fallback)
}

// thoughtName checks a new thought's name on frame id.
func (m *Mem) thoughtName(id string, it *Todo, name string) string {
	switch {
	case name == "":
		panic("a thought needs a name to find it by: mem.Think(\"port-env\", \"the port is read from an env var\")")
	case name == id || strings.Contains(name, "/") && m.get(name) != nil:
		panic("`" + name + "` is a frame; a thought's name is a label of your own on the frame you are in, `" + id + "`: mem.Think(\"attempt-1\", \"…\"); to note something on `" + name + "` itself: mem.Note(\"" + name + "\", \"…\")")
	case strings.ToLower(name) != name && validName(strings.ToLower(name)):
		panic("names are lowercase: `" + strings.ToLower(name) + "`")
	case strings.Contains(name, "/") || !validName(name):
		panic(named(name) + " is not a name: a thought's name is lowercase words joined by \"-\"" + like(nameForm(stepName(name)), "port-env"))
	case len(name) > maxGoalName:
		panic("a thought's name is at most " + strconv.Itoa(maxGoalName) + " bytes: try `" + nameOf(name, "thought") + "`")
	case m.thoughtOn(name) != "":
		panic("a thought named `" + name + "` is on `" + m.thoughtOn(name) + "` already: a thought is never edited; add another, like `" + m.freeThought(name) + "`")
	}
	return name
}

// thoughtOn is the id of the item holding the thought named name, or "".
func (m *Mem) thoughtOn(name string) string {
	found := ""
	m.items.Iterate("", "", func(id string, v any) bool {
		if noteIndex(v.(*Todo), name) >= 0 {
			found = id
		}
		return found != ""
	})
	return found
}

// freeThought is base, or base-2, base-3, …, whichever names no thought in
// mem yet.
func (m *Mem) freeThought(base string) string {
	if m.thoughtOn(base) == "" {
		return base
	}
	for i := 2; ; i++ {
		if n := base + "-" + strconv.Itoa(i); m.thoughtOn(n) == "" {
			return n
		}
	}
}

// Thought is a thought by name: the present frame's, else the first found
// walking down the stack, each frame's own and then its todos', else the
// one of that name anywhere in mem; a name mem makes is unique, so a
// thought is reached by name from any frame. Given words, not a name, or
// a name and words, it keeps a thought, as mem.Think does: models write
// both (QA, 2026-11-04).
func (m *Mem) Thought(args ...string) Thought {
	switch {
	case len(args) == 2:
		return m.Think(args[0], args[1])
	case len(args) != 1:
		panic("Thought takes a thought's name, mem.Thought(\"port\"); mem.Think(\"…\") keeps one")
	case strings.ContainsRune(strings.TrimSpace(args[0]), ' '):
		return m.Think(args[0])
	}
	return m.thought(args[0])
}

// thought is the thought named name, as Thought finds it.
func (m *Mem) thought(name string) Thought {
	for _, f := range m.framesDown() {
		it := m.get(f)
		if it == nil {
			continue
		}
		if n := noteNamed(it, name); n != nil {
			return m.thoughtOf(f, *n)
		}
		for _, s := range it.Steps {
			step := f + "/" + s
			if n := noteNamed(m.get(step), name); n != nil {
				return m.thoughtOf(step, *n)
			}
		}
	}
	if id := m.thoughtOn(name); id != "" {
		return m.thoughtOf(id, *noteNamed(m.get(id), name))
	}
	panic("no thought named `" + name + "` in mem: mem.Recall(\"…\") finds one by meaning")
}

// framesDown is the present frame, then the stack from the top down.
func (m *Mem) framesDown() []string {
	var out []string
	if p := m.present(); p != "" {
		out = append(out, p)
	}
	stack := frames()
	for i := len(stack) - 1; i >= 0; i-- {
		if len(out) == 0 || stack[i] != out[0] {
			out = append(out, stack[i])
		}
	}
	return out
}

// Recall is what in mem is about words, by meaning where gnosh has an
// embedding server and by the words otherwise: frames by their purpose,
// thoughts by their text and facts by their body, a handful at most, each
// a Namable. A promoted thought is one entry, the thought.
func (m *Mem) Recall(words string) []Namable {
	words = text("the words", words)
	if words == "" {
		panic("Recall needs words: mem.Recall(\"shared port\")")
	}
	names, kinds, _ := find(words, maxRecall*2)
	promoted := map[string]bool{}
	for i, k := range kinds {
		if k == "thought" {
			if item, name := splitAddr(names[i]); m.get(item) != nil {
				if n := noteNamed(m.get(item), name); n != nil && n.Step != "" {
					promoted[n.Step] = true
				}
			}
		}
	}
	var out []Namable
	for i, n := range names {
		if len(out) == maxRecall {
			break
		}
		switch kinds[i] {
		case "item":
			if it := m.get(n); it != nil && it.Status != "forgotten" && !promoted[n] {
				out = append(out, m.frame(n))
			}
		case "thought":
			item, name := splitAddr(n)
			if it := m.get(item); it != nil && it.Status != "forgotten" {
				if t := noteNamed(it, name); t != nil {
					out = append(out, m.thoughtOf(item, *t))
				}
			}
		case "fact":
			if f := m.getFact(n); f != nil {
				out = append(out, Fact{name: n, Body: f.Text})
			}
		}
	}
	return out
}

// Push dives into the thought: it makes a todo named as the thought, if it
// has none, under the thought's frame when that is the present frame or on
// the stack, else under the present frame; the todo's purpose is why, if
// given, else empty, since the thought's text is there; then it enters the
// todo as mem.Push does. A thought already dived into enters its todo
// again. A Thought comes from Think, Thought(name) or Recall.
func (t Thought) Push(why ...string) {
	if t.m == nil {
		panic("no thought to push: mem.Think, mem.Thought(name) or mem.Recall(words) gives one")
	}
	t.m.pushThought(t, why...)
}

func (m *Mem) pushThought(t Thought, why ...string) {
	if len(why) > 1 {
		panic("a thought's Push takes one reason for the dive, or none")
	}
	frame := t.frame
	if frame == "" {
		frame = m.present()
	}
	if frame == "" {
		panic("you are at the root, in no frame: a thought lives on a frame; mem.Frame(id).Thoughts lists one's")
	}
	it := m.get(frame)
	if it == nil || it.Status == "forgotten" {
		panic(m.noFrame(frame))
	}
	n := noteNamed(it, t.name)
	if n == nil {
		panic("`" + frame + "` has no thought named `" + t.name + "`")
	}
	if n.Step != "" {
		if s := m.get(n.Step); s != nil && s.Status != "forgotten" {
			m.push(n.Step, n.Text)
			return
		}
	}
	present := m.present()
	parent := frame
	if it.Status != "" || !(frame == present || onStack(frames(), frame)) {
		parent = present
	}
	purpose := ""
	if len(why) == 1 {
		purpose = clip(text("why", why[0]), maxTitle)
	}
	if parent == "" && it.Status != "" {
		// From the root, a thought of a closed todo becomes a top-level todo:
		// that is when a thought turns out to need work, after its todo was
		// done, and the only place left for it. Refused, a model reopened
		// the finished todo just to stand in it (QA, 2026-11-04).
		title := purpose
		if title == "" {
			title = clip(text("the thought", n.Text), maxTitle)
		}
		m.refuseOpenTitle(title)
		id := m.freeName(t.name, func(x string) bool { return m.freeTop(x) })
		m.do(Op{Kind: "add", Name: id, New: title, By: caller()})
		m.do(Op{Kind: "set", Name: frame + "#" + t.name, Field: "frame", Old: n.Step, New: id})
		println("thought `" + t.name + "` is now the todo `" + id + "`, at the top")
		m.push(id, n.Text)
		return
	}
	if parent == "" {
		// From the root, a thought of an open todo becomes a todo under it,
		// entered with it (pushChain).
		parent = frame
	}
	step := parent + "/" + m.freeStep(parent, t.name)
	m.addStep(parent, step, purpose)
	m.do(Op{Kind: "set", Name: frame + "#" + t.name, Field: "frame", Old: n.Step, New: step})
	println("thought `" + t.name + "` is now the todo `" + step + "`")
	if present == "" {
		m.pushChain(step, n.Text)
		return
	}
	m.push(step, n.Text)
}
