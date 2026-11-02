package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/gnovm"
	gnovmtools "github.com/charmbracelet/crush/internal/gnovm/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/pubsub"
	"github.com/stretchr/testify/require"
)

func push(id, purpose string) gnovm.MemFrame {
	return gnovm.MemFrame{Kind: "push", Id: id, Purpose: purpose, Session: "s1"}
}

func pop(id string) gnovm.MemFrame {
	return gnovm.MemFrame{Kind: "pop", Id: id, Session: "s1"}
}

func popWith(id, result string) gnovm.MemFrame {
	return gnovm.MemFrame{Kind: "pop", Id: id, Result: result, Session: "s1"}
}

// memResult is a gnovm_run result whose metadata carries frames, as the
// tool writes it.
func memResult(t *testing.T, callID string, frames ...gnovm.MemFrame) message.ToolResult {
	t.Helper()
	meta, err := json.Marshal(gnovmtools.RunResponseMetadata{Frames: frames})
	require.NoError(t, err)
	return message.ToolResult{ToolCallID: callID, Name: gnovmtools.RunToolName, Content: "ok", Metadata: string(meta)}
}

func toolRow(id string, at int64, results ...message.ToolResult) message.Message {
	parts := make([]message.ContentPart, 0, len(results))
	for _, r := range results {
		parts = append(parts, r)
	}
	return message.Message{ID: id, SessionID: "s1", Role: message.Tool, CreatedAt: at, Parts: parts}
}

// cutMsg is a reply of size bytes that the pop or slide mark took out of
// the model's context: about size/4 tokens.
func cutMsg(id, mark string, size int) message.Message {
	return message.Message{
		ID: id, SessionID: "s1", Role: message.Assistant, Popped: mark,
		Parts: []message.ContentPart{message.TextContent{Text: strings.Repeat("x", size)}},
	}
}

func openFrames(f sessionFrames) []*frameNode {
	_, stack := f.tree()
	return stack
}

func stackIDs(f sessionFrames) []string {
	var ids []string
	for _, fr := range openFrames(f) {
		ids = append(ids, fr.id)
	}
	return ids
}

func TestDeriveFrames_TheStackIsThePushesNotYetPopped(t *testing.T) {
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("release", "release v2"))),
		toolRow("m2", 20, memResult(t, "c2", push("release/bump", "bump the version"))),
		toolRow("m3", 30, memResult(t, "c3", pop("release/bump"), push("release/tag", "tag the release"))),
	})

	require.Equal(t, []string{"release", "release/tag"}, stackIDs(f))
	top := openFrames(f)[1]
	require.Equal(t, "tag the release", top.purpose)
	require.Equal(t, int64(30), top.since)
}

func TestDeriveFrames_APopTakesItsOwnFrameOff(t *testing.T) {
	// A pop names its frame: it takes that one off even when it is not
	// on top, as the agent's own stack does.
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "a"), push("a/b", "b"))),
		toolRow("m2", 20, memResult(t, "c2", pop("a"))),
	})
	require.Equal(t, []string{"a/b"}, stackIDs(f))
}

func TestDeriveFrames_KeepsOnlyTheSessionsOwnSuccessfulMemCalls(t *testing.T) {
	copied := push("theirs", "a frame of the session this one was branched from")
	copied.Session = "s0"
	failed := memResult(t, "c3", push("failed", "a call that failed"))
	failed.IsError = true
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", copied)),
		toolRow("m2", 20, message.ToolResult{ToolCallID: "c2", Name: "bash", Metadata: `{"frames":[{"kind":"push","id":"x","session":"s1"}]}`}),
		toolRow("m3", 30, failed),
		toolRow("m4", 40, memResult(t, "c4", push("mine", "mine"))),
	})
	require.Equal(t, []string{"mine"}, stackIDs(f))
}

func TestSessionFrames_ObserveIsIdempotent(t *testing.T) {
	// A row is published again on every update: its push counts once.
	var f sessionFrames
	row := toolRow("m1", 10, memResult(t, "c1", push("a", "a")))
	require.True(t, f.observe("s1", row, time.Time{}))
	require.False(t, f.observe("s1", row, time.Time{}))
	require.Equal(t, []string{"a"}, stackIDs(f))
}

func TestSessionFrames_ForgetDropsADeletedRowsFrames(t *testing.T) {
	// /rewind deletes the rows after the point it goes back to.
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "a"))),
		toolRow("m2", 20, memResult(t, "c2", push("a/b", "b"))),
	})
	require.True(t, f.forget("m2"))
	require.False(t, f.forget("m2"))
	require.Equal(t, []string{"a"}, stackIDs(f))
}

func TestFrameTree_FramesNestAsTheyWereEntered(t *testing.T) {
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "task a"), push("a/b", "step b"))),
		toolRow("m2", 20, memResult(t, "c2", popWith("a/b", "b is done"), push("a/c", "step c"))),
		toolRow("m3", 30, memResult(t, "c3", push("a/c/d", "step d"))),
	})

	roots, stack := f.tree()
	require.Len(t, roots, 1)
	a := roots[0]
	require.Len(t, a.children, 2)
	b, c := a.children[0], a.children[1]
	require.True(t, b.popped)
	require.Equal(t, "b is done", b.result)
	require.False(t, c.popped)
	require.Len(t, c.children, 1)
	require.Equal(t, "a/c/d", c.children[0].id)
	require.Equal(t, []*frameNode{a, c, c.children[0]}, stack)
}

func TestFrameTree_EnteringAFrameAgainIsANewFrame(t *testing.T) {
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "a"))),
		toolRow("m2", 20, memResult(t, "c2", popWith("a", "first"), push("a", "a"))),
	})

	roots, stack := f.tree()
	require.Len(t, roots, 2)
	require.True(t, roots[0].popped)
	require.False(t, roots[1].popped)
	require.Equal(t, []*frameNode{roots[1]}, stack)
}

func TestFrameTree_EachFrameKeepsWhatItsPopFreed(t *testing.T) {
	pushes := toolRow("m1", 10, memResult(t, "c1", push("a", "a"), push("a/b", "b")))
	inB := cutMsg("m2", "a/b", 400)
	popB := toolRow("m3", 30, memResult(t, "c3", popWith("a/b", "done")))
	slid := cutMsg("m4", "slide::c1:c3", 40)
	f := deriveFrames("s1", []message.Message{pushes, inB, popB, slid})

	roots, _ := f.tree()
	a, b := roots[0], roots[0].children[0]
	require.Equal(t, int64(100), b.total())
	require.Equal(t, int64(100), a.total(), "a frame's figure holds its children's")
	rows, tokens := f.slid()
	require.Equal(t, 1, rows)
	require.Equal(t, int64(10), tokens)

	// The parent's pop marks the child's rows as its own; nothing is
	// counted twice.
	f.observe("s1", toolRow("m5", 50, memResult(t, "c5", popWith("a", "all done"))), time.Time{})
	inB.Popped = "a"
	require.True(t, f.observe("s1", inB, time.Time{}))
	roots, _ = f.tree()
	require.Equal(t, int64(0), roots[0].children[0].total())
	require.Equal(t, int64(100), roots[0].total())
}

// release-v2 as a session works it: two steps done, the third under way.
func releaseFrames(t *testing.T) sessionFrames {
	t.Helper()
	return deriveFrames("s1", []message.Message{
		toolRow("m1", 1_700_000_000, memResult(t, "c1", push("release-v2", "release v2"))),
		toolRow("m2", 1_700_000_010, memResult(t, "c2", push("release-v2/bump-version", "bump the version"))),
		cutMsg("m3", "release-v2/bump-version", 4000),
		toolRow("m4", 1_700_000_030, memResult(t, "c4", popWith("release-v2/bump-version", "VERSION says 2.0.0"), push("release-v2/tag-release", "tag the release"))),
		cutMsg("m5", "release-v2/tag-release", 400),
		toolRow("m6", 1_700_000_050, memResult(t, "c6", popWith("release-v2/tag-release", "tagged v2.0.0"), push("release-v2/ci-green", "CI is green"))),
	})
}

func TestStackSection_ShowsFinishedFramesWithTheirResults(t *testing.T) {
	got := stripANSI(stackSection(minimalFileStyles(), releaseFrames(t), 30, stackLook{}))

	lines := []string{
		"▸ release-v2",
		"├ ✓ bump-version",
		"│   VERSION says 2.0.0",
		"├ ✓ tag-release",
		"│   tagged v2.0.0",
		"└ ▸ ci-green",
		`"CI is green"`,
	}
	at := 0
	for _, want := range lines {
		i := strings.Index(got[at:], want)
		require.GreaterOrEqual(t, i, 0, "want %q after what came before, in:\n%s", want, got)
		at += i + len(want)
	}
	require.Contains(t, got, "−1K tok", "bump-version's pop freed about 1000 tokens")
	require.Contains(t, got, "−100 tok")
	require.Contains(t, got, time.Unix(1_700_000_050, 0).Format("15:04"), "the frame the agent is in says when it entered it")
	require.NotContains(t, got, "release-v2/bump-version", "a frame under its parent is named by its last part")
	require.NotContains(t, got, `"release v2"`, "only the frame the agent is in shows its purpose")
}

func TestStackSection_AFinishedTaskFoldsItsSteps(t *testing.T) {
	f := releaseFrames(t)
	f.observe("s1", toolRow("m7", 1_700_000_070, memResult(t, "c7", popWith("release-v2/ci-green", "CI passed"), popWith("release-v2", "released 2.0.0"))), time.Time{})

	got := stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{}))

	require.Contains(t, got, "✓ release-v2")
	require.Contains(t, got, "released 2.0.0")
	require.Contains(t, got, "−1.1K tok", "the task's figure holds its steps'")
	require.NotContains(t, got, "bump-version")
	require.NotContains(t, got, "▸")
}

func TestStackSection_TheHeadingSaysWhatLeftTheContextInAll(t *testing.T) {
	f := releaseFrames(t)
	heading := func() string {
		got := stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{}))
		return got[:strings.Index(got, "\n")]
	}

	require.Contains(t, heading(), "1.1K tok freed", "both pops")
	f.observe("s1", cutMsg("m9", "slide::c1:c4", 4000), time.Time{})
	require.Contains(t, heading(), "2.1K tok freed", "and what slid out")

	fresh := deriveFrames("s1", []message.Message{toolRow("m1", 10, memResult(t, "c1", push("a", "a")))})
	got := stripANSI(stackSection(minimalFileStyles(), fresh, 30, stackLook{}))
	require.NotContains(t, got, "freed", "nothing has left the context yet")
}

func TestStackSection_KeepsTheLastThreeFinishedTasks(t *testing.T) {
	var rows []message.Message
	for i := range 5 {
		id := fmt.Sprintf("task-%d", i+1)
		rows = append(rows, toolRow(fmt.Sprintf("m%d", i), int64(i), memResult(t, fmt.Sprintf("c%d", i), push(id, id), popWith(id, "done "+id))))
	}

	got := stripANSI(stackSection(minimalFileStyles(), deriveFrames("s1", rows), 30, stackLook{}))

	require.Contains(t, got, "…2 earlier")
	require.NotContains(t, got, "task-1")
	require.NotContains(t, got, "task-2")
	for _, id := range []string{"task-3", "task-4", "task-5"} {
		require.Contains(t, got, id)
	}
}

func TestStackSection_ShowsWhatSlidOut(t *testing.T) {
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "a"))),
		cutMsg("m2", "slide::c1:c9", 8000),
	})

	got := stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{}))

	require.Contains(t, got, "slid out")
	require.Contains(t, got, "1 msg")
	require.Contains(t, got, "2K tokens")
}

func TestStackSection_FitsTheSidebar(t *testing.T) {
	long := strings.Repeat("a long result that keeps going ", 4)
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a-very-long-todo-name-that-overflows", strings.Repeat("a long purpose ", 6)))),
		toolRow("m2", 20, memResult(t, "c2", push("a-very-long-todo-name-that-overflows/an-equally-long-step-name", "step"))),
		cutMsg("m3", "a-very-long-todo-name-that-overflows/an-equally-long-step-name", 400_000),
		toolRow("m4", 30, memResult(t, "c4", popWith("a-very-long-todo-name-that-overflows/an-equally-long-step-name", long), push("a-very-long-todo-name-that-overflows/x", "x"))),
		toolRow("m5", 40, memResult(t, "c5", push("a-very-long-todo-name-that-overflows/x/y", "y"), push("a-very-long-todo-name-that-overflows/x/y/z", long))),
	})
	for _, width := range []int{20, 30} {
		for _, line := range strings.Split(stripANSI(stackSection(minimalFileStyles(), f, width, stackLook{})), "\n") {
			require.LessOrEqual(t, lipgloss.Width(line), width, "line exceeds a sidebar %d wide: %q", width, line)
		}
	}
}

func TestStackSection_TheFrameAtWorkShowsHowLongItHasRun(t *testing.T) {
	f := releaseFrames(t)
	now := time.Unix(1_700_000_050+65, 0)

	working := stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{now: now, busy: true}))
	waiting := stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{now: now}))

	require.Contains(t, working, "1m05s")
	require.NotContains(t, waiting, "1m05s", "a waiting agent's clock would stand still")
	require.Contains(t, waiting, time.Unix(1_700_000_050, 0).Format("15:04"))
}

func TestStackSection_TheFrameAtWorkShimmers(t *testing.T) {
	f := releaseFrames(t)
	at := time.Unix(1_700_000_100, 0)
	draw := func(busy bool, after time.Duration) string {
		return stackSection(minimalFileStyles(), f, 30, stackLook{now: at.Add(after), busy: busy})
	}

	require.NotEqual(t, draw(true, 0), draw(true, 240*time.Millisecond), "its colors move while the agent works")
	require.Equal(t, stripANSI(draw(true, 0)), stripANSI(draw(true, 240*time.Millisecond)), "and only its colors")
	require.Equal(t, draw(false, 0), draw(false, 240*time.Millisecond), "they stand still while the agent waits")
}

func TestStackSection_APopCountsUpWhatItFreed(t *testing.T) {
	f := deriveFrames("s1", []message.Message{
		toolRow("m1", 10, memResult(t, "c1", push("a", "a"), push("a/b", "b"))),
		cutMsg("m2", "a/b", 4000),
	})
	popped := time.Unix(1_700_000_200, 0)
	f.observe("s1", toolRow("m3", 1_700_000_200, memResult(t, "c3", popWith("a/b", "done"))), popped)
	draw := func(after time.Duration) string {
		return stripANSI(stackSection(minimalFileStyles(), f, 30, stackLook{now: popped.Add(after)}))
	}

	require.Contains(t, draw(100*time.Millisecond), "✓ b")
	require.NotContains(t, draw(100*time.Millisecond), "−1K", "the count starts low")
	require.Contains(t, draw(2*time.Second), "−1K tok", "and ends at what the pop freed")
	require.True(t, f.animating(popped.Add(100*time.Millisecond), false))
	require.False(t, f.animating(popped.Add(2*time.Second), false))
}

func TestSessionFrames_AnimatesWhileTheAgentWorksInAFrame(t *testing.T) {
	now := time.Unix(1_700_000_100, 0)
	require.True(t, releaseFrames(t).animating(now, true))
	require.False(t, releaseFrames(t).animating(now, false), "a loaded pop does not flash")
	require.False(t, sessionFrames{}.animating(now, true), "nothing to animate outside a frame")
}

func TestStackSection_IsHiddenForASessionWithoutFrames(t *testing.T) {
	require.Empty(t, stackSection(minimalFileStyles(), sessionFrames{}, 30, stackLook{}))
}

// The sidebar follows the agent's frames as their rows arrive, and drops
// the ones /rewind deletes.
func TestSidebarShowsTheStackLive(t *testing.T) {
	// The section's heading, not the word, which the test's temporary
	// directory, shown as the working directory, also holds.
	const heading = "Stack ─"
	u, scr := sidebarUI(t, 60)
	require.Equal(t, -1, rowShowing(u, scr, heading), "no Stack section before a frame")

	send := func(typ pubsub.EventType, row message.Message) {
		t.Helper()
		_, _ = u.Update(pubsub.Event[message.Message]{Type: typ, Payload: row})
	}
	send(pubsub.CreatedEvent, toolRow("m1", 10, memResult(t, "c1", push("fix-flaky-test", "fix the flaky test"))))
	send(pubsub.CreatedEvent, toolRow("m2", 20, memResult(t, "c2", push("fix-flaky-test/find-cause", "find why TestFlaky fails"))))
	scr = drawSidebarOf(u)
	require.GreaterOrEqual(t, rowShowing(u, scr, heading), 0)
	require.GreaterOrEqual(t, rowShowing(u, scr, "find-cause"), 0)
	require.Less(t, rowShowing(u, scr, heading), rowShowing(u, scr, "Artifacts"), "the Stack sits above the Artifacts")

	send(pubsub.DeletedEvent, message.Message{ID: "m2", SessionID: "s1"})
	scr = drawSidebarOf(u)
	require.Equal(t, -1, rowShowing(u, scr, "find-cause"))
	require.GreaterOrEqual(t, rowShowing(u, scr, "fix-flaky-test"), 0)

	// A pop that just came keeps the animation clock running while it
	// flashes, the agent busy or not. The pending artifact would keep it
	// running on its own.
	u.sessionArtifacts = nil
	require.False(t, u.sidebarAnimating(time.Now()))
	send(pubsub.CreatedEvent, toolRow("m3", 30, memResult(t, "c3", popWith("fix-flaky-test", "fixed"))))
	require.True(t, u.sidebarAnimating(time.Now()))
}
