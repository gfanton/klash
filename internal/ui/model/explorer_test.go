package model

import (
	"image"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/crush/internal/message"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/stretchr/testify/require"
)

// explorerRows is a session in fix-flaky-test › find-cause ›
// try-free-port, after reproduce-it's pop took out 2K tokens, as its rows
// come: each call's assistant row, then the tool row answering it.
func explorerRows(t *testing.T) []message.Message {
	t.Helper()
	return []message.Message{
		callRow("e1", 1_700_000_000, memCall("fc1")),
		toolRow("f1", 1_700_000_000, memResult(t, "fc1", push("fix-flaky-test", "fix the flaky test"))),
		callRow("e2", 1_700_000_010, memCall("fc2")),
		toolRow("f2", 1_700_000_010, memResult(t, "fc2", push("fix-flaky-test/reproduce-it", "make it fail"))),
		cutMsg("f3", "fix-flaky-test/reproduce-it", 8000),
		callRow("e4", 1_700_000_030, memCall("fc4")),
		toolRow("f4", 1_700_000_030, memResult(t, "fc4", popWith("fix-flaky-test/reproduce-it", "fails 3 times in 20"), push("fix-flaky-test/find-cause", "find why it fails"))),
		callRow("e5", 1_700_000_040, memCall("fc5")),
		toolRow("f5", 1_700_000_040, memResult(t, "fc5", push("fix-flaky-test/find-cause/try-free-port", "listen on a free port"))),
	}
}

// explorerUI is a UI of width × height with the frames of explorerRows,
// their pops long seen.
func explorerUI(t *testing.T, width, height int) *UI {
	t.Helper()
	u, _ := sidebarUI(t, height)
	u.header = newHeader(u.com)
	u.keyMap = DefaultKeyMap()
	u.status = NewStatus(u.com, u)
	u.width = width
	u.updateLayoutAndSize()
	observeRows(u, explorerRows(t)...)
	for i := range u.sessionFrames.calls {
		u.sessionFrames.calls[i].seen = time.Time{}
	}
	return u
}

func press(u *UI, keys ...tea.KeyPressMsg) {
	for _, k := range keys {
		_, _ = u.Update(k)
	}
}

var (
	ctrlX = tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl}
	esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	tab   = tea.KeyPressMsg{Code: tea.KeyTab}
)

func typed(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func click(u *UI, x, y int) {
	_, _ = u.Update(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: uv.MouseLeft}))
}

// drawn is the screen as the UI draws it, row by row.
func drawn(u *UI) []string {
	scr := uv.NewScreenBuffer(u.width, u.height)
	u.Draw(scr, scr.Bounds())
	return screenRows(scr, scr.Bounds())
}

// at is the first point inside area of the screen showing text, or
// (-1, -1).
func at(rows []string, area image.Rectangle, text string) image.Point {
	for y := max(area.Min.Y, 0); y < min(area.Max.Y, len(rows)); y++ {
		cells := []rune(rows[y])
		seg := string(cells[min(area.Min.X, len(cells)):min(area.Max.X, len(cells))])
		if i := strings.Index(seg, text); i >= 0 {
			return image.Pt(area.Min.X+len([]rune(seg[:i])), y)
		}
	}
	return image.Pt(-1, -1)
}

func TestExplorer_CtrlXOpensItAndEscClosesIt(t *testing.T) {
	u := explorerUI(t, 200, 50)
	require.Equal(t, uiFocusEditor, u.focus)

	press(u, ctrlX)
	require.True(t, u.explorer.open)
	require.Equal(t, uiFocusExplorer, u.focus, "it takes the keys")
	require.True(t, u.explorer.following, "opened from a key, it follows the agent")
	require.GreaterOrEqual(t, at(drawn(u), u.layout.explorer, "Frames").Y, 0)

	press(u, esc)
	require.False(t, u.explorer.open)
	require.Equal(t, uiFocusEditor, u.focus, "focus goes back where it was")
	require.Equal(t, -1, at(drawn(u), u.layout.area, "Frames").Y)
}

func TestExplorer_CtrlXClosesItOrTakesFocusBack(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, tab)
	require.True(t, u.explorer.open)
	require.Equal(t, uiFocusEditor, u.focus, "tab hands the keys to the editor, the half view stays")

	press(u, ctrlX)
	require.True(t, u.explorer.open)
	require.Equal(t, uiFocusExplorer, u.focus, "ctrl+x takes them back")

	press(u, ctrlX)
	require.False(t, u.explorer.open, "and closes it once it has them")
}

// esc cancels a working agent; with the explorer open, esc closes the
// explorer and leaves the agent alone.
func TestExplorer_EscClosesItWithoutCancellingTheAgent(t *testing.T) {
	u := explorerUI(t, 200, 50)
	u.agentReady = true
	u.agentBusyCache.set(true)
	press(u, ctrlX, esc)

	require.False(t, u.explorer.open)
	require.False(t, u.isCanceling, "the agent's cancel is not armed")

	press(u, esc)
	require.True(t, u.isCanceling, "esc with the explorer closed arms it, as before")
}

func TestExplorer_HalfWhereItFitsFullElsewhere(t *testing.T) {
	u := explorerUI(t, 200, 50)
	edge := u.layout.sidebar.Max.X
	press(u, ctrlX)
	require.Equal(t, explorerHalf, u.explorerDrawn(), "opens half at 200 columns")
	require.Equal(t, edge, u.layout.explorer.Max.X, "it ends where the sidebar did")
	require.InDelta(t, u.layout.main.Dx(), u.layout.explorer.Dx(), 2, "and is as wide as the chat")
	require.Less(t, u.layout.main.Max.X, u.layout.explorer.Min.X, "the chat keeps the left half")

	u.width = 150
	u.updateLayoutAndSize()
	require.Equal(t, explorerFull, u.explorerDrawn(), "too narrow for half")
	require.True(t, u.layout.main.In(u.layout.explorer), "it covers the chat")
	require.True(t, u.layout.editor.In(u.layout.explorer), "and the editor")
	require.LessOrEqual(t, u.layout.explorer.Max.Y, u.layout.status.Min.Y, "not the status bar")

	u.width = 200
	u.updateLayoutAndSize()
	require.Equal(t, explorerHalf, u.explorerDrawn(), "half again once there is room")

	press(u, typed('+'))
	require.Equal(t, explorerFull, u.explorerDrawn())
	press(u, typed('-'))
	require.Equal(t, explorerHalf, u.explorerDrawn())

	u.width = 100
	u.updateLayoutAndSize()
	require.True(t, u.isCompact)
	require.Equal(t, explorerFull, u.explorerDrawn(), "compact mode draws it full")
}

func TestExplorer_SizeAndMovesLeaveTheTabAlone(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, typed('+'), typed('k'), typed('k'))

	require.Equal(t, explorerTree, u.explorer.tab)
	require.Equal(t, explorerFull, u.explorerDrawn())
}

func TestExplorer_TabsSwitchOnBracketsAndDigits(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX)
	require.Equal(t, explorerTree, u.explorer.tab)

	press(u, typed(']'))
	require.Equal(t, explorerPulse, u.explorer.tab)
	press(u, typed(']'), typed(']'))
	require.Equal(t, explorerTree, u.explorer.tab, "round again")
	press(u, typed('['))
	require.Equal(t, explorerContext, u.explorer.tab)
	press(u, typed('1'))
	require.Equal(t, explorerTree, u.explorer.tab)
	press(u, typed('2'))
	require.Equal(t, explorerPulse, u.explorer.tab)
}

func TestExplorer_FollowsTheAgentUntilMoved(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX)
	require.Equal(t, "fix-flaky-test/find-cause/try-free-port", u.selectedFrame().id)

	press(u, typed('k'))
	require.False(t, u.explorer.following)
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id)

	observeRows(u, toolRow("f6", 1_700_000_050, memResult(t, "fc6", push("fix-flaky-test/find-cause/try-free-port/rerun", "rerun the suite"))))
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id, "paused, the selection stays")
	require.GreaterOrEqual(t, at(drawn(u), u.layout.explorer, "1 new below").Y, 0)

	press(u, typed('G'))
	require.True(t, u.explorer.following)
	require.Equal(t, "fix-flaky-test/find-cause/try-free-port/rerun", u.selectedFrame().id)
}

func TestExplorer_TreeFoldsAndShowsTheSelectedFrame(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, typed('k'), typed('k'))
	require.Equal(t, "fix-flaky-test/reproduce-it", u.selectedFrame().id)

	rows := drawn(u)
	for _, want := range []string{"reproduce-it", "make it fail", "fails 3 times in 20", "−2K tok"} {
		require.GreaterOrEqual(t, at(rows, u.layout.explorer, want).Y, 0, "the detail shows %q", want)
	}

	press(u, typed('k'), typed('h'))
	require.Equal(t, "fix-flaky-test", u.selectedFrame().id)
	require.Equal(t, -1, at(drawn(u), u.layout.explorer, "try-free-port").Y, "folded, its frames are hidden")
	press(u, typed('l'))
	require.GreaterOrEqual(t, at(drawn(u), u.layout.explorer, "try-free-port").Y, 0)
}

func TestExplorer_OpensFromAClickOnTheSidebarStack(t *testing.T) {
	u := explorerUI(t, 200, 50)
	rows := drawn(u)
	heading := at(rows, u.layout.sidebar, "Stack ─")
	require.GreaterOrEqual(t, heading.Y, 0)

	click(u, heading.X+1, heading.Y)
	require.True(t, u.explorer.open)
	require.True(t, u.explorer.following, "the heading opens it following")
	press(u, esc)

	row := at(rows, u.layout.sidebar, "reproduce-it")
	click(u, row.X+1, row.Y)
	require.True(t, u.explorer.open)
	require.False(t, u.explorer.following, "a frame's row opens it on that frame")
	require.Equal(t, "fix-flaky-test/reproduce-it", u.selectedFrame().id)
}

func TestExplorer_OpensFromACompactTrailClick(t *testing.T) {
	u := explorerUI(t, 100, 40)
	require.True(t, u.isCompact)

	click(u, u.layout.stackTrail.Min.X+4, u.layout.stackTrail.Min.Y)

	require.True(t, u.explorer.open)
	require.Equal(t, explorerFull, u.explorerDrawn())
}

func TestExplorer_OpensFromAClickInTheDetailsColumn(t *testing.T) {
	u := explorerUI(t, 100, 40)
	press(u, tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	require.True(t, u.detailsOpen)
	row := at(drawn(u), u.layout.sessionDetails, "reproduce-it")
	require.GreaterOrEqual(t, row.Y, 0)

	click(u, row.X+1, row.Y)

	require.True(t, u.explorer.open)
	require.Equal(t, "fix-flaky-test/reproduce-it", u.selectedFrame().id)
	require.False(t, u.detailsOpen, "the panel it came from closes")
}

func TestExplorer_HeaderButtonsSizeAndCloseIt(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX)

	full := at(drawn(u), u.layout.explorer, "■ full")
	click(u, full.X+1, full.Y)
	require.Equal(t, explorerFull, u.explorerDrawn())

	half := at(drawn(u), u.layout.explorer, "◧ half")
	click(u, half.X+1, half.Y)
	require.Equal(t, explorerHalf, u.explorerDrawn())

	pulse := at(drawn(u), u.layout.explorer, "Pulse")
	click(u, pulse.X+1, pulse.Y)
	require.Equal(t, explorerPulse, u.explorer.tab, "a tab's name opens it")

	closing := at(drawn(u), u.layout.explorer, "× close")
	click(u, closing.X+1, closing.Y)
	require.False(t, u.explorer.open)
}

func TestExplorer_AClickOnAFrameSelectsIt(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX)
	frame := at(drawn(u), u.layout.explorer, "└ ▸ find-cause")
	require.GreaterOrEqual(t, frame.Y, 0)

	click(u, frame.X+5, frame.Y)

	require.False(t, u.explorer.following)
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id)
}

func TestExplorer_KeysShowInTheStatusBar(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX)

	var help []string
	for _, b := range u.ShortHelp() {
		help = append(help, b.Help().Key+" "+b.Help().Desc)
	}
	require.Contains(t, help, "esc close")
	require.Contains(t, help, "[/] tabs")
	require.Contains(t, help, "+/- full/half")
	require.Contains(t, help, "enter show in chat")
	require.Contains(t, help, "/ search")
}

func TestExplorer_IsNotOfferedWithoutFrames(t *testing.T) {
	u, _ := sidebarUI(t, 50)
	u.keyMap = DefaultKeyMap()
	u.width = 200
	u.updateLayoutAndSize()

	press(u, ctrlX)

	require.False(t, u.explorer.open)
}

// treeOf is the Tree tab's lines above its detail, each cut to the
// explorer.
func treeOf(u *UI) []string {
	rows := drawn(u)
	area := u.layout.explorer
	var tree []string
	for y := area.Min.Y + explorerBodyRow; y < area.Max.Y; y++ {
		cells := []rune(rows[y])
		line := string(cells[min(area.Min.X, len(cells)):min(area.Max.X, len(cells))])
		if strings.HasPrefix(strings.TrimSpace(line), "────") {
			break
		}
		tree = append(tree, line)
	}
	return tree
}

// lineWith is the first of lines holding text, or "".
func lineWith(lines []string, text string) string {
	for _, l := range lines {
		if strings.Contains(l, text) {
			return l
		}
	}
	return ""
}

// column is the column of text in line, in cells, or -1.
func column(line, text string) int {
	i := strings.Index(line, text)
	if i < 0 {
		return -1
	}
	return len([]rune(line[:i]))
}

func TestExplorer_TreeDrawsEachFrameOnATimeAxis(t *testing.T) {
	u := explorerUI(t, 200, 34)
	press(u, ctrlX, typed('+'))
	tree := treeOf(u)

	axis := lineWith(tree, "freed")
	require.Contains(t, axis, "0s", "the axis starts at the first push")
	require.Contains(t, axis, "10s")
	require.Contains(t, lineWith(tree, "▴"), "pop", "a row marks the pops on the axis")

	top, reproduce := lineWith(tree, "fix-flaky-test"), lineWith(tree, "reproduce-it")
	require.Greater(t, column(reproduce, "█"), column(top, "█"), "reproduce-it was pushed after its parent")
	require.Contains(t, reproduce, "−2K tok", "what its pop freed ends its line")
	cause := lineWith(tree, "find-cause")
	require.Contains(t, cause, "▶", "an open frame's bar runs to the end")
	require.Less(t, column(cause, "▶"), column(cause, time.Unix(1_700_000_030, 0).Format("15:04")), "then when it was entered")
}

func TestExplorer_TreeDropsTheBarsWhenNarrow(t *testing.T) {
	u := explorerUI(t, 48, 30)
	press(u, ctrlX)
	tree := strings.Join(treeOf(u), "\n")

	require.NotContains(t, tree, "█")
	require.NotContains(t, tree, "freed", "no axis either")
	require.Contains(t, tree, "reproduce-it")
	require.Contains(t, tree, "−2K tok")
}

// slidRows is explorerRows with try-free-port's first round slid out of the
// model's context, and a round after it.
func slidRows(t *testing.T) []message.Message {
	t.Helper()
	mark := "slide:fix-flaky-test/find-cause/try-free-port:fc5:fc7"
	first, answer := callRow("e6", 1_700_000_050, toolCall("v1", "view", `{"file_path":"a.go"}`)), toolRow("f6", 1_700_000_050, viewResult("v1"))
	first.Popped, answer.Popped = mark, mark
	return append(explorerRows(t), first, answer,
		callRow("e7", 1_700_000_060, toolCall("v2", "view", `{"file_path":"b.go"}`)),
		toolRow("f7", 1_700_000_060, viewResult("v2")),
	)
}

func TestExplorer_TreeShowsASlideUnderTheFrameItSlid(t *testing.T) {
	u := explorerUI(t, 200, 34)
	observeRows(u, slidRows(t)[len(explorerRows(t)):]...)
	press(u, ctrlX, typed('+'))
	tree := treeOf(u)

	slide := slices.IndexFunc(tree, func(l string) bool { return strings.Contains(l, "⟲ slid · 2 rows") })
	require.Greater(t, slide, slices.IndexFunc(tree, func(l string) bool { return strings.Contains(l, "try-free-port") }))
	require.Contains(t, tree[slide], "−1K tok")
	require.Contains(t, lineWith(tree, "▴"), "⟲", "the axis marks it too")

	press(u, typed('j'))
	require.Equal(t, "fix-flaky-test/find-cause/try-free-port", u.selectedFrame().id, "the selection skips it")
}

func TestExplorer_TreeShowsTheContextOverTimeWhenTall(t *testing.T) {
	tall := explorerUI(t, 200, 50)
	press(tall, ctrlX, typed('+'))
	require.NotEmpty(t, lineWith(treeOf(tall), "model context"))

	short := explorerUI(t, 200, 34)
	press(short, ctrlX, typed('+'))
	require.Empty(t, lineWith(treeOf(short), "model context"), "a short terminal drops the chart")
}

func TestContextChartScalesToItsHeight(t *testing.T) {
	series := []contextPoint{{at: 10, tokens: 1000}, {at: 20, tokens: 2000}, {at: 30, tokens: 500}}

	require.Equal(t, []string{" █ ", "██▄"}, contextChart(series, 10, 30, 3, 2, 2000))
	require.Equal(t, []string{"   ██ ", " ████▄"}, contextChart(series, 5, 30, 6, 2, 2000), "a column shows the last point at or before its time")
}

var enter = tea.KeyPressMsg{Code: tea.KeyEnter}

func TestExplorer_SearchSelectsTheFirstFrameMatching(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, typed('/'))
	for _, r := range "WHY" {
		press(u, typed(r))
	}
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id, "its purpose matches, in any case")
	require.GreaterOrEqual(t, at(drawn(u), u.layout.explorer, "/WHY").Y, 0, "the query shows while it is typed")

	press(u, enter)
	require.True(t, u.explorer.open)
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id)
	require.False(t, u.explorer.following)
	require.Equal(t, -1, at(drawn(u), u.layout.explorer, "/WHY").Y, "enter ends the search")

	press(u, typed('/'), typed('m'), typed('a'), typed('k'))
	require.Equal(t, "fix-flaky-test/reproduce-it", u.selectedFrame().id)
	press(u, esc)
	require.True(t, u.explorer.open, "esc ends the search, not the explorer")
	require.Equal(t, "fix-flaky-test/find-cause", u.selectedFrame().id, "and puts the selection back")
}

func TestExplorer_SearchOpensTheFramesAboveItsMatch(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, typed('k'), typed('k'), typed('k'), typed('h'))
	require.Equal(t, "fix-flaky-test", u.selectedFrame().id)
	require.Empty(t, lineWith(treeOf(u), "try-free-port"))

	press(u, typed('/'), typed('p'), typed('o'), typed('r'), typed('t'))

	require.Equal(t, "fix-flaky-test/find-cause/try-free-port", u.selectedFrame().id)
	require.NotEmpty(t, lineWith(treeOf(u), "try-free-port"))
}

func TestExplorer_EnterShowsTheFrameInTheChat(t *testing.T) {
	u := explorerUI(t, 200, 50)
	press(u, ctrlX, typed('k'), typed('k'))
	require.Equal(t, "fix-flaky-test/reproduce-it", u.selectedFrame().id)

	press(u, enter)

	require.True(t, u.explorer.open, "beside the chat, it stays")
	require.True(t, u.chat.framed["f3"], "the frame's rows are marked")
	require.True(t, u.chat.framed["fc4"], "and the call that popped it")
	require.False(t, u.chat.framed["fc2"], "not the one that pushed it")
	top, _ := u.chat.ScrollPosition()
	require.Equal(t, u.chat.idInxMap["fc2"], top, "the chat starts at its push")
	require.GreaterOrEqual(t, at(drawn(u), u.layout.main, "▌").Y, 0, "the marked rows have a gutter")

	press(u, typed('+'), enter)
	require.True(t, u.explorer.open)
	require.Equal(t, explorerHalf, u.explorerDrawn(), "full drops to half, where half fits")

	narrow := explorerUI(t, 150, 50)
	press(narrow, ctrlX, typed('k'), enter)
	require.False(t, narrow.explorer.open, "where it does not, it closes")
	require.True(t, narrow.chat.framed["e5"])

	press(narrow, ctrlX, esc)
	require.Empty(t, narrow.chat.framed, "closing the explorer clears the marks")
}
