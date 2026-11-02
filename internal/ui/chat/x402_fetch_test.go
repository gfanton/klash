package chat

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/stretchr/testify/require"
)

const (
	x402TestHash  = "d0839647f149e41972ca10aecb99c4ec6277bc803b8601150b72bc86f453d603"
	x402TestPayer = "g1jg8mtutu9khhfwc4nxmuhcpftf0pajdhfvsqf5"
)

// newX402FetchItem is an x402_fetch call that came back with result.
func newX402FetchItem(t *testing.T, result *message.ToolResult) ToolMessageItem {
	t.Helper()
	sty := styles.CharmtonePantera()
	return NewX402FetchToolMessageItem(&sty,
		message.ToolCall{ID: "1", Name: "x402_fetch", Input: `{"url":"http://localhost:8401/weather","key":"test1"}`, Finished: true},
		result, false)
}

// renderX402Fetch renders item at width, and returns the rendered lines and
// their text with the wrapping undone.
func renderX402Fetch(item ToolMessageItem, width int) ([]string, string) {
	out := item.Render(width)
	return strings.Split(out, "\n"), strings.Join(strings.Fields(stripANSI(out)), " ")
}

// The receipt names the transaction a user looks up to check the payment, and
// it is longer than any chat column, so it has to wrap rather than be cut.
func TestX402FetchShowsTheWholeReceipt(t *testing.T) {
	t.Parallel()

	receipt := "Paid 0.25 GNOT (250000ugnot) to g1u7y667z64x2h7vc6fmpcprgey4ck233jaww9zq on dev, plus a 0.02 GNOT (20000ugnot) network fee. Transaction " + x402TestHash + ", payer " + x402TestPayer + "."
	const content = `{"celsius":1,"forecast":"snow","location":"Paris"}`
	const width = 100
	lines, text := renderX402Fetch(newX402FetchItem(t, &message.ToolResult{ToolCallID: "1", Content: receipt + "\n\n" + content}), width)

	require.Contains(t, text, receipt)
	require.Contains(t, text, "1 "+content, "what was paid for keeps its numbered code view")
	for _, line := range lines {
		require.LessOrEqual(t, lipgloss.Width(line), width, "line %q", stripANSI(line))
	}
}

// A failed payment names the transaction that may have settled, so the user
// can look it up before paying again; cut to one line, the hash never shows.
func TestX402FetchShowsTheWholeError(t *testing.T) {
	t.Parallel()

	failure := `The payment was signed, but the paid request failed: Get "http://localhost:8401/weather": EOF. The payment may have settled: a seller settles before it answers. Its transaction is ` + x402TestHash + "; look it up before paying again. If no block executed it, it stays valid until the paying account sends another transaction"
	const width = 100
	lines, text := renderX402Fetch(newX402FetchItem(t, &message.ToolResult{ToolCallID: "1", Content: failure, IsError: true}), width)

	require.Contains(t, text, "ERROR "+failure)
	for _, line := range lines {
		require.LessOrEqual(t, lipgloss.Width(line), width, "line %q", stripANSI(line))
	}
}

// An error the tool writes as a list keeps a line for each item.
func TestX402FetchKeepsTheLinesOfAnError(t *testing.T) {
	t.Parallel()

	failure := "gnosh cannot pay any of the seller's offers:\n- exact 100ugnot on gno:gnoland1: gnosh pays only on dev and test chains\n- exact 5usdc on eip155:8453: not a gno.land chain"
	lines, _ := renderX402Fetch(newX402FetchItem(t, &message.ToolResult{ToolCallID: "1", Content: failure, IsError: true}), 100)

	var items []string
	for _, line := range lines {
		if item := strings.TrimSpace(stripANSI(line)); strings.HasPrefix(item, "- ") {
			items = append(items, item)
		}
	}
	require.Equal(t, []string{
		"- exact 100ugnot on gno:gnoland1: gnosh pays only on dev and test chains",
		"- exact 5usdc on eip155:8453: not a gno.land chain",
	}, items)
}

// The user declining the payment stays a warning, as every tool's denial is,
// however narrow the chat.
func TestX402FetchShowsADenialAsAWarning(t *testing.T) {
	t.Parallel()

	for _, width := range []int{100, 30} {
		t.Run(strconv.Itoa(width), func(t *testing.T) {
			t.Parallel()

			_, text := renderX402Fetch(newX402FetchItem(t, &message.ToolResult{ToolCallID: "1", Content: "User denied permission", IsError: true}), width)

			require.Contains(t, text, "WARN User")
			require.NotContains(t, text, "ERROR")
		})
	}
}

// The seller writes the reason it refused and the receipt's transaction, at
// any length, so each folds like long content until the item is expanded.
func TestX402FetchFoldsLongSellerText(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("reason ", 2000)
	results := map[string]*message.ToolResult{
		"error":   {ToolCallID: "1", Content: "Not paid: the seller refused the payment (" + long + "end).", IsError: true},
		"receipt": {ToolCallID: "1", Content: "Paid 0.25 GNOT (250000ugnot) to g1u7y667z64x2h7vc6fmpcprgey4ck233jaww9zq on dev, plus a 0.02 GNOT (20000ugnot) network fee. Transaction " + long + "end, payer " + x402TestPayer + ".\n\nsunny"},
	}
	for name, result := range results {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			item := newX402FetchItem(t, result)

			folded, text := renderX402Fetch(item, 100)
			require.Less(t, len(folded), 2*responseContextHeight, "a folded item stays short")
			require.Contains(t, text, "lines hidden")
			require.NotContains(t, text, "end")

			require.True(t, item.(Expandable).ToggleExpanded())
			_, text = renderX402Fetch(item, 100)
			require.Contains(t, text, "end")
		})
	}
}
