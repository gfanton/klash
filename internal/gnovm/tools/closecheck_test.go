package gnovmtools

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/gnovm"
	"github.com/stretchr/testify/require"
)

// A close of a todo with nothing under it is handed to the check before
// mem keeps the call (CLOSE-CHECK.md): approved, the call runs again and is
// kept; refused, nothing of it is, and the model reads the refusal. A task
// closed on its todos, a drop and a leave are not checked.
func TestRunToolChecksCloses(t *testing.T) {
	l := newLazy(t)
	var stack []string
	var asked []gnovm.MemClose
	refuse := ""
	run := NewRunTool(l, "/proj",
		MemStack(func(context.Context, string) []string { return stack }),
		OnMemFrames(func(_ context.Context, _, _ string, events []gnovm.MemFrame) string {
			for _, e := range events {
				if e.Kind == "push" {
					stack = append(stack, e.Id)
				} else if n := len(stack); n > 0 && stack[n-1] == e.Id {
					stack = stack[:n-1]
				}
			}
			return ""
		}),
		CheckCloses(func(_ context.Context, _, _ string, closes []gnovm.MemClose) string {
			asked = append(asked, closes...)
			return refuse
		}))
	mem := func(code string) string {
		t.Helper()
		resp := call(t, run, `{"name": "mem", "code": `+quoteJSON(code)+`}`)
		return resp.Content
	}

	names := added(mem(`mem.Todo("stats package", "add Min with a test", "add Max with a test")`))
	require.Len(t, names, 3)
	task, minTodo, maxTodo := names[0], names[1], names[2]
	mem(`mem.Next()`)
	require.Equal(t, []string{task, minTodo}, stack)
	require.Empty(t, asked, "nothing closed yet")

	refuse = "not popped: the check of `" + minTodo + "` found: no edit of stats.go is shown. Do what is missing."
	out := mem("mem.Think(\"min\", \"Min of an empty slice panics\")\nmem.Pop(\"Min added in stats.go\")")
	require.Contains(t, out, "ERROR — nothing from this call was kept: not popped: the check of `"+minTodo+"` found")
	require.NotContains(t, out, "done `", "a refused call's run printed nothing kept")
	require.Len(t, asked, 1)
	require.Equal(t, gnovm.MemClose{Id: minTodo, Title: "add Min with a test", Result: "Min added in stats.go", Thoughts: "min: Min of an empty slice panics"}, asked[0])
	require.Equal(t, []string{task, minTodo}, stack, "still in the todo")
	require.NotContains(t, mem(`mem.Show("`+minTodo+`")`), "thought `min`", "the thought went with the refused call")

	refuse = ""
	asked = nil
	out = mem("mem.Pop(\"Min added in stats.go\")\nmem.Next()")
	require.Contains(t, out, "done `"+minTodo+"`")
	require.Contains(t, out, "in `"+maxTodo+"`")
	require.Len(t, asked, 1, "approved once, then kept on the second run")
	require.Equal(t, []string{task, maxTodo}, stack)

	asked = nil
	out = mem("mem.Pop(\"Max added in stats.go\")\nmem.Pop(\"Min and Max added\")")
	require.Contains(t, out, "done `"+task+"` ")
	require.Len(t, asked, 1, "the task, closed on its todos, is not checked")
	require.Equal(t, maxTodo, asked[0].Id)

	// A todo entered and closed in one call says so; one closed by its
	// name, not on the stack, is checked too; a drop is not.
	tidy := added(mem(`mem.Todo("tidy up", "remove dead code", "fix the typo")`))
	require.Len(t, tidy, 3)
	asked = nil
	refuse = "no"
	mem("mem.Next()\nmem.Pop(\"done\")")
	require.Len(t, asked, 1)
	require.True(t, asked[0].Entered)
	require.False(t, asked[0].Worked)
	asked = nil
	mem("mem.Next()\nmem.Fact(\"lint\", \"golangci-lint run\")\nmem.Pop(\"kept the lint command\")")
	require.Len(t, asked, 1)
	require.True(t, asked[0].Entered)
	require.True(t, asked[0].Worked, "a fact kept in the todo is work")
	asked = nil
	mem(`mem.Done("` + tidy[2] + `", "README.md:3 fixed")`)
	require.Len(t, asked, 1)
	require.Equal(t, tidy[2], asked[0].Id)
	require.False(t, asked[0].Entered)
	asked = nil
	refuse = ""
	out = mem(`mem.Drop("` + tidy[1] + `", "nothing is dead")`)
	require.Contains(t, out, "dropped `"+tidy[1]+"`")
	require.Empty(t, asked, "a drop gives up, honestly")

	// A todo whose todos were all dropped is checked like one with none;
	// the user's answer to a question is not.
	race := added(mem(`mem.Todo("fix the race", "reproduce it")`))
	require.Len(t, race, 2)
	mem(`mem.Drop("` + race[1] + `", "cannot reproduce")`)
	asked = nil
	mem(`mem.Done("` + race[0] + `", "fixed the race in listen.go")`)
	require.Len(t, asked, 1, "nothing done under it: its own work is checked")
	out = mem(`mem.Ask("", "which region do we deploy to?")`)
	_, rest, _ := strings.Cut(out, "asked `")
	q := []string{rest[:strings.IndexByte(rest, '`')]}
	asked = nil
	out = mem(`mem.Done("` + q[0] + `", "eu-west-1, the user says")`)
	require.Contains(t, out, "answered `"+q[0]+"`")
	require.Empty(t, asked, "an answer the user gave is not work to check")
}

// added is the names a receipt says were added, in order.
func added(out string) []string {
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "added `"); ok {
			names = append(names, rest[:strings.IndexByte(rest, '`')])
		}
	}
	return names
}

func quoteJSON(s string) string {
	b := []byte{'"'}
	for _, r := range s {
		switch r {
		case '"', '\\':
			b = append(b, '\\', byte(r))
		case '\n':
			b = append(b, '\\', 'n')
		default:
			b = append(b, string(r)...)
		}
	}
	return string(append(b, '"'))
}
