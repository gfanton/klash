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

// Next from the root enters the free todo alone, as peers sharing a plan
// take its todos (PARALLEL.md). The todo above, entered once its todos are
// done, closes though nothing came between its push and its pop: its work
// was done in its todos. A second pop at the root says how to close it
// (QA, 2026-11-04).
func TestMemParentOfTodosDoneFromTheRoot(t *testing.T) {
	t.Parallel()
	s := newMemSession(t)
	names := addedNames(s.ok(`p := mem.Todo("validate the create functions"); p.Todo("validate the user name"); p.Todo("validate the order quantity")`).Output)
	require.Len(t, names, 3)
	task, first, second := names[0], names[1], names[2]

	s.ok(`mem.Next()`)
	require.Equal(t, []string{first}, s.stack)
	s.ok("mem.Pop(\"names are checked\")\nmem.Next()")
	require.Equal(t, []string{second}, s.stack)
	res := s.run("mem.Pop(\"quantities are checked\")\nmem.Pop(\"both validations added\")")
	require.ErrorContains(t, res.Err, "nothing to pop: you are at the root. A todo mem.Next() entered from the root is entered without the todo above it: close that one with mem.Done(")
	res = s.ok(`mem.Pop("quantities are checked")`)
	require.Contains(t, res.Output, "next: mem.Done(\""+task+"\", \"…\") once it holds")

	// Entered now, with its todos done, it is no fresh frame, even when
	// the agent says nothing came between.
	s.ok(`mem.Frame("` + task + `").Push()`)
	res = s.ok(`mem.Pop("both validations added, go test passes")`, task)
	require.Contains(t, res.Output, "done `"+task+"`")
	require.Empty(t, s.stack)
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
	s.ok(`mem.Think("readme-1-4", "user: the README must say --shout was added in version 1.4")`)
	res := s.ok(`mem.Pop("--shout upper-cases the greeting")`)
	require.Contains(t, res.Output, "its thoughts, kept: `readme-1-4`: user: the README must say --shout was added in version 1.4\n")
	require.NotContains(t, res.Output, "the whole plan", "a todo under a task does not end the plan")

	// A note left on a later todo reaches the model when it enters it.
	s.ok(`mem.Note("` + names[2] + `", "give an example run with its output")`)
	res = s.ok(`mem.Next()`)
	require.Contains(t, res.Output, "thoughts on it: `")
	require.Contains(t, res.Output, ": give an example run with its output\n")

	s.ok(`mem.Pop("README documents --shout, added in 1.4, with an example")`)
	s.ok(`mem.Frame("` + names[0] + `").Push()`)
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
	require.ErrorContains(t, res.Err, "mem.Note(\"todo-name\", \"…\") keeps it on a todo")

	res = s.run(`mem.Frame("").Push()`)
	require.ErrorContains(t, res.Err, "to add a todo at the top from inside a frame: mem.Frame(\"\").Todo(\"…\")")

	tag := addedNames(s.ok(`p := mem.Todo("release 2.0"); p.Todo("tag v2.0.0"); p.Push()`).Output)[1]
	res = s.run(`mem.Think("` + tag + `", "the freeze ends 2026-11-20")`)
	require.ErrorContains(t, res.Err, "to note something on `"+tag+"` itself: mem.Note(\""+tag+"\", \"…\")")

	res = s.run(`mem.Wait("` + tag + `", "the code freeze ends on 2027-02-20")`)
	require.ErrorContains(t, res.Err, "a wait's day stands alone, written 2027-02-20: .Wait(\"2027-02-20\")")
}
