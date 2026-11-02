package chat

import (
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/agent/tools"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// -----------------------------------------------------------------------------
// x402 Fetch Tool
// -----------------------------------------------------------------------------

// X402FetchToolMessageItem is a message item that represents an x402_fetch
// tool call: a fetch the user may pay for.
type X402FetchToolMessageItem struct {
	*baseToolMessageItem
}

var _ ToolMessageItem = (*X402FetchToolMessageItem)(nil)

// NewX402FetchToolMessageItem creates a new [X402FetchToolMessageItem].
func NewX402FetchToolMessageItem(
	sty *styles.Styles,
	toolCall message.ToolCall,
	result *message.ToolResult,
	canceled bool,
) ToolMessageItem {
	return newBaseToolMessageItem(sty, toolCall, result, &X402FetchToolRenderContext{}, canceled)
}

// X402FetchToolRenderContext renders x402_fetch tool messages: the URL and the
// key that pays, then the receipt and the content, or why nothing was paid.
type X402FetchToolRenderContext struct{}

// RenderTool implements the [ToolRenderer] interface.
func (x *X402FetchToolRenderContext) RenderTool(sty *styles.Styles, width int, opts *ToolRenderOpts) string {
	cappedWidth := cappedMessageWidth(width)
	if opts.IsPending() {
		return pendingTool(sty, "Pay", opts.Anim, opts.Compact)
	}

	var params tools.X402FetchParams
	if err := json.Unmarshal([]byte(opts.ToolCall.Input), &params); err != nil {
		return toolErrorContent(sty, &message.ToolResult{Content: "Invalid parameters"}, cappedWidth)
	}

	toolParams := []string{params.URL, "key", params.Key}
	if params.Master != "" {
		toolParams = append(toolParams, "for", params.Master)
	}

	header := toolHeader(sty, opts.Status, "Pay", cappedWidth, opts, toolParams...)
	if opts.Compact {
		return header
	}

	if opts.Status == ToolStatusError {
		return joinToolParts(header, x402ErrorContent(sty, opts.Result, cappedWidth, opts.ExpandedContent))
	}
	if earlyState, ok := toolEarlyStateContent(sty, opts, cappedWidth); ok {
		return joinToolParts(header, earlyState)
	}

	if opts.HasEmptyResult() {
		return header
	}

	// The tool writes the receipt, a blank line, then what was paid for.
	receipt, content, _ := strings.Cut(opts.Result.Content, "\n\n")
	bodyWidth := cappedWidth - toolBodyLeftPaddingTotal
	paid := capLineWidths(sty.Tool.ContentLine.Width(bodyWidth).PaddingLeft(1).Render(receipt), bodyWidth)
	paid = sty.Tool.Body.Render(x402Folded(sty, paid, bodyWidth, opts.ExpandedContent))
	if content == "" {
		return joinToolParts(header, paid)
	}
	body := toolOutputCodeContent(sty, getFileExtensionForFormat(params.Format), content, 0, cappedWidth, opts.ExpandedContent)
	return joinToolParts(header, paid+"\n\n"+body)
}

// x402ErrorContent is the ERROR line every tool shows, wrapped where the
// message does not fit it: a failed payment names the transaction to look up
// near the end of its message.
func x402ErrorContent(sty *styles.Styles, result *message.ToolResult, width int, expanded bool) string {
	tag := sty.Tool.ErrorTag.Render("ERROR")
	msgWidth := width - lipgloss.Width(tag) - 3
	if result == nil || isUserRefusal(result.Content) ||
		!strings.Contains(result.Content, "\n") && lipgloss.Width(result.Content) <= msgWidth {
		return toolErrorContent(sty, result, width)
	}
	msg := capLineWidths(sty.Tool.ErrorMessage.Width(msgWidth).Render(result.Content), msgWidth)
	return lipgloss.JoinHorizontal(lipgloss.Top, tag+" ", x402Folded(sty, msg, msgWidth, expanded))
}

// x402Folded keeps the first lines of a wrapped block until the item is
// expanded: the seller writes the text of a receipt and of a refusal, at any
// length.
func x402Folded(sty *styles.Styles, block string, width int, expanded bool) string {
	lines := strings.Split(block, "\n")
	maxLines := collapsedMaxLines(len(lines))
	if expanded || len(lines) <= maxLines {
		return block
	}
	hint := sty.Tool.ContentTruncation.Width(width).Render(fmt.Sprintf(assistantMessageTruncateFormat, len(lines)-maxLines))
	return strings.Join(append(lines[:maxLines], hint), "\n")
}
