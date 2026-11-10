package agent

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/fantasy"
	"github.com/stretchr/testify/require"
)

// callingAtOnce is a step in which the model calls tool n times, each with
// its own query, as Qwen on the AI box called sourcegraph twice in one step.
func callingAtOnce(tool string, n int) []fantasy.StreamPart {
	var parts []fantasy.StreamPart
	for i := range n {
		id := fmt.Sprintf("call-%d", i)
		input := fmt.Sprintf(`{"query":"q%d"}`, i)
		parts = append(parts,
			fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputStart, ID: id, ToolCallName: tool},
			fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputDelta, ID: id, Delta: input},
			fantasy.StreamPart{Type: fantasy.StreamPartTypeToolInputEnd, ID: id},
			fantasy.StreamPart{Type: fantasy.StreamPartTypeToolCall, ID: id, ToolCallName: tool, ToolCallInput: input},
		)
	}
	return append(parts, fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls})
}

type queryParams struct {
	Query string `json:"query"`
}

// Tools that may run at once, as sourcegraph, web_fetch and agentic_fetch
// may, finish on goroutines of their own, and the turn takes each result
// there. Two sourcegraph calls finishing together killed the AI box's
// gnosh on 2026-11-10: the turn noted the tools that had finished in a map
// without a lock, and Go stops a program that writes a map from two
// goroutines at once ("fatal error: concurrent map writes"). Here five
// calls finish at the same moment; under -race, as CI runs the suite, an
// unguarded write fails the test every time.
func TestRun_ToolsFinishingTogether(t *testing.T) {
	t.Parallel()

	// Five is as many as fantasy runs at once.
	const n = 5
	var started atomic.Int32
	allRunning := make(chan struct{})
	search := fantasy.NewParallelAgentTool("search", "Searches.",
		func(ctx context.Context, p queryParams, _ fantasy.ToolCall) (fantasy.ToolResponse, error) {
			if started.Add(1) == n {
				close(allRunning)
			}
			// Each call returns once all of them are running, so that they
			// finish together; should fewer run at once, they finish anyway.
			select {
			case <-allRunning:
			case <-time.After(10 * time.Second):
			case <-ctx.Done():
			}
			return fantasy.NewTextResponse("found " + p.Query), nil
		})

	env := testEnv(t)
	llm := &scriptedModel{steps: [][]fantasy.StreamPart{callingAtOnce("search", n), saying("Found them.")}}
	a := testSessionAgent(env, llm, llm, "test", search)
	sess, err := env.sessions.Create(t.Context(), "tools at once")
	require.NoError(t, err)

	_, err = a.Run(t.Context(), SessionAgentCall{SessionID: sess.ID, Prompt: "search for all five"})
	require.NoError(t, err)

	select {
	case <-allRunning:
	default:
		t.Fatalf("only %d of the %d calls ran at once", started.Load(), n)
	}
	msgs, err := env.messages.List(t.Context(), sess.ID)
	require.NoError(t, err)
	var results []string
	for _, msg := range msgs {
		for _, r := range msg.ToolResults() {
			require.False(t, r.IsError, r.Content)
			results = append(results, r.Content)
		}
	}
	require.ElementsMatch(t, []string{"found q0", "found q1", "found q2", "found q3", "found q4"}, results,
		"every result is kept")
	require.Equal(t, "Found them.", msgs[len(msgs)-1].Content().String(), "and the turn goes on to its answer")
}
