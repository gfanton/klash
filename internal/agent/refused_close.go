package agent

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/crush/internal/agent/notify"
	"github.com/charmbracelet/crush/internal/message"
)

// refusedCloseContinuation sends the agent back, once a turn, when the turn
// would end in a frame mem refused to close as never done, with no work
// since (refusedCloseLeft): the model wrote its result before the work.
// Left there, a model told the user "Fixed.", with made-up timings, of a
// file it never touched; mem stayed true and the user did not (QA,
// 2026-11-04). The user is told too.
func (a *sessionAgent) refusedCloseContinuation(ctx context.Context, call SessionAgentCall) (SessionAgentCall, bool) {
	if call.refusedCloseSent || a.frames == nil {
		return SessionAgentCall{}, false
	}
	top, ok := a.frames.top(ctx, call.SessionID)
	if !ok {
		return SessionAgentCall{}, false
	}
	msgs, err := a.messages.List(ctx, call.SessionID)
	if err != nil || !refusedCloseLeft(msgs, top.id) {
		return SessionAgentCall{}, false
	}
	a.notifyGoal(call.SessionID, notify.TypeWarning, fmt.Sprintf("mem refused to close `%s`, since nothing was done in it; the agent goes back to it", top.id))
	cont := call
	cont.refusedCloseSent = true
	cont.Prompt = fmt.Sprintf(refusedClosePrompt, top.id)
	// Generated, not typed, and the attachments went with the prompt.
	cont.HiddenUserMessage = true
	cont.Attachments = nil
	cont.stopHookReflecting = false
	return cont, true
}

// refusedClosePrompt is what the agent is sent back with, for the frame.
const refusedClosePrompt = "mem refused to close `%s`: nothing was done in it, and it is still open. Do its work, then pop it with what came of it; or, if it cannot be done now, tell the user plainly that it is not done. Do not tell the user it is done."

// refusedCloseLeft reports whether, in the turn msgs end with, mem refused
// a pop or close of frame id as never done (refuseFresh), and no work came
// in or after the step that made the last such refusal (workIn).
func refusedCloseLeft(msgs []message.Message, id string) bool {
	refusal := refusedFreshText(id)
	step := -1
	for i := turnStartAt(msgs, len(msgs)); i < len(msgs); i++ {
		for _, r := range msgs[i].ToolResults() {
			if r.IsError && strings.Contains(r.Content, refusal) {
				step = slices.IndexFunc(msgs[:i], func(m message.Message) bool { return m.Role == message.Assistant && hasToolCall(m, r.ToolCallID) })
			}
		}
	}
	return step >= 0 && !workIn(msgs[step:], id)
}
