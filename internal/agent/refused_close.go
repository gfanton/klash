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
// would end in a frame whose last close was refused (refusedCloseLeft): the
// model wrote its result before the work.
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
	if err != nil {
		return SessionAgentCall{}, false
	}
	refusal, idle := refusedCloseLeft(msgs, top.id)
	if refusal == "" {
		return SessionAgentCall{}, false
	}
	cont := call
	cont.refusedCloseSent = true
	if idle && (strings.Contains(refusal, "nothing came between your entering") || strings.Contains(refusal, "this call entered")) {
		a.notifyGoal(call.SessionID, notify.TypeWarning, fmt.Sprintf("mem refused to close `%s`, since nothing was done in it; the agent goes back to it", top.id))
		cont.Prompt = fmt.Sprintf(refusedClosePrompt, top.id)
	} else {
		a.notifyGoal(call.SessionID, notify.TypeWarning, fmt.Sprintf("The check refused to close `%s`, and the turn ended with it open; the agent goes back to it", top.id))
		cont.Prompt = fmt.Sprintf(checkRefusedClosePrompt, top.id, refusal)
	}
	// Generated, not typed, and the attachments went with the prompt.
	cont.HiddenUserMessage = true
	cont.Attachments = nil
	cont.stopHookReflecting = false
	return cont, true
}

// refusedClosePrompt is what the agent is sent back with, for the frame,
// when its close was refused as never done; checkRefusedClosePrompt, when
// the check of its close refused it, with the refusal.
const (
	refusedClosePrompt      = "mem refused to close `%s`: nothing was done in it, and it is still open. Do its work, then pop it with what came of it; or, if it cannot be done now, tell the user plainly that it is not done. Do not tell the user it is done."
	checkRefusedClosePrompt = "The close of `%s` was refused, and it is still open: %s\n\nDo what is missing, then pop it with what came of it; or, if it cannot be done now, tell the user plainly that it is not done. Do not tell the user it is done."
)

// refusedCloseLeft is the refusal of the last attempt, in the turn msgs
// end with, to close frame id, by mem as never done (refuseFresh) or by
// the check of closes (checkClose); "" when the turn made none, or the
// last was kept. idle is set when no work came in or after the step that
// made it (workIn). The turn's end sends the model back to a frame left so
// even when it did work after the refusal: one look after a refused claim
// counted as work, and the turn ended with the todo open and the user told
// it was done (QA, 2026-11-04).
func refusedCloseLeft(msgs []message.Message, id string) (refusal string, idle bool) {
	step := -1
	for i := turnStartAt(msgs, len(msgs)); i < len(msgs); i++ {
		for _, r := range msgs[i].ToolResults() {
			switch {
			case r.IsError && refusedClose(r.Content) == id:
				refusal = refusalLine(r.Content)
				step = slices.IndexFunc(msgs[:i], func(m message.Message) bool { return m.Role == message.Assistant && hasToolCall(m, r.ToolCallID) })
			case !r.IsError && (strings.Contains(r.Content, "done `"+id+"`") || strings.Contains(r.Content, "dropped `"+id+"`") || strings.Contains(r.Content, "left `"+id+"`")):
				refusal, step = "", -1
			}
		}
	}
	return refusal, refusal != "" && (step < 0 || !workIn(msgs[step:], id))
}

// refusalLine is the refusal of a close in a call's result: its line from
// "not popped:" or "not closed:".
func refusalLine(content string) string {
	i := closeRefusal.FindStringIndex(content)
	if i == nil {
		return ""
	}
	line, _, _ := strings.Cut(content[i[0]:], "\n")
	return line
}
