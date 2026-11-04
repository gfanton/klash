// Copyright (C) 2026 Jae Kwon
//
// This file is part of gnosh's GnoVM machine subsystem. It is licensed under
// the GNO Network General Public License, version 6, as published by
// NewTendermint, LLC or its successor, or any later version of that License
// that the Licensor accepts as the proxy under its section 14, by a statement
// published in the canonical copy of internal/gnovm/LICENSE.md; together with
// the Additional Terms (Strong Attribution) in that file.

package gnovm

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// memSession runs mem inputs for one session in the test, keeping its stack
// as gnosh would from the pushes and pops each input reports.
type memSession struct {
	t     *testing.T
	h     *Host
	id    string
	stack []string
}

func (s *memSession) run(code string, fresh ...string) *RunResult {
	s.t.Helper()
	req := RunRequest{Project: "/proj", Session: s.id, Name: MemVM, Code: code, Frames: s.stack}
	if len(fresh) > 0 {
		req.Fresh = fresh[0]
	}
	res, err := s.h.Run(context.Background(), req)
	require.NoError(s.t, err, code)
	if res.Err == nil {
		for _, f := range res.Frames {
			switch f.Kind {
			case "push":
				s.stack = append(s.stack, f.Id)
			case "pop":
				for i := len(s.stack) - 1; i >= 0; i-- {
					if s.stack[i] == f.Id {
						s.stack = append(s.stack[:i], s.stack[i+1:]...)
						break
					}
				}
			}
		}
	}
	return res
}

func (s *memSession) ok(code string, fresh ...string) *RunResult {
	s.t.Helper()
	res := s.run(code, fresh...)
	require.NoError(s.t, res.Err, res.Output)
	return res
}

func newMemSession(t *testing.T) *memSession {
	return &memSession{t: t, h: newTestHost(t, t.TempDir()), id: "s1"}
}

// other is another session on s's mem, a new one, with its own stack.
func (s *memSession) other(id string) *memSession {
	return &memSession{t: s.t, h: s.h, id: id}
}

// addedNames is the names a receipt says were added, in order.
func addedNames(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "added `"); ok {
			names = append(names, rest[:strings.IndexByte(rest, '`')])
		}
	}
	return names
}

// Next from the root enters the todo with the todos above it, so that the
// stack matches the plan: popping the last todo and then "the parent", in
// one call, closes both (QA, 2026-11-04: the parent was never on the
// stack, and the refused second pop undid the first).
func TestMemNextFromTheRootEntersTheTodoAbove(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`p := mem.Todo("validate the create functions"); p.Todo("validate the user name"); p.Todo("validate the order quantity")`).Output)
	require.Len(t, names, 3)
	task, first, second := names[0], names[1], names[2]

	res := s.ok(`mem.Next()`)
	require.Equal(t, []string{task, first}, s.stack)
	require.Contains(t, res.Output, "in `"+task+"` \"validate the create functions\", the todo above the next one\n")
	s.ok("mem.Pop(\"names are checked\")\nmem.Next()")
	require.Equal(t, []string{task, second}, s.stack)
	res = s.ok("mem.Pop(\"quantities are checked\")\nmem.Pop(\"both validations added, go test passes\")")
	require.Contains(t, res.Output, "done `"+task+"`")
	require.Empty(t, s.stack)

	// A pop at the root says how to close a todo from there.
	res = s.run(`mem.Pop("done")`)
	require.ErrorContains(t, res.Err, "nothing to pop: you are at the root, in no todo; from here a todo is closed by its name: mem.Done(")

	// A todo entered once its todos are done is no fresh frame, even when
	// the agent says nothing came between.
	names = addedNames(s.ok(`p := mem.Todo("tidy the docs"); p.Todo("fix the typos")`).Output)
	s.ok(`mem.Frame("` + names[1] + `").Push()`)
	s.ok(`mem.Pop("typos fixed")`)
	s.ok(`mem.Frame("` + names[0] + `").Push()`)
	res = s.ok(`mem.Pop("the docs are tidy")`, names[0])
	require.Contains(t, res.Output, "done `"+names[0]+"`")
}

// The todo above, entered on the way, holds nothing once its session
// stops: a new session takes up the plan and closes it, and the first,
// back, finds it closed (QA, 2026-11-04: plans carried into a new session
// ended with their task held for good).
func TestMemTodoAboveIsFreeOnceItsSessionStops(t *testing.T) {
	t.Parallel()
	s1 := newMemSession(t)
	names := addedNames(s1.ok(`mem.Todo("ship the docs", "write the guide", "check the links")`).Output)
	require.Len(t, names, 3)
	task, guide, links := names[0], names[1], names[2]
	s1.ok(`mem.Next()`)
	require.Equal(t, []string{task, guide}, s1.stack)
	s1.ok(`mem.Pop("guide written")`)
	require.Equal(t, []string{task}, s1.stack, "it stops here, in the task")

	s2 := s1.other("s2")
	s2.ok(`mem.Next()`)
	require.Equal(t, []string{task, links}, s2.stack)
	res := s2.ok("mem.Pop(\"links checked\")\nmem.Pop(\"the docs are out\")")
	require.Contains(t, res.Output, "done `"+task+"`")

	res = s1.ok(`mem.Pop("shipped")`)
	require.Contains(t, res.Output, "`"+task+"` was already done → the docs are out")
	require.Empty(t, s1.stack)
}

// Next from the root takes the top-level todos in the order they were
// made, whether made in one call or another, passing over one that waits
// or is asked of the user (QA, 2026-11-04: a plan's steps made as
// top-level todos ran newest first, Median before Max).
func TestMemNextTakesTodosInTheOrderMade(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	s.ok(`mem.Todo("renew the TLS cert").Wait("2027-12-01")`)
	s.ok(`mem.Ask("", "which region do we deploy to?")`)
	names := addedNames(s.ok(`mem.Todo("add Min to stats.go"); mem.Todo("add Max to stats.go")`).Output)
	names = append(names, addedNames(s.ok(`mem.Todo("add Median to stats.go")`).Output)...)
	require.Len(t, names, 3)
	for _, n := range names {
		res := s.ok(`mem.Next()`)
		require.Equal(t, []string{n}, s.stack, res.Output)
		s.ok(`mem.Pop("added, with a test")`)
	}
	res := s.ok(`mem.Next()`)
	require.Contains(t, res.Output, "no next todo: `renew-tls-cert` \"renew the TLS cert\" has no open todo and waits until 2027-12-01; `")
	require.Contains(t, res.Output, "is asked of the user; or make a todo")
}

// Leave steps out of a todo and leaves it open: held by nobody, with why
// kept on it, while another todo of the plan is done (QA, 2026-11-04:
// Drop, the only way out but Pop, gave up a todo the user wanted kept).
func TestMemLeaveKeepsTheTodoOpen(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`mem.Todo("ship 1.5", "write the changelog", "bump the version")`).Output)
	require.Len(t, names, 3)
	task, changelog, bump := names[0], names[1], names[2]
	s.ok(`mem.Next()`)
	require.Equal(t, []string{task, changelog}, s.stack)
	res := s.ok(`mem.Leave("the user wants the changelog later")`)
	require.Contains(t, res.Output, "left `"+changelog+"` \"write the changelog\" open: it stays a todo, held by nobody")
	require.Contains(t, res.Output, "to take another todo of `"+task+"`: mem.Frame(\"name\").Push()")
	require.NotContains(t, res.Output, "next:", "no hint sends it back into what it left")
	require.Equal(t, []string{task}, s.stack)
	s.ok(`mem.Frame("` + bump + `").Push()`)
	s.ok(`mem.Pop("Version is 1.5.0")`)
	res = s.ok(`println(mem.Show("` + changelog + `"))`)
	require.Contains(t, res.Output, "[ ] `"+changelog+"`")
	require.Contains(t, res.Output, "left open: the user wants the changelog later")
	require.NotContains(t, res.Output, "held by")
	res = s.run(`mem.Leave()`)
	require.NoError(t, res.Err, "it leaves the task too")
	require.Empty(t, s.stack)
	res = s.run(`mem.Leave()`)
	require.ErrorContains(t, res.Err, "nothing to leave: you are at the root")
}

// A plan is one call; Later adds at the top from inside a todo and leaves
// the stack alone.
func TestMemPlanInOneCallAndLater(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`mem.Todo("make the port configurable", "add a --port flag", "document the flag", "test its default").Push()`).Output)
	require.Len(t, names, 4)
	for _, n := range names[1:] {
		require.Equal(t, names[0], n[:strings.LastIndexByte(n, '/')])
	}
	require.Equal(t, []string{names[0]}, s.stack)
	s.ok(`mem.Next()`)
	require.Equal(t, names[:2], s.stack)
	later := addedNames(s.ok(`mem.Later("fix the flaky listener test")`).Output)
	require.Len(t, later, 1)
	require.NotContains(t, later[0], "/")
	require.Equal(t, names[:2], s.stack)
}

// Close, drop, reopen and think on the todo itself; Push returns the todo,
// so a question chains off it.
func TestMemVerbsOnTheTodo(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`mem.Todo("release 2.0", "bump the version", "tag v2.0.0", "announce it")`).Output)
	require.Len(t, names, 4)
	res := s.ok(`mem.Frame("` + names[1] + `").Done("Version is 2.0.0")`)
	require.Contains(t, res.Output, "done `"+names[1]+"`")
	res = s.ok(`mem.Frame("` + names[3] + `").Drop("no announcement for this one")`)
	require.Contains(t, res.Output, "dropped `"+names[3]+"`")
	res = s.ok(`mem.Frame("` + names[3] + `").Reopen("the user wants one after all")`)
	require.Contains(t, res.Output, "reopened `"+names[3]+"`")
	res = s.ok(`mem.Frame("` + names[2] + `").Think("sign-key", "sign the tag with the 2026 key")`)
	require.Contains(t, res.Output, "thought `sign-key` kept on `"+names[2]+"`")
	res = s.ok(`mem.Frame("` + names[2] + `").Push().Ask("which key signs the tag?")`)
	require.Contains(t, res.Output, "thoughts on it: `sign-key`: sign the tag with the 2026 key")
	require.Contains(t, res.Output, "asked `"+names[2]+"/")
}

// mem.Fact(name) reads a fact whole, shown once whether printed or not;
// a view that cuts a fact says how to read it.
func TestMemFactReadsAFactByName(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	long := strings.Repeat("the token is session.expiry.mac and ", 6) + "expiry is checked in Verify (auth/tokens.go:27)"
	s.ok(`mem.Fact("auth/how-it-works", "` + long + `")`)
	res := s.ok(`mem.Fact("auth/how-it-works")`)
	require.Equal(t, 1, strings.Count(res.Output, "expiry is checked in Verify"), res.Output)
	res = s.ok(`println(mem.Fact("auth/how-it-works"))`)
	require.Equal(t, 1, strings.Count(res.Output, "expiry is checked in Verify"), res.Output)
	res = s.ok(`println(mem.Show())`)
	require.Contains(t, res.Output, "… (mem.Fact(\"auth/how-it-works\") reads it whole)")
	res = s.run(`mem.Fact("auth/expiry")`)
	require.ErrorContains(t, res.Err, "no fact `auth/expiry`; `auth/` has `auth/how-it-works`")
	res = s.ok(`mem.Fact("the staging database is db-stg-7")`)
	require.Contains(t, res.Output, "is new")
}

// mem.Help() lists the calls to know; mem.Help(name) says what one does.
func TestMemHelp(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	res := s.ok(`mem.Help()`)
	require.Contains(t, res.Output, "mem.Leave(\"why\")  step out of the todo you are in and leave it open")
	require.Contains(t, res.Output, "mem.Help(\"Todo\") says what one call does and takes")
	res = s.ok(`mem.Help("Todo")`)
	require.Contains(t, res.Output, "mem.Todo(task, todos...): Todo adds a todo and returns it")
	require.Contains(t, res.Output, "a Frame's Todo(task, todos...)")
	res = s.ok(`mem.Help("Frame.Push")`)
	require.Contains(t, res.Output, "a Frame's Push(), as in mem.Todo(…).Push(): Push steps into the frame")
	res = s.ok(`mem.Help("Fly")`)
	require.Contains(t, res.Output, "mem has no call \"Fly\"")
}

// The call shapes models write: Done with a result alone closes the todo
// you are in, Pop takes a name before its result, Thought keeps a thought
// given words (QA, 2026-11-04).
func TestMemTakesTheShapesModelsWrite(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`mem.Todo("tidy up", "fix the typos", "fix the links")`).Output)
	require.Len(t, names, 3)
	s.ok(`mem.Next()`)
	res := s.run(`mem.Done("All typos fixed in README.md")`)
	require.ErrorContains(t, res.Err, "mem.Done takes a todo's name, then what came of it; to close `"+names[1]+"`, the todo you are in, with those words: mem.Pop(\"All typos fixed in README.md\")")
	res = s.ok(`mem.Pop("All typos fixed in README.md")`)
	require.Contains(t, res.Output, "done `"+names[1]+"`")
	s.ok(`mem.Next()`)
	res = s.ok(`mem.Pop("` + names[2] + `", "two links fixed")`)
	require.Contains(t, res.Output, "done `"+names[2]+"`")
	res = s.run(`mem.Pop("` + names[1] + `", "again")`)
	require.ErrorContains(t, res.Err, "you are in `"+names[0]+"`, not `"+names[1]+"`")
	res = s.ok(`mem.Thought("wiki", "two links point at the old wiki")`)
	require.Contains(t, res.Output, "thought `wiki` kept on `"+names[0]+"`")
	res = s.ok(`mem.Thought("the wiki moved to docs.example.test")`)
	require.Contains(t, res.Output, "kept on `"+names[0]+"`")
	res = s.ok(`println(mem.Thought("wiki"))`)
	require.Contains(t, res.Output, "two links point at the old wiki")
	res = s.run(`mem.Pop()`)
	require.ErrorContains(t, res.Err, "to step out of it and leave it open: mem.Leave(\"why\")")

	// A todo's task where its name goes is named in the refusal; Pop takes
	// the present todo's last part, or its task, before the result.
	s2 := newMemSession(t)
	more := addedNames(s2.ok(`mem.Todo("tidy more", "fix the spelling", "fix the dates")`).Output)
	require.Len(t, more, 3)
	s2.ok(`mem.Next()`)
	require.Equal(t, more[:2], s2.stack)
	res = s2.run(`mem.Done("fix the dates")`)
	require.ErrorContains(t, res.Err, "\"fix the dates\" is the task of `"+more[2]+"`, so mem.Done(\""+more[2]+"\", \"what came of it\")")
	res = s2.ok(`mem.Done("` + more[2] + `", "dates are ISO 8601 now")`)
	require.Contains(t, res.Output, "done `"+more[2]+"`")
	require.Equal(t, more[:2], s2.stack, "the present todo stays open")
	res = s2.ok(`mem.Pop("` + more[1][strings.LastIndexByte(more[1], '/')+1:] + `", "spelling fixed")`)
	require.Contains(t, res.Output, "done `"+more[1]+"`")
	res = s2.ok(`mem.Pop("tidy more", "both fixed")`)
	require.Contains(t, res.Output, "done `"+more[0]+"`")
}

// A fresh frame with no todos of its own is still refused.
func TestMemFreshStillRefusesAnUntouchedTodo(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	name := addedNames(s.ok(`mem.Todo("update the README").Push()`).Output)[0]
	res := s.run(`mem.Pop("README.md now documents Get")`, name)
	require.ErrorContains(t, res.Err, "not popped: nothing came between")
}

// The close that ends a plan says so, and the receipt of any pop hands
// back the frame's thoughts word for word; entering a todo shows the
// thoughts already on it.
func TestMemPopHandsBackThoughtsAndEndsThePlan(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`p := mem.Todo("add a shout flag"); p.Todo("add the flag to main.go"); p.Todo("document it in the README")`).Output)
	require.Len(t, names, 3)
	s.ok(`mem.Next()`)
	require.Equal(t, names[:2], s.stack, "it entered the task on the way")
	s.ok(`mem.Think("readme-1-4", "user: the README must say --shout was added in version 1.4")`)
	res := s.ok(`mem.Pop("--shout upper-cases the greeting")`)
	require.Contains(t, res.Output, "its thoughts, kept: `readme-1-4`: user: the README must say --shout was added in version 1.4\n")
	require.NotContains(t, res.Output, "the whole plan", "a todo under a task does not end the plan")

	// A note left on a later todo reaches the model when it enters it.
	s.ok(`mem.Frame("` + names[2] + `").Think("give an example run with its output")`)
	res = s.ok(`mem.Next()`)
	require.Contains(t, res.Output, "thoughts on it: `")
	require.Contains(t, res.Output, ": give an example run with its output\n")

	s.ok(`mem.Pop("README documents --shout, added in 1.4, with an example")`)
	require.Equal(t, names[:1], s.stack)
	res = s.ok(`mem.Pop("the flag works and is documented")`)
	require.Contains(t, res.Output, "That closes `"+names[0]+"`, the whole plan: its work is done and stays done")
}

// Entering a done todo is refused with what came of it first, not with an
// invitation to reopen it.
func TestMemClosedTodoLeadsWithItsResult(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	name := addedNames(s.ok(`mem.Todo("add the json flag").Push()`).Output)[0]
	s.ok(`mem.Pop("--json prints {\"words\": N}")`, "")
	res := s.run(`mem.Frame("` + name + `").Push()`)
	require.ErrorContains(t, res.Err, "is done → --json prints {\"words\": N}. That is what came of it, and the work stays done though it is out of your context: answer from it. Reopen it only if the user wants more done")
}

// From the root, a thought whose todo is closed is promoted to a todo of
// its own at the top, and entered.
func TestMemThoughtPromotedFromTheTop(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	s.ok(`mem.Todo("add a json flag").Push()`)
	s.ok(`mem.Think("error-handling", "os.Open's error is ignored in main.go")`)
	s.ok(`mem.Pop("--json added")`, "")
	require.Empty(t, s.stack)

	res := s.ok(`mem.Thought("error-handling").Push()`)
	require.Contains(t, res.Output, "thought `error-handling` is now the todo `error-handling`, at the top\n")
	require.Equal(t, []string{"error-handling"}, s.stack)
	res = s.ok(`println(mem.Show("error-handling"))`)
	require.Contains(t, res.Output, "\"os.Open's error is ignored in main.go\"")
	res = s.ok(`println(mem.Thought("error-handling"))`)
	require.Contains(t, res.Output, "(now the todo `error-handling`)")
}

// The named Show prints a todo's whole title, past the 80 bytes the
// outlines show.
func TestMemShowNamedPrintsTheWholeTitle(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	title := "later: fix remaining pager.go issues - Offset(p,per) should be (p-1)*per for 1-based pages, and guard per==0 divide-by-zero in Pages"
	name := addedNames(s.ok(`mem.Todo("` + title + `")`).Output)[0]
	res := s.ok(`println(mem.Show("` + name + `"))`)
	require.Contains(t, res.Output, "guard per==0 divide-by-zero in Pages\"")
	res = s.ok(`println(mem.Show())`)
	require.NotContains(t, res.Output, "divide-by-zero", "the outline still cuts long titles")
}

// Standing in a todo, its todos are named by their last part.
func TestMemLastPartNamesATodoFromItsParent(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	s.ok(`mem.Todo("add slug table tests").Push()`)
	full := addedNames(s.ok(`mem.Todo("make slug lower-case")`).Output)[0]
	last := full[strings.LastIndexByte(full, '/')+1:]
	res := s.ok(`mem.Drop("` + last + `", "the user wants it later")`)
	require.Contains(t, res.Output, "dropped `"+full+"`")
	res = s.ok(`mem.Reopen("` + last + `")`)
	require.Contains(t, res.Output, "reopened `"+full+"`")
}

// Refusals name the call that does what the model was after.
func TestMemRefusalsNameTheCall(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	res := s.run(`mem.Think("the code freeze ends Nov 20")`)
	require.ErrorContains(t, res.Err, "mem.Frame(\"todo-name\").Think(\"…\") keeps it on a todo")

	res = s.run(`mem.Frame("").Push()`)
	require.ErrorContains(t, res.Err, "to add a todo at the top from inside a frame: mem.Frame(\"\").Todo(\"…\")")

	tag := addedNames(s.ok(`p := mem.Todo("release 2.0"); p.Todo("tag v2.0.0"); p.Push()`).Output)[1]
	res = s.run(`mem.Think("` + tag + `", "the freeze ends 2026-11-20")`)
	require.ErrorContains(t, res.Err, "to note something on `"+tag+"` itself: mem.Note(\""+tag+"\", \"…\")")

	res = s.run(`mem.Wait("` + tag + `", "the code freeze ends on 2027-02-20")`)
	require.ErrorContains(t, res.Err, "a wait's day stands alone, written 2027-02-20: .Wait(\"2027-02-20\")")
}
