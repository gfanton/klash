package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"charm.land/fantasy"
	"github.com/charmbracelet/crush/internal/gnovm"
	gnovmtools "github.com/charmbracelet/crush/internal/gnovm/tools"
)

const (
	loopDetectionWindowSize = 10
	loopDetectionMaxRepeats = 5
)

// repeatedToolCalls checks whether the agent is stuck in a loop by looking
// at recent steps. It examines the last windowSize steps and, if any
// tool-call signature appears more than maxRepeats times, returns the tool
// calls of the step being repeated. A window of steps that all made tool
// calls, with no more than two signatures among them, is a loop too: going
// back and forth between two calls that answer the same each time, as a
// model went round mem.Drop and mem.Reopen for 12 minutes (QA, 2026-11-04),
// each signature at exactly half the window, never above it. It then
// returns the calls of both.
func repeatedToolCalls(steps []fantasy.StepResult, windowSize, maxRepeats int) ([]fantasy.ToolCallContent, bool) {
	if len(steps) < windowSize {
		return nil, false
	}

	window := steps[len(steps)-windowSize:]
	counts := make(map[string]int)
	var order []string
	callsOf := map[string][]fantasy.ToolCallContent{}
	signed := 0

	for _, step := range window {
		sig := getToolInteractionSignature(step.Content)
		if sig == "" {
			continue
		}
		signed++
		if counts[sig] == 0 {
			order = append(order, sig)
			callsOf[sig] = step.Content.ToolCalls()
		}
		counts[sig]++
		if counts[sig] > maxRepeats {
			return step.Content.ToolCalls(), true
		}
	}

	if signed == windowSize && len(order) <= 2 {
		var calls []fantasy.ToolCallContent
		for _, sig := range order {
			calls = append(calls, callsOf[sig]...)
		}
		return calls, true
	}
	return nil, false
}

// loopStopCondition is the StopWhen condition that stops a turn once the
// agent keeps making the same tool calls, handing the repeated calls to
// found. It only stops a turn that would otherwise go on. One whose last
// step ends it anyway is over: with an answer, which a nudge to change
// approach would replace, or with a call that halted the turn, such as a
// permission the user denied, which the nudge would resume.
func loopStopCondition(found func([]fantasy.ToolCallContent)) fantasy.StopCondition {
	return func(steps []fantasy.StepResult) bool {
		if len(steps) == 0 || !stepContinuesTurn(steps[len(steps)-1]) {
			return false
		}
		repeated, ok := repeatedToolCalls(steps, loopDetectionWindowSize, loopDetectionMaxRepeats)
		if ok {
			found(repeated)
		}
		return ok
	}
}

// stepContinuesTurn reports whether the model is asked again after step,
// by the same test the agent loop applies: the step made tool calls,
// finished for them, and none of their results halted the turn.
func stepContinuesTurn(step fantasy.StepResult) bool {
	if len(step.Content.ToolCalls()) == 0 || step.FinishReason != fantasy.FinishReasonToolCalls {
		return false
	}
	for _, result := range step.Content.ToolResults() {
		if result.StopTurn {
			return false
		}
	}
	return true
}

// loopContinuation is the follow-up queued for a turn that was stopped
// because the agent kept making the same tool calls and getting the same
// results. Ending the turn there left nothing to show for it: a sub-agent
// on the AI box re-read the same eight lines of a file six times running,
// was stopped, and handed its parent no output at all after 45 minutes of
// work. Told what it is doing, the agent can use what it already has or
// change approach.
//
// Like a Stop hook continuation, it is this same call with its prompt
// replaced, so it keeps the turn's RunID: the turn is not over. There is
// deliberately no cap on how often it happens, as with Stop hooks; each
// time, the loop detector needs a fresh window of steps before it can
// fire again, and the user can always cancel.
func loopContinuation(call SessionAgentCall, repeated []fantasy.ToolCallContent) SessionAgentCall {
	next := call
	next.Prompt = loopNudgePrompt(repeated)
	// Generated, not typed: keep it out of the transcript the user reads.
	next.HiddenUserMessage = true
	// Attachments belong to the user's original prompt and were already
	// delivered on this turn.
	next.Attachments = nil
	// A pending Stop hook checkpoint question is no longer the latest
	// prompt, so a stop marker in the answer to this one must not count.
	next.stopHookReflecting = false
	return next
}

func loopNudgePrompt(repeated []fantasy.ToolCallContent) string {
	var calls strings.Builder
	for _, tc := range repeated {
		fmt.Fprintf(&calls, "- %s %s\n", tc.ToolName, compactReason(tc.Input))
	}
	return fmt.Sprintf(
		"In your last %d steps you made the same tool calls over and over and got "+
			"the same results every time:\n\n%s\n"+
			"Making them again will not tell you anything new, so do not repeat them. "+
			"Use what they already returned: if that is enough, give your answer now; "+
			"if not, take a different approach.",
		loopDetectionWindowSize, calls.String(),
	)
}

// getToolInteractionSignature computes a hash signature for the tool
// interactions in a single step's content. It pairs tool calls with their
// results (matched by ToolCallID) and returns a hex-encoded SHA-256 hash.
// If the step contains no tool calls, it returns "".
func getToolInteractionSignature(content fantasy.ResponseContent) string {
	toolCalls := content.ToolCalls()
	if len(toolCalls) == 0 {
		return ""
	}

	// Index tool results by their ToolCallID for fast lookup.
	resultsByID := make(map[string]fantasy.ToolResultContent)
	for _, tr := range content.ToolResults() {
		resultsByID[tr.ToolCallID] = tr
	}

	h := sha256.New()
	for _, tc := range toolCalls {
		output := ""
		if tr, ok := resultsByID[tc.ToolCallID]; ok {
			output = toolResultOutputString(tr.Result)
		}
		if memRun(tc) {
			// mem's answer ends with what the run took ("ran 31ms · kept
			// as entry 23"), different every time: with its numbers
			// blanked, the same call answered the same way signs the
			// same, and a loop of calls to mem is seen. Another VM's
			// numbers can be its work, a counter moving on.
			output = runNumbers.ReplaceAllString(output, "#")
		}
		io.WriteString(h, tc.ToolName)
		io.WriteString(h, "\x00")
		io.WriteString(h, tc.Input)
		io.WriteString(h, "\x00")
		io.WriteString(h, output)
		io.WriteString(h, "\x00")
	}
	return hex.EncodeToString(h.Sum(nil))
}

// runNumbers is the numbers in mem's answer, blanked for its signature.
var runNumbers = regexp.MustCompile(`[0-9]+(\.[0-9]+)?`)

// memRun reports whether tc is a call to mem.
func memRun(tc fantasy.ToolCallContent) bool {
	if tc.ToolName != gnovmtools.RunToolName {
		return false
	}
	var input struct {
		Name string `json:"name"`
	}
	return json.Unmarshal([]byte(tc.Input), &input) == nil && input.Name == gnovm.MemVM
}

// toolResultOutputString converts a ToolResultOutputContent to a stable string
// representation for signature comparison.
func toolResultOutputString(result fantasy.ToolResultOutputContent) string {
	if result == nil {
		return ""
	}
	if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result); ok {
		return text.Text
	}
	if errResult, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result); ok {
		if errResult.Error != nil {
			return errResult.Error.Error()
		}
		return ""
	}
	if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result); ok {
		return media.Data
	}
	return ""
}
