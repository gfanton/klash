package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/gnovm"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// The claims a result makes of files changed: the false closes seen in QA
// (2026-11-04) make them, and the true results models write do not.
func TestChangeClaims(t *testing.T) {
	t.Parallel()
	files := func(result string) []string {
		var out []string
		for _, c := range changeClaims(result) {
			out = append(out, c.file)
		}
		return out
	}
	// The false closes.
	require.Equal(t, []string{"stats.go"}, files("Median(xs []int) float64 added in stats.go:25-36 (sorts a copy; mean of the middle two for an even count); TestMedian in stats_test.go"))
	require.Equal(t, []string{"README.md"}, files("README.md:3 now says Get"))
	require.Equal(t, []string{"README.md"}, files("README.md:3 now reads 'Fetch a URL…' → 'Get a URL…'"))
	require.Equal(t, []string{"README.md"}, files("README.md:3 now documents Get(base, path)"))
	require.Equal(t, []string{"main.go"}, files("main.go:9 now calls client.Get; build passes"))
	require.Equal(t, []string{"main.go"}, files("Updated main.go:10 to call Get instead of Fetch"))
	require.Equal(t, []string{"client.go", "main.go", "README.md"}, files("Renamed Fetch to Get in client.go, main.go and README.md"))
	require.Equal(t, []string{"stats.go"}, files("stats.go updated with Median"))
	// True results that claim no change made in the todo.
	for _, r := range []string{
		"README.md already documents --port (README.md:12)",
		"store.go:23 still reads `_ = json.NewDecoder(f).Decode(&items)`",
		"go.mod: no change needed",
		"verified README.md:3 says Get",
		"README.md must be updated next",
		"TestLoad checks Load now returns the error (store.go)",
		"grep -rn Fetch main.go README.md → no matches",
		"go test ./... → ok stats 0.002s",
		"rewritten per README.md",
	} {
		require.Empty(t, files(r), r)
	}
	made := changeClaims("Created stats_test.go with TestMedian")
	require.Equal(t, []fileClaim{{"stats_test.go", true}}, made)
}

// A claim is backed by an edit or a write of the file in the todo, by a
// change to it since the todo began (a command's), or by a view of it;
// not by a look at anything else, nor by a failed edit.
func TestUnbackedClaim(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []string{"stats.go", "README.md", filepath.Join("internal", "pager", "pager.go")} {
		p := filepath.Join(dir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("package x\n"), 0o644))
	}
	// The todo began after the files were last changed.
	since := time.Now().Add(time.Millisecond)
	median := "Median(xs []int) float64 added in stats.go:25-36; TestMedian in stats_test.go"
	call := func(id, name, input string) message.Message {
		return row(message.Assistant, "a-"+id, message.ToolCall{ID: id, Name: name, Input: input, Finished: true})
	}
	result := func(id, name, content string, failed bool) message.Message {
		return row(message.Tool, "t-"+id, message.ToolResult{ToolCallID: id, Name: name, Content: content, IsError: failed})
	}
	looked := []message.Message{call("l1", tools.LSToolName, `{}`), result("l1", tools.LSToolName, "stats.go", false)}
	f, made := unbackedClaim(median, looked, since, nil, dir)
	require.Equal(t, "stats.go", f, "an ls changes nothing (postA)")
	require.False(t, made)

	edited := []message.Message{call("e1", tools.EditToolName, `{"file_path":"`+filepath.Join(dir, "stats.go")+`"}`), result("e1", tools.EditToolName, "edited", false)}
	f, _ = unbackedClaim(median, edited, since, nil, dir)
	require.Empty(t, f, "an edit of it backs it")

	failedEdit := []message.Message{call("e2", tools.EditToolName, `{"file_path":"`+filepath.Join(dir, "stats.go")+`"}`), result("e2", tools.EditToolName, "old_string not found", true)}
	f, _ = unbackedClaim(median, failedEdit, since, nil, dir)
	require.Equal(t, "stats.go", f, "a failed edit changed nothing")

	otherFile := []message.Message{call("e3", tools.EditToolName, `{"file_path":"`+filepath.Join(dir, "main.go")+`"}`), result("e3", tools.EditToolName, "edited", false)}
	f, _ = unbackedClaim("README.md:3 now reads 'Get a URL'", otherFile, since, nil, dir)
	require.Equal(t, "README.md", f, "main.go's edit does not back README.md (closeAB1)")

	viewed := []message.Message{call("v1", tools.ViewToolName, `{"file_path":"`+filepath.Join(dir, "README.md")+`"}`), result("v1", tools.ViewToolName, "# x", false)}
	f, _ = unbackedClaim("README.md:3 now says Get", viewed, since, nil, dir)
	require.Empty(t, f, "a view shows it: the review judges what it showed")

	bash := []message.Message{call("b1", tools.BashToolName, `{"command":"sed -i s/Fetch/Get/ stats.go"}`), result("b1", tools.BashToolName, "", false)}
	f, _ = unbackedClaim(median, bash, since, nil, dir)
	require.Equal(t, "stats.go", f, "a command that left the file as it was backs nothing")
	later := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "stats.go"), later, later))
	f, _ = unbackedClaim(median, bash, since, nil, dir)
	require.Empty(t, f, "the file changed since the todo began")

	f, _ = unbackedClaim("Fixed the off-by-one in pager.go", looked, since, nil, dir)
	require.Equal(t, "pager.go", f, "found under the project by its name")

	f, made = unbackedClaim("Created stats_test.go with TestMedian", looked, since, nil, dir)
	require.Equal(t, "stats_test.go", f)
	require.True(t, made, "said made, and there is no such file")

	f, _ = unbackedClaim("README.md already documents --port", looked, since, nil, dir)
	require.Empty(t, f, "no claim")
	f, _ = unbackedClaim("Renamed Fetch to client.Get", looked, since, nil, dir)
	require.Empty(t, f, "client.Get names no file")
}

// Each refusal of a close names its todo where the guard, the loop's nudge
// and the turn's end read it.
func TestRefusedClose(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"ERROR — nothing from this call was kept: not popped: nothing came between your entering `stats/median` \"Median\" and this pop, so it is not done.",
		enteredHereText("stats/median"),
		claimText("popped", "stats/median", "stats.go", false),
		claimText("closed", "stats/median", "stats_test.go", true),
		reviewText("popped", "stats/median", "no edit of stats.go adds Median."),
	} {
		require.Equal(t, "stats/median", refusedClose(text), text)
		require.True(t, strings.HasPrefix(refusalLine(text), "not "), text)
	}
	require.Empty(t, refusedClose("done `stats/median` \"Median\" → added"))
	require.Contains(t, reviewText("popped", "x", "nothing shows the test run."), "found: nothing shows the test run. Do what is missing")
}

// A refused pop of a todo whose work was done before it was entered points
// at closing it by its name from where the work was done, which is checked
// against that frame; a refused close by name, already there, does not
// (bench, 2026-11-06).
func TestRefusalPointsAtDoneByName(t *testing.T) {
	t.Parallel()
	const id = "plan/fix-tax"
	require.Contains(t, enteredHereText(id), `mem.Done("plan/fix-tax", "what came of it")`)
	popped := reviewText("popped", id, "no edit of tax.go is shown.")
	require.Contains(t, popped, `mem.Leave("done before")`)
	require.Contains(t, popped, `mem.Done("plan/fix-tax", "what came of it")`)
	require.Contains(t, claimText("popped", id, "tax.go", false), `mem.Done("plan/fix-tax", "what came of it")`)
	require.NotContains(t, reviewText("closed", id, "no edit of tax.go is shown."), "done before")
	require.NotContains(t, claimText("closed", id, "tax.go", false), "done before")
	for _, text := range []string{enteredHereText(id), popped, claimText("popped", id, "tax.go", false)} {
		require.Equal(t, id, refusedClose(text), text)
	}
}

// A call that failed is no work: two failed renames let a todo be closed
// "done" (QA, 2026-11-04).
func TestWorkInFailedCall(t *testing.T) {
	t.Parallel()
	failed := []message.Message{
		row(message.Assistant, "a1", message.ToolCall{ID: "r1", Name: tools.RenameToolName, Input: `{"file_path":"x.go"}`, Finished: true}),
		row(message.Tool, "t1", message.ToolResult{ToolCallID: "r1", Name: tools.RenameToolName, Content: "no identifier found", IsError: true}),
	}
	require.False(t, workIn(failed, "x"))
	ran := []message.Message{
		row(message.Assistant, "a1", message.ToolCall{ID: "b1", Name: tools.BashToolName, Input: `{"command":"go test ./..."}`, Finished: true}),
		row(message.Tool, "t1", message.ToolResult{ToolCallID: "b1", Name: tools.BashToolName, Content: "FAIL\nExit code 1"}),
	}
	require.True(t, workIn(ran, "x"), "a command that ran and failed is work")
}

// The review is told the todo, its thoughts, the files changed and the
// commands run, and the claim, before the transcript; a long transcript is
// cut to fit, a failed call marked so.
func TestClosePrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []message.Message{
		row(message.Assistant, "a1", message.ToolCall{ID: "e1", Name: tools.EditToolName, Input: `{"file_path":"` + filepath.Join(dir, "stats.go") + `","old_string":"x","new_string":"func Median"}`, Finished: true}),
		row(message.Tool, "t1", message.ToolResult{ToolCallID: "e1", Name: tools.EditToolName, Content: "edited"}),
		row(message.Assistant, "a2", message.ToolCall{ID: "b1", Name: tools.BashToolName, Input: `{"command":"go test ./..."}`, Finished: true}),
		row(message.Tool, "t2", message.ToolResult{ToolCallID: "b1", Name: tools.BashToolName, Content: "ok stats", Metadata: `{"exit_code":0}`}),
		row(message.Assistant, "a3", message.ToolCall{ID: "v1", Name: tools.ViewToolName, Input: `{"file_path":"nope.go"}`, Finished: true}),
		row(message.Tool, "t3", message.ToolResult{ToolCallID: "v1", Name: tools.ViewToolName, Content: "no such file", IsError: true}),
	}
	cl := gnovm.MemClose{Id: "stats/median", Title: "Median with a test", Result: "Median added in stats.go", Thoughts: "user: keep it O(n log n)"}
	p := closePrompt(cl, closeWindow{rows: rows, prompt: "now do Median"}, dir)
	for _, want := range []string{
		"The todo: Median with a test",
		"Thoughts kept on it: user: keep it O(n log n)",
		"What the user asked in this turn: now do Median",
		"Files the agent's edits and writes changed in the todo: stats.go",
		"Commands it ran in the todo: `go test ./...` → exit 0",
		"The result it is being closed with (the agent's claim): Median added in stats.go",
		"view failed: no such file",
		"Does what happened in the todo show the result?",
	} {
		require.Contains(t, p, want)
	}
	require.Less(t, strings.Index(p, "Files the agent's"), strings.Index(p, "Agent called edit"), "the lists come before the transcript")

	var long []message.Message
	for i := range 200 {
		long = append(long, row(message.Tool, "t", message.ToolResult{ToolCallID: "x", Name: tools.ViewToolName, Content: strings.Repeat("y", 3000) + string(rune('a'+i%26))}))
	}
	require.LessOrEqual(t, len([]rune(closeTranscript(long))), closeTranscriptLimit+1)
}

// The window of a todo entered in the call that popped the one before
// starts after that call, so the todo before's work is not this one's: a
// view in s1 does not show what s2 claims (code review, 2026-11-05). A call
// made before the close in its own round is in it.
func TestCloseWindowStartsAfterThePopBefore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s1File, s2File := filepath.Join(dir, "min.go"), filepath.Join(dir, "README.md")
	for _, f := range []string{s1File, s2File} {
		require.NoError(t, os.WriteFile(f, []byte("x\n"), 0o644))
	}
	popS2 := `mem.Pop("Max added")`
	replies := map[string]memReply{
		pushG:      {text: inG, events: []gnovm.MemFrame{pushed("g", "fix it")}},
		pushS1:     {text: inS1, events: []gnovm.MemFrame{pushed("g/s1", "find the cause")}},
		popS1PushS: {text: doneS1 + "\n" + inS2, events: []gnovm.MemFrame{poppedFrame("g/s1", "found it", 0), pushed("g/s2", "rerun")}},
		popS2:      {text: "done `g/s2` \"rerun\" → Max added", events: []gnovm.MemFrame{poppedFrame("g/s2", "Max added", 0)}},
	}
	s := newFramesScenario(t, replies, [][]fantasy.StreamPart{
		calling(memCall("c1", pushG)),
		calling(memCall("c2", pushS1)),
		calling(viewCall("v1", s1File)),
		calling(memCall("c3", popS1PushS)),
		calling(viewCall("v2", s2File), memCall("c4", popS2)),
		saying("done"),
	}, 200_000)
	var w closeWindow
	var ok bool
	s.mem.onCall = func(ctx context.Context, sessionID, callID string) {
		if callID == "c4" {
			w, ok = s.frames.closeWindow(ctx, sessionID, gnovm.MemClose{Id: "g/s2"}, callID)
		}
	}
	s.run("fix the flaky test")
	require.True(t, ok)
	require.True(t, w.onStack)
	var calls []string
	for _, r := range w.rows {
		for _, c := range r.ToolCalls() {
			calls = append(calls, c.ID)
		}
	}
	require.Equal(t, []string{"v2"}, calls, "only s2's own round: not s1's view, nor the pop and push")
	require.True(t, w.start.After(time.Now().Add(-time.Minute)))
}
