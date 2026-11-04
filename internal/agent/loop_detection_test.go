package agent

import (
	"fmt"
	"testing"

	"charm.land/fantasy"
	gnovmtools "github.com/charmbracelet/crush/internal/gnovm/tools"
)

// makeStep creates a StepResult with the given tool calls and results in its Content.
func makeStep(calls []fantasy.ToolCallContent, results []fantasy.ToolResultContent) fantasy.StepResult {
	var content fantasy.ResponseContent
	for _, c := range calls {
		content = append(content, c)
	}
	for _, r := range results {
		content = append(content, r)
	}
	return fantasy.StepResult{
		Response: fantasy.Response{
			Content: content,
		},
	}
}

// makeToolStep creates a step with a single tool call and matching text result.
func makeToolStep(name, input, output string) fantasy.StepResult {
	callID := fmt.Sprintf("call_%s_%s", name, input)
	return makeStep(
		[]fantasy.ToolCallContent{
			{ToolCallID: callID, ToolName: name, Input: input},
		},
		[]fantasy.ToolResultContent{
			{ToolCallID: callID, ToolName: name, Result: fantasy.ToolResultOutputContentText{Text: output}},
		},
	)
}

// makeEmptyStep creates a step with no tool calls (e.g. a text-only response).
func makeEmptyStep() fantasy.StepResult {
	return fantasy.StepResult{
		Response: fantasy.Response{
			Content: fantasy.ResponseContent{
				fantasy.TextContent{Text: "thinking..."},
			},
		},
	}
}

func TestRepeatedToolCalls(t *testing.T) {
	t.Run("no steps", func(t *testing.T) {
		_, result := repeatedToolCalls(nil, 10, 5)
		if result {
			t.Error("expected false for empty steps")
		}
	})

	t.Run("fewer steps than window", func(t *testing.T) {
		steps := make([]fantasy.StepResult, 5)
		for i := range steps {
			steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content")
		}
		_, result := repeatedToolCalls(steps, 10, 5)
		if result {
			t.Error("expected false when fewer steps than window size")
		}
	})

	t.Run("all different signatures", func(t *testing.T) {
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			steps[i] = makeToolStep("tool", fmt.Sprintf(`{"i":%d}`, i), fmt.Sprintf("result-%d", i))
		}
		_, result := repeatedToolCalls(steps, 10, 5)
		if result {
			t.Error("expected false when all signatures are different")
		}
	})

	t.Run("exact repeat at threshold not detected", func(t *testing.T) {
		// maxRepeats=5 means > 5 is needed, so exactly 5 should return false
		steps := make([]fantasy.StepResult, 10)
		for i := range 5 {
			steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content")
		}
		for i := 5; i < 10; i++ {
			steps[i] = makeToolStep("tool", fmt.Sprintf(`{"i":%d}`, i), fmt.Sprintf("result-%d", i))
		}
		_, result := repeatedToolCalls(steps, 10, 5)
		if result {
			t.Error("expected false when count equals maxRepeats (threshold is >)")
		}
	})

	t.Run("loop detected", func(t *testing.T) {
		// 6 identical steps in a window of 10 with maxRepeats=5 → detected
		steps := make([]fantasy.StepResult, 10)
		for i := range 6 {
			steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content")
		}
		for i := 6; i < 10; i++ {
			steps[i] = makeToolStep("tool", fmt.Sprintf(`{"i":%d}`, i), fmt.Sprintf("result-%d", i))
		}
		_, result := repeatedToolCalls(steps, 10, 5)
		if !result {
			t.Error("expected true when same signature appears more than maxRepeats times")
		}
	})

	t.Run("steps without tool calls are skipped", func(t *testing.T) {
		// Mix of tool steps and empty steps — empty ones should not affect counts
		steps := make([]fantasy.StepResult, 10)
		for i := range 4 {
			steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content")
		}
		for i := 4; i < 8; i++ {
			steps[i] = makeEmptyStep()
		}
		for i := 8; i < 10; i++ {
			steps[i] = makeToolStep("write", `{"file":"b.go"}`, "ok")
		}
		_, result := repeatedToolCalls(steps, 10, 5)
		if result {
			t.Error("expected false: only 4 repeated tool calls, empty steps should be skipped")
		}
	})

	t.Run("two patterns alternating", func(t *testing.T) {
		// Two patterns alternating, each 5 times: never above the
		// threshold, but back and forth between two calls that answer the
		// same every time is a loop.
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			if i%2 == 0 {
				steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content-a")
			} else {
				steps[i] = makeToolStep("write", `{"file":"b.go"}`, "content-b")
			}
		}
		calls, result := repeatedToolCalls(steps, 10, 5)
		if !result || len(calls) != 2 || calls[0].ToolName != "read" || calls[1].ToolName != "write" {
			t.Errorf("expected the two alternating calls, got %v %v", result, calls)
		}
	})

	t.Run("three patterns cycling", func(t *testing.T) {
		// Three calls in turn are not caught by the cycle rule: work that
		// goes round edit, test and view is often progress.
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			steps[i] = makeToolStep("tool", fmt.Sprintf(`{"i":%d}`, i%3), fmt.Sprintf("result-%d", i%3))
		}
		if _, result := repeatedToolCalls(steps, 10, 5); result {
			t.Error("expected false: three patterns, none above the threshold")
		}
	})

	t.Run("two patterns with a step that made no call", func(t *testing.T) {
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			if i%2 == 0 {
				steps[i] = makeToolStep("read", `{"file":"a.go"}`, "content-a")
			} else {
				steps[i] = makeToolStep("write", `{"file":"b.go"}`, "content-b")
			}
		}
		steps[9] = makeEmptyStep()
		if _, result := repeatedToolCalls(steps, 10, 5); result {
			t.Error("expected false: the cycle rule needs every step of the window to call tools")
		}
	})

	t.Run("a call to mem repeated, its receipt's numbers changing", func(t *testing.T) {
		// mem's receipts end with the run's time and entry number, so the
		// same call never answered the same text twice.
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			steps[i] = makeToolStep(gnovmtools.RunToolName, `{"name":"mem","code":"mem.Reopen(\"x\")"}`,
				fmt.Sprintf("reopened `x` \"do x\"\nok · mem · ran %dms · kept as entry %d", 20+i, 40+i))
		}
		if _, result := repeatedToolCalls(steps, 10, 5); !result {
			t.Error("expected true: the same call to mem, answered the same but for its numbers")
		}
	})

	t.Run("other tools' numbers still count", func(t *testing.T) {
		steps := make([]fantasy.StepResult, 10)
		for i := range steps {
			steps[i] = makeToolStep("bash", `{"command":"cat progress"}`, fmt.Sprintf("%d%% done", i*10))
		}
		if _, result := repeatedToolCalls(steps, 10, 5); result {
			t.Error("expected false: a command whose output moves on is not a loop")
		}
	})
}

func TestGetToolInteractionSignature(t *testing.T) {
	t.Run("empty content returns empty string", func(t *testing.T) {
		sig := getToolInteractionSignature(fantasy.ResponseContent{})
		if sig != "" {
			t.Errorf("expected empty string, got %q", sig)
		}
	})

	t.Run("text only content returns empty string", func(t *testing.T) {
		content := fantasy.ResponseContent{
			fantasy.TextContent{Text: "hello"},
		}
		sig := getToolInteractionSignature(content)
		if sig != "" {
			t.Errorf("expected empty string, got %q", sig)
		}
	})

	t.Run("tool call with result produces signature", func(t *testing.T) {
		content := fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "1", ToolName: "read", Input: `{"file":"a.go"}`},
			fantasy.ToolResultContent{ToolCallID: "1", ToolName: "read", Result: fantasy.ToolResultOutputContentText{Text: "content"}},
		}
		sig := getToolInteractionSignature(content)
		if sig == "" {
			t.Error("expected non-empty signature")
		}
	})

	t.Run("same interactions produce same signature", func(t *testing.T) {
		content1 := fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "1", ToolName: "read", Input: `{"file":"a.go"}`},
			fantasy.ToolResultContent{ToolCallID: "1", ToolName: "read", Result: fantasy.ToolResultOutputContentText{Text: "content"}},
		}
		content2 := fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "2", ToolName: "read", Input: `{"file":"a.go"}`},
			fantasy.ToolResultContent{ToolCallID: "2", ToolName: "read", Result: fantasy.ToolResultOutputContentText{Text: "content"}},
		}
		sig1 := getToolInteractionSignature(content1)
		sig2 := getToolInteractionSignature(content2)
		if sig1 != sig2 {
			t.Errorf("expected same signature for same interactions, got %q and %q", sig1, sig2)
		}
	})

	t.Run("different inputs produce different signatures", func(t *testing.T) {
		content1 := fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "1", ToolName: "read", Input: `{"file":"a.go"}`},
			fantasy.ToolResultContent{ToolCallID: "1", ToolName: "read", Result: fantasy.ToolResultOutputContentText{Text: "content"}},
		}
		content2 := fantasy.ResponseContent{
			fantasy.ToolCallContent{ToolCallID: "1", ToolName: "read", Input: `{"file":"b.go"}`},
			fantasy.ToolResultContent{ToolCallID: "1", ToolName: "read", Result: fantasy.ToolResultOutputContentText{Text: "content"}},
		}
		sig1 := getToolInteractionSignature(content1)
		sig2 := getToolInteractionSignature(content2)
		if sig1 == sig2 {
			t.Error("expected different signatures for different inputs")
		}
	})
}

// The loop detector only stops a turn that would otherwise go on. Six
// repeats in the window used to flag the step that ended the turn too,
// replacing a final answer with a nudge, or resuming a turn halted by a
// permission the user denied.
func TestLoopStopCondition(t *testing.T) {
	t.Parallel()

	toolStep := func(input string, stopTurn bool) fantasy.StepResult {
		callID := "call_" + input
		step := makeStep(
			[]fantasy.ToolCallContent{{ToolCallID: callID, ToolName: "view", Input: input}},
			[]fantasy.ToolResultContent{{
				ToolCallID: callID,
				ToolName:   "view",
				Result:     fantasy.ToolResultOutputContentText{Text: "same lines"},
				StopTurn:   stopTurn,
			}},
		)
		step.FinishReason = fantasy.FinishReasonToolCalls
		return step
	}
	// Nine steps, six of them the same call with the same result.
	firstNine := func() []fantasy.StepResult {
		var steps []fantasy.StepResult
		for range 6 {
			steps = append(steps, toolStep(`{"file_path":"a.go"}`, false))
		}
		for i := range 3 {
			steps = append(steps, toolStep(fmt.Sprintf(`{"file_path":"%d.go"}`, i), false))
		}
		return steps
	}
	run := func(last fantasy.StepResult) (bool, []fantasy.ToolCallContent) {
		var found []fantasy.ToolCallContent
		stop := loopStopCondition(func(repeated []fantasy.ToolCallContent) { found = repeated })
		return stop(append(firstNine(), last)), found
	}

	stopped, found := run(toolStep(`{"file_path":"b.go"}`, false))
	if !stopped || len(found) != 1 || found[0].Input != `{"file_path":"a.go"}` {
		t.Fatalf("a turn that would go on must be stopped for the repeated call, got %v %v", stopped, found)
	}

	answer := makeEmptyStep()
	answer.FinishReason = fantasy.FinishReasonStop
	if stopped, found := run(answer); stopped || found != nil {
		t.Fatal("a final answer ends the turn; it must not be taken for a loop")
	}

	if stopped, found := run(toolStep(`{"file_path":"b.go"}`, true)); stopped || found != nil {
		t.Fatal("a call that halted the turn, such as a denied permission, must not be resumed")
	}
}
