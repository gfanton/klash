package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// A turn that would end in a frame whose last close was refused is sent
// back, idle or not: one look after a refused claim counted as work, and
// the turn ended with the todo open (QA, 2026-11-04). A close kept after
// the refusal, a refusal in an earlier turn, or another frame's, is not.
func TestRefusedCloseLeft(t *testing.T) {
	t.Parallel()

	refused := message.ToolResult{
		ToolCallID: "m2", Name: "gnovm_run", IsError: true,
		Content: "ERROR — nothing from this call was kept: not popped: nothing came between your entering `wait-ready-fix` \"fix waitReady sleeps\" and this pop, so it is not done.\nfailed · mem · ran 2ms · nothing kept\n",
	}
	closeCall := message.ToolCall{ID: "m2", Name: "gnovm_run", Input: `{"name":"mem","code":"mem.Done(\"wait-ready-fix\", \"polls /ready every 5ms\")"}`, Finished: true}
	turn := []message.Message{
		row(message.User, "u0", message.TextContent{Text: "fix the biggest cause"}),
		row(message.Assistant, "a1", callPart("m1")), // the push
		row(message.Tool, "t1", resultPart("m1")),
		row(message.Assistant, "a2", closeCall), // the close, refused
		row(message.Tool, "t2", refused),
		row(message.Assistant, "a3", message.TextContent{Text: "Fixed."}),
	}
	refusal, idle := refusedCloseLeft(turn, "wait-ready-fix")
	require.Equal(t, "not popped: nothing came between your entering `wait-ready-fix` \"fix waitReady sleeps\" and this pop, so it is not done.", refusal)
	require.True(t, idle)
	refusal, _ = refusedCloseLeft(turn, "other-frame")
	require.Empty(t, refusal)

	worked := append(append([]message.Message{}, turn[:5]...),
		row(message.Assistant, "a3", message.ToolCall{ID: "e1", Name: tools.EditToolName, Input: `{"file_path":"helper_test.go"}`, Finished: true}),
		row(message.Tool, "t3", message.ToolResult{ToolCallID: "e1", Name: tools.EditToolName, Content: "edited"}),
		row(message.Assistant, "a4", message.TextContent{Text: "Done, the poll replaces the sleep."}))
	refusal, idle = refusedCloseLeft(worked, "wait-ready-fix")
	require.NotEmpty(t, refusal, "work after the refusal, but the todo is still open")
	require.False(t, idle)

	checked := []message.Message{
		turn[0], turn[1], turn[2],
		row(message.Assistant, "a2", closeCall),
		row(message.Tool, "t2", message.ToolResult{ToolCallID: "m2", Name: "gnovm_run", IsError: true, Content: "ERROR — nothing from this call was kept: not popped: the check of `wait-ready-fix` found: no edit of helper_test.go is shown. Do what is missing; …\nfailed · mem · ran 2ms · nothing kept\n"}),
		row(message.Assistant, "a3", message.ToolCall{ID: "l1", Name: tools.LSToolName, Input: `{}`, Finished: true}),
		row(message.Tool, "t3", message.ToolResult{ToolCallID: "l1", Name: tools.LSToolName, Content: "helper_test.go"}),
		row(message.Assistant, "a4", message.TextContent{Text: "Fixed."}),
	}
	refusal, _ = refusedCloseLeft(checked, "wait-ready-fix")
	require.Contains(t, refusal, "not popped: the check of `wait-ready-fix` found: no edit of helper_test.go is shown")

	kept := append(append([]message.Message{}, worked[:7]...),
		row(message.Assistant, "a5", message.ToolCall{ID: "m3", Name: "gnovm_run", Input: `{"name":"mem","code":"mem.Pop(\"polls /ready\")"}`, Finished: true}),
		row(message.Tool, "t5", message.ToolResult{ToolCallID: "m3", Name: "gnovm_run", Content: "done `wait-ready-fix` \"fix waitReady sleeps\" → polls /ready\nok · mem"}),
		row(message.Assistant, "a6", message.TextContent{Text: "Done."}))
	refusal, _ = refusedCloseLeft(kept, "wait-ready-fix")
	require.Empty(t, refusal, "the close was kept after the refusal")

	later := append(append([]message.Message{}, turn...),
		row(message.User, "u1", message.TextContent{Text: "thanks"}),
		row(message.Assistant, "a4", message.TextContent{Text: "You're welcome."}))
	refusal, _ = refusedCloseLeft(later, "wait-ready-fix")
	require.Empty(t, refusal, "the refusal was in an earlier turn")
}
