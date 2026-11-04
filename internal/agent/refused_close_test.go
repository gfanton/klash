package agent

import (
	"testing"

	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/stretchr/testify/require"
)

// A turn that would end in a frame mem refused to close, with no work
// since, is sent back; work after the refusal, a refusal in an earlier
// turn, or another frame's, is not.
func TestRefusedCloseLeft(t *testing.T) {
	t.Parallel()

	refused := message.ToolResult{
		ToolCallID: "m2", Name: "gnovm_run", IsError: true,
		Content: "ERROR — nothing from this call was kept: not popped: nothing came between your entering `wait-ready-fix` \"fix waitReady sleeps\" and this pop, so it is not done.",
	}
	turn := []message.Message{
		row(message.User, "u0", message.TextContent{Text: "fix the biggest cause"}),
		row(message.Assistant, "a1", callPart("m1")), // the push
		row(message.Tool, "t1", resultPart("m1")),
		row(message.Assistant, "a2", callPart("m2")), // the close, refused
		row(message.Tool, "t2", refused),
		row(message.Assistant, "a3", message.TextContent{Text: "Fixed."}),
	}
	require.True(t, refusedCloseLeft(turn, "wait-ready-fix"))
	require.False(t, refusedCloseLeft(turn, "other-frame"))

	worked := append(append([]message.Message{}, turn[:5]...),
		row(message.Assistant, "a3", message.ToolCall{ID: "e1", Name: tools.EditToolName, Input: `{"file_path":"helper_test.go"}`, Finished: true}),
		row(message.Tool, "t3", message.ToolResult{ToolCallID: "e1", Name: tools.EditToolName, Content: "edited"}),
		row(message.Assistant, "a4", message.TextContent{Text: "Done, the poll replaces the sleep."}))
	require.False(t, refusedCloseLeft(worked, "wait-ready-fix"), "work came after the refusal")

	later := append(append([]message.Message{}, turn...),
		row(message.User, "u1", message.TextContent{Text: "thanks"}),
		row(message.Assistant, "a4", message.TextContent{Text: "You're welcome."}))
	require.False(t, refusedCloseLeft(later, "wait-ready-fix"), "the refusal was in an earlier turn")
}
