package model

import (
	"fmt"
	"image"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ---- State

// explorerSize is how much of the terminal the frame explorer takes.
type explorerSize int

const (
	explorerHalf explorerSize = iota
	explorerFull
)

// explorerTab is a view of the explorer.
type explorerTab int

const (
	explorerTree explorerTab = iota
	explorerPulse
	explorerContext
	explorerTabCount
)

var explorerTabNames = [explorerTabCount]string{"Tree", "Pulse", "Context"}

// explorerHalfWidth is the narrowest terminal the explorer takes half of,
// each side keeping about 80 columns.
const explorerHalfWidth = 160

// explorer is the frame explorer: the session's frames over half of the
// terminal or all of it.
type explorer struct {
	open bool
	// want is the size asked for; a terminal too small for half draws it
	// full.
	want explorerSize
	tab  explorerTab
	// following is whether the selection is the frame the agent is in;
	// otherwise selected is the entry selected, by key, and seen how many
	// entries there were when following stopped.
	following bool
	selected  string
	seen      int
	// flipped is the entries folded or open against their default: a
	// finished top-level task folded, any other open.
	flipped map[string]bool
	// searching is whether / reads a query; query is what it read, and
	// from the selection to put back when the search is dropped.
	searching bool
	query     string
	from      explorerSelection
	// back is the focus to give back on closing.
	back uiFocusState
}

// explorerSelection is what the explorer selects: the agent's frame, or
// the entry selected.
type explorerSelection struct {
	following bool
	selected  string
}

// halfFits reports whether the terminal keeps two sides of about 80
// columns outside compact mode.
func (m *UI) halfFits() bool {
	return !m.isCompact && m.width >= explorerHalfWidth
}

// explorerDrawn is the size the explorer is drawn at: half where asked
// and where half fits, else full.
func (m *UI) explorerDrawn() explorerSize {
	if m.explorer.want == explorerHalf && m.halfFits() {
		return explorerHalf
	}
	return explorerFull
}

// explorerBeside reports whether the explorer is open beside the chat.
func (m *UI) explorerBeside() bool {
	return m.explorer.open && m.explorerDrawn() == explorerHalf
}

// frameEntries is how many frame entries the session has.
func (m *UI) frameEntries() int {
	roots, _ := m.sessionFrames.tree()
	n := 0
	eachFrame(roots, func(*frameNode) { n++ })
	return n
}

// openExplorer opens the explorer on the entry key, or following the
// agent for "", and gives it the keys.
func (m *UI) openExplorer(key string) {
	m.explorer.open = true
	m.explorer.searching = false
	m.explorer.following = key == ""
	m.explorer.selected = key
	m.explorer.seen = m.frameEntries()
	m.focusExplorer()
	m.updateLayoutAndSize()
}

// focusExplorer gives the explorer the keys, remembering who had them.
func (m *UI) focusExplorer() {
	if m.focus != uiFocusExplorer {
		m.explorer.back = m.focus
	}
	m.focus = uiFocusExplorer
	m.textarea.Blur()
	m.chat.Blur()
}

// closeExplorer closes the explorer, clears the marks it left in the
// chat, and gives the keys back.
func (m *UI) closeExplorer() tea.Cmd {
	m.chat.ShowFrame(nil)
	return m.hideExplorer()
}

// hideExplorer closes the explorer, leaving its marks in the chat, and
// gives the keys back.
func (m *UI) hideExplorer() tea.Cmd {
	m.explorer.open = false
	var cmd tea.Cmd
	if m.focus == uiFocusExplorer {
		if m.explorer.back == uiFocusMain {
			m.focus = uiFocusMain
			m.chat.Focus()
		} else {
			m.focus = uiFocusEditor
			cmd = m.textarea.Focus()
		}
	}
	m.updateLayoutAndSize()
	return cmd
}

// toggleExplorer is ctrl+x: it opens the explorer, gives it the keys back,
// or closes it once it has them. A session without frames has nothing to
// open.
func (m *UI) toggleExplorer() (tea.Cmd, bool) {
	switch {
	case !m.explorer.open:
		if m.frameEntries() == 0 {
			return nil, false
		}
		m.openExplorer("")
	case m.focus != uiFocusExplorer:
		m.focusExplorer()
	default:
		return m.closeExplorer(), true
	}
	return nil, true
}

// ---- Selection

// explorerRow is a line of the explorer's tree: a frame entry, or a slide
// under the frame it slid.
type explorerRow struct {
	node   *frameNode
	slide  *frameSlide
	parent string
	// lead is the tree's branches drawn before the entry.
	lead  string
	depth int
}

// name is what the row says after its marker.
func (r explorerRow) name() string {
	if r.slide != nil {
		return "slid · " + plural(r.slide.rows, "row")
	}
	return shortName(r.node.id, r.parent)
}

// folds reports whether n's frames are hidden at depth: a finished
// top-level task folds by default, the rest are open, and the user flips
// either.
func (e explorer) folds(n *frameNode, depth int) bool {
	return (depth == 0 && !n.open()) != e.flipped[n.key]
}

// rows is the tree's lines, as folded. A slide stands under the last
// entry of the frame it slid pushed before it, among that entry's frames
// by time; at the top where there is none.
func (e explorer) rows(roots []*frameNode, slides []frameSlide) []explorerRow {
	owned := map[*frameNode][]*frameSlide{}
	var top []*frameSlide
	for i := range slides {
		s := &slides[i]
		var owner *frameNode
		eachFrame(roots, func(n *frameNode) {
			if n.id == s.frame && n.since <= s.at {
				owner = n
			}
		})
		if owner == nil {
			top = append(top, s)
		} else {
			owned[owner] = append(owned[owner], s)
		}
	}
	var rows []explorerRow
	var walk func(nodes []*frameNode, slides []*frameSlide, parent, cont string, depth int)
	walk = func(nodes []*frameNode, slides []*frameSlide, parent, cont string, depth int) {
		var items []explorerRow
		for _, n := range nodes {
			for len(slides) > 0 && slides[0].at < n.since {
				items, slides = append(items, explorerRow{slide: slides[0]}), slides[1:]
			}
			items = append(items, explorerRow{node: n})
		}
		for _, s := range slides {
			items = append(items, explorerRow{slide: s})
		}
		for i, item := range items {
			lead, next := "", ""
			if depth > 0 {
				lead, next = cont+"├ ", cont+"│ "
				if i == len(items)-1 {
					lead, next = cont+"└ ", cont+"  "
				}
			}
			item.parent, item.lead, item.depth = parent, lead, depth
			rows = append(rows, item)
			if n := item.node; n != nil && !e.folds(n, depth) {
				walk(n.children, owned[n], n.id, next, depth+1)
			}
		}
	}
	walk(roots, top, "", "", 0)
	return rows
}

// treeRows is the explorer's tree lines for the session's frames.
func (m *UI) treeRows() []explorerRow {
	roots, _ := m.sessionFrames.tree()
	return m.explorer.rows(roots, m.sessionFrames.slides())
}

// followed is the entry the agent is in: the top of the stack, or with
// none open, the last one entered; nil without frames.
func followed(roots, stack []*frameNode) *frameNode {
	if len(stack) > 0 {
		return stack[len(stack)-1]
	}
	var last *frameNode
	eachFrame(roots, func(n *frameNode) { last = n })
	return last
}

// holds reports whether the entry key is n or entered from it.
func holds(n *frameNode, key string) bool {
	return n.key == key || slices.ContainsFunc(n.children, func(c *frameNode) bool { return holds(c, key) })
}

// selectedFrame is the entry the explorer shows; nil without frames.
func (m *UI) selectedFrame() *frameNode {
	roots, stack := m.sessionFrames.tree()
	if !m.explorer.following {
		var found *frameNode
		eachFrame(roots, func(n *frameNode) {
			if n.key == m.explorer.selected {
				found = n
			}
		})
		if found != nil {
			return found
		}
	}
	return followed(roots, stack)
}

// selectedRow is where the selection is among rows, or its nearest shown
// frame when it is folded away.
func (m *UI) selectedRow(rows []explorerRow) int {
	sel := m.selectedFrame()
	if sel == nil {
		return 0
	}
	at := slices.IndexFunc(rows, func(r explorerRow) bool { return r.node != nil })
	for i, r := range rows {
		if r.node == nil {
			continue
		}
		if r.node.key == sel.key {
			return i
		}
		if holds(r.node, sel.key) {
			at = i
		}
	}
	return at
}

// pauseExplorer selects the entry key and stops following the agent.
func (m *UI) pauseExplorer(key string) {
	if m.explorer.following {
		m.explorer.seen = m.frameEntries()
	}
	m.explorer.following = false
	m.explorer.selected = key
}

// moveExplorer moves the selection by delta frames, over the slides.
func (m *UI) moveExplorer(delta int) {
	rows := m.treeRows()
	if len(rows) == 0 {
		return
	}
	i, step := m.selectedRow(rows), 1
	if delta < 0 {
		delta, step = -delta, -1
	}
	for range delta {
		j := i + step
		for j >= 0 && j < len(rows) && rows[j].node == nil {
			j += step
		}
		if j < 0 || j >= len(rows) {
			break
		}
		i = j
	}
	m.pauseExplorer(rows[i].node.key)
}

// foldExplorer folds or opens the selected entry's frames.
func (m *UI) foldExplorer(fold bool) {
	rows := m.treeRows()
	if len(rows) == 0 {
		return
	}
	r := rows[m.selectedRow(rows)]
	if len(r.node.children) == 0 || m.explorer.folds(r.node, r.depth) == fold {
		return
	}
	m.explorer.flip(r.node)
	m.pauseExplorer(r.node.key)
}

// flip folds n's frames if they show, else opens them.
func (e *explorer) flip(n *frameNode) {
	if e.flipped == nil {
		e.flipped = map[string]bool{}
	}
	e.flipped[n.key] = !e.flipped[n.key]
}

// ---- Search

// matches reports whether n's id or purpose holds the query q, lowered.
func matches(n *frameNode, q string) bool {
	return strings.Contains(strings.ToLower(n.id), q) || strings.Contains(strings.ToLower(n.purpose), q)
}

// searchKey reads a key into the search: text adds to the query,
// backspace takes from it, each selecting the first frame it matches;
// enter keeps the selection, esc puts back the one from before.
func (m *UI) searchKey(msg tea.KeyPressMsg) {
	e := &m.explorer
	switch {
	case key.Matches(msg, m.keyMap.Explorer.Close):
		e.searching = false
		e.following, e.selected = e.from.following, e.from.selected
		return
	case key.Matches(msg, m.keyMap.Explorer.Show):
		e.searching = false
		return
	case msg.Code == tea.KeyBackspace:
		r := []rune(e.query)
		e.query = string(r[:max(len(r)-1, 0)])
	case msg.Text != "":
		e.query += msg.Text
	default:
		return
	}
	m.findFrame()
}

// findFrame selects the first frame the query matches, in any case, and
// opens the frames above it; with no query or no match, the selection
// from before the search.
func (m *UI) findFrame() {
	e := &m.explorer
	e.following, e.selected = e.from.following, e.from.selected
	q := strings.ToLower(e.query)
	if q == "" {
		return
	}
	roots, _ := m.sessionFrames.tree()
	var find func(nodes []*frameNode, depth int) *frameNode
	find = func(nodes []*frameNode, depth int) *frameNode {
		for _, n := range nodes {
			if matches(n, q) {
				return n
			}
			if found := find(n.children, depth+1); found != nil {
				if e.folds(n, depth) {
					e.flip(n)
				}
				return found
			}
		}
		return nil
	}
	if found := find(roots, 0); found != nil {
		m.pauseExplorer(found.key)
	}
}

// showFrameInChat shows the selected frame's rows in the chat: the chat
// starts at its push and marks its rows and its pop with a gutter. A full
// explorer makes room: it turns half where half fits, else it closes,
// leaving the marks and the keys to the chat.
func (m *UI) showFrameInChat() tea.Cmd {
	n := m.selectedFrame()
	if n == nil {
		return nil
	}
	var ids []string
	for _, r := range m.sessionFrames.frameRows(n) {
		ids = append(ids, poppedItemIDs(r)...)
	}
	if n.popCall != "" {
		ids = append(ids, n.popCall)
	}
	m.chat.ShowFrame(ids)
	m.chat.ScrollToItem(n.pushCall)
	switch {
	case m.explorerDrawn() == explorerHalf:
		return nil
	case m.halfFits():
		m.explorer.want = explorerHalf
		m.updateLayoutAndSize()
		return nil
	}
	m.explorer.back = uiFocusMain
	return m.hideExplorer()
}

// ---- Input

// handleExplorerKey handles a key while the explorer has focus, and
// reports false for one it leaves to the global keys.
func (m *UI) handleExplorerKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k, e := m.keyMap.Explorer, &m.explorer
	if e.searching && !key.Matches(msg, m.keyMap.Chat.Frames) {
		m.searchKey(msg)
		return nil, true
	}
	switch {
	case key.Matches(msg, k.Close), key.Matches(msg, m.keyMap.Chat.Frames):
		return m.closeExplorer(), true
	case key.Matches(msg, k.Search):
		e.searching, e.query, e.from = true, "", explorerSelection{following: e.following, selected: e.selected}
	case key.Matches(msg, k.Show):
		return m.showFrameInChat(), true
	case key.Matches(msg, k.Full):
		e.want = explorerFull
		m.updateLayoutAndSize()
	case key.Matches(msg, k.Half):
		e.want = explorerHalf
		m.updateLayoutAndSize()
	case key.Matches(msg, k.NextTab):
		e.tab = (e.tab + 1) % explorerTabCount
	case key.Matches(msg, k.PrevTab):
		e.tab = (e.tab + explorerTabCount - 1) % explorerTabCount
	case key.Matches(msg, k.Tree):
		e.tab = explorerTree
	case key.Matches(msg, k.Pulse):
		e.tab = explorerPulse
	case key.Matches(msg, k.Context):
		e.tab = explorerContext
	case key.Matches(msg, k.Up):
		m.moveExplorer(-1)
	case key.Matches(msg, k.Down):
		m.moveExplorer(1)
	case key.Matches(msg, k.Fold):
		m.foldExplorer(true)
	case key.Matches(msg, k.Unfold):
		m.foldExplorer(false)
	case key.Matches(msg, k.Follow):
		e.following, e.selected = true, ""
	case key.Matches(msg, k.Focus):
		if m.explorerBeside() {
			m.focus = uiFocusEditor
			return m.textarea.Focus(), true
		}
	default:
		return nil, false
	}
	return nil, true
}

// handleExplorerClick handles a click on the explorer: a header button, a
// tab's name, a frame of the tree. It gives the explorer the keys.
func (m *UI) handleExplorerClick(msg tea.MouseClickMsg) tea.Cmd {
	area := m.layout.explorer
	x, y := msg.X-area.Min.X, msg.Y-area.Min.Y
	m.focusExplorer()
	switch {
	case y == explorerHeaderRow:
		switch explorerButtonAt(area.Dx(), x) {
		case buttonHalf:
			m.explorer.want = explorerHalf
			m.updateLayoutAndSize()
		case buttonFull:
			m.explorer.want = explorerFull
			m.updateLayoutAndSize()
		case buttonClose:
			return m.closeExplorer()
		}
	case y == explorerTabsRow:
		if tab := explorerTabAt(x); tab >= 0 {
			m.explorer.tab = tab
		}
	case m.explorer.tab == explorerTree:
		rows := m.treeRows()
		l := treeLayoutFor(rows, explorerInner(area.Dx()), area.Dy())
		first := explorerBodyRow + l.head
		i := explorerTreeTop(m.selectedRow(rows), l.tree) + y - first
		if y >= first && y < first+l.tree && i < len(rows) && rows[i].node != nil {
			m.pauseExplorer(rows[i].node.key)
		}
	}
	return nil
}

// ---- Drawing

const (
	explorerHeaderRow = iota
	explorerTabsRow
	explorerRuleRow
	explorerBodyRow
)

// explorerPad is the room between the explorer's edges and its content.
const explorerPad = 1

// explorerInner is the width of an explorer's content.
func explorerInner(width int) int {
	return max(width-2*explorerPad, 1)
}

const (
	buttonHalf = iota
	buttonFull
	buttonClose
)

var explorerButtons = [...]string{buttonHalf: "◧ half", buttonFull: "■ full", buttonClose: "× close"}

// explorerButtonAt is the header button over column x of an explorer
// width wide, or -1. The buttons end the header, two columns apart.
func explorerButtonAt(width, x int) int {
	right := width - explorerPad
	for i := len(explorerButtons) - 1; i >= 0; i-- {
		left := right - lipgloss.Width(explorerButtons[i])
		if x >= left && x < right {
			return i
		}
		right = left - 2
	}
	return -1
}

// explorerTabAt is the tab whose name is over column x, or -1. The names
// follow each other three columns apart.
func explorerTabAt(x int) explorerTab {
	left := explorerPad
	for i, name := range explorerTabNames {
		right := left + lipgloss.Width(name)
		if x >= left && x < right {
			return explorerTab(i)
		}
		left = right + 3
	}
	return -1
}

// explorerTreeHeights is how many rows the Tree tab gives its tree and its
// detail pane in an explorer height rows tall. A short one trims the
// detail; a very short one has none.
func explorerTreeHeights(height int) (tree, detail int) {
	body := height - explorerBodyRow
	switch {
	case body >= 24:
		detail = 9
	case body >= 10:
		detail = 4
	}
	if detail == 0 {
		return max(body, 0), 0
	}
	return body - detail - 1, detail
}

// explorerTreeTop is the first tree row in view for the selection at row
// sel to show in tree rows.
func explorerTreeTop(sel, tree int) int {
	return max(sel-tree+1, 0)
}

// drawExplorer draws the explorer over area, the selected frame's row
// lit.
func (m *UI) drawExplorer(scr uv.Screen, area uv.Rectangle) {
	view, lit := m.explorerView(area.Dx(), area.Dy())
	uv.NewStyledString(view).Draw(scr, area)
	if lit < 0 {
		return
	}
	bg := m.com.Styles.Explorer.Selected.GetBackground()
	for x := area.Min.X; x < area.Max.X; x++ {
		if c := scr.CellAt(x, area.Min.Y+lit); c != nil {
			cell := *c
			cell.Style.Bg = bg
			scr.SetCell(x, area.Min.Y+lit, &cell)
		}
	}
}

// explorerView renders the explorer width × height, and says which of its
// rows is the selected frame's, or -1.
func (m *UI) explorerView(width, height int) (string, int) {
	t := m.com.Styles
	inner := explorerInner(width)
	lines := []string{
		m.explorerHeader(inner),
		m.explorerTabs(),
		t.Files.TruncationHint.Render(strings.Repeat("─", inner)),
	}
	lit := -1
	switch m.explorer.tab {
	case explorerTree:
		var body []string
		body, lit = m.explorerTree(inner, height)
		lines = append(lines, body...)
	default:
		lines = append(lines, "", t.Files.EmptyMessage.Render("not built yet"))
	}
	if lit >= 0 {
		lit += explorerBodyRow
	}
	pad := strings.Repeat(" ", explorerPad)
	out := make([]string, height)
	for i := range out {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "…")
		}
		out[i] = pad + line + strings.Repeat(" ", max(width-explorerPad-lipgloss.Width(line), 0))
	}
	return strings.Join(out, "\n"), lit
}

// explorerHeader is the explorer's first line, inner wide: what the
// session's frames freed, whether it follows the agent, and the size and
// close buttons at its end.
func (m *UI) explorerHeader(inner int) string {
	t := m.com.Styles
	roots, _ := m.sessionFrames.tree()
	pops := 0
	eachFrame(roots, func(n *frameNode) {
		if n.popped {
			pops++
		}
	})
	slides := map[string]bool{}
	for _, r := range m.sessionFrames.cut {
		if strings.HasPrefix(r.mark, slideMarkPrefix) {
			slides[r.mark] = true
		}
	}
	stats := []string{plural(pops, "pop"), plural(len(slides), "slide")}
	if freed := freedLabel(t, m.sessionFrames); freed != "" {
		stats = append([]string{freed}, stats...)
	}
	// state says whether the explorer follows the agent; brief drops the
	// hint for a narrow header.
	state := t.Files.Additions.Render("● following")
	brief := state
	if !m.explorer.following {
		brief = t.LSP.WarningDiagnostic.Render("◌ paused")
		if n := m.frameEntries() - m.explorer.seen; n > 0 {
			brief += t.Files.EmptyMessage.Render(fmt.Sprintf(" · %d new below", n))
		}
		state = brief + t.Files.EmptyMessage.Render(" · G to follow")
	}
	drawn := map[explorerSize]int{explorerHalf: buttonHalf, explorerFull: buttonFull}[m.explorerDrawn()]
	var buttons []string
	for i, b := range explorerButtons {
		style := t.Files.EmptyMessage
		if i == drawn {
			style = t.Files.ArtifactLabel.Underline(true)
		}
		buttons = append(buttons, style.Render(b))
	}
	right := strings.Join(buttons, "  ")

	title := ""
	if m.session != nil {
		title = t.Files.EmptyMessage.Render(m.session.Title)
	}
	sep := t.Files.TruncationHint.Render(" · ")
	head := lipgloss.NewStyle().Bold(true).Render("Frames")
	for _, left := range []string{
		head + "  " + title + "  " + strings.Join(stats, sep) + "  " + state,
		head + "  " + strings.Join(stats, sep) + "  " + state,
		head + "  " + strings.Join(stats, sep) + "  " + brief,
		head + "  " + brief,
		head,
	} {
		if gap := inner - lipgloss.Width(left) - lipgloss.Width(right); gap >= 2 {
			return left + strings.Repeat(" ", gap) + right
		}
	}
	return right
}

// plural writes n things: "1 pop", "3 pops".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

// explorerTabs is the tabs' names, the open one underlined, then the
// search's query while / reads one.
func (m *UI) explorerTabs() string {
	t := m.com.Styles
	names := make([]string, len(explorerTabNames))
	for i, name := range explorerTabNames {
		style := t.Files.EmptyMessage
		if explorerTab(i) == m.explorer.tab {
			style = t.Files.ArtifactLabel.Underline(true)
		}
		names[i] = style.Render(name)
	}
	line := strings.Join(names, "   ")
	if !m.explorer.searching {
		return line
	}
	q := strings.ToLower(m.explorer.query)
	hint := "  enter keeps it · esc goes back"
	if q != "" {
		roots, _ := m.sessionFrames.tree()
		found := false
		eachFrame(roots, func(n *frameNode) { found = found || matches(n, q) })
		if !found {
			hint = "  no match · esc goes back"
		}
	}
	cursor := lipgloss.NewStyle().Foreground(t.Header.LogoGradToColor).Render("▏")
	return line + "      " + t.Files.ArtifactLabel.Render("/"+m.explorer.query) + cursor + t.Files.EmptyMessage.Render(hint)
}

// explorerTree is the Tree tab's lines, inner wide, for an explorer height
// rows tall: where there is room, the context chart and the time axis;
// the tree in view around the selection, each frame on the axis where
// there is room; then the selected frame's detail; and the row of the
// selection among the lines.
func (m *UI) explorerTree(inner, height int) ([]string, int) {
	t := m.com.Styles
	roots, stack := m.sessionFrames.tree()
	slides := m.sessionFrames.slides()
	rows := m.explorer.rows(roots, slides)
	if len(rows) == 0 {
		return nil, -1
	}
	l := treeLayoutFor(rows, inner, height)
	sel := m.selectedRow(rows)
	top := explorerTreeTop(sel, l.tree)
	look := stackLook{now: time.Now(), busy: m.isAgentBusy()}
	r := stackRenderer{t: t, width: inner - treeGutter, look: look}
	r.freedOf = func(n *frameNode) int64 { return tokensOf(m.sessionFrames.heldRows(n)) }
	if len(stack) > 0 {
		r.top = stack[len(stack)-1]
	}
	axis := m.sessionFrames.treeAxis(roots, look, l.cols.bars)
	lines := m.treeHead(l, axis, roots, slides)
	gutter := lipgloss.NewStyle().Foreground(t.Header.LogoGradToColor)
	for i := top; i < min(top+l.tree, len(rows)); i++ {
		mark := "  "
		if i == sel {
			mark = gutter.Render("›") + " "
		}
		lines = append(lines, mark+r.treeLine(rows[i], l.cols, axis, inner-treeGutter))
	}
	lit := l.head + sel - top
	if l.detail == 0 {
		return lines, lit
	}
	for len(lines) < l.head+l.tree {
		lines = append(lines, "")
	}
	lines = append(lines, t.Files.TruncationHint.Render(strings.Repeat("─", inner)))
	return append(lines, m.detailPane(rows[sel].node, &r, inner, l.detail)...), lit
}

// listBesideWidth is the narrowest explorer content that lists what a
// frame held beside its detail.
const listBesideWidth = 120

// detailPane is the detail pane's lines for n, inner wide and at most
// height tall: its detail, and where there is room, beside it, what it
// held.
func (m *UI) detailPane(n *frameNode, r *stackRenderer, inner, height int) []string {
	detail := m.frameDetail(n, r)
	if inner < listBesideWidth {
		return detail[:min(height, len(detail))]
	}
	left := inner / 2
	held := m.heldList(n, inner-left-2, height)
	lines := make([]string, min(height, max(len(detail), len(held))))
	for i := range lines {
		d, h := "", ""
		if i < len(detail) {
			d = detail[i]
		}
		if i < len(held) {
			h = held[i]
		}
		lines[i] = fitLeft(d, left) + "  " + h
	}
	return lines
}

// heldList is, width wide and at most height lines, what the model no
// longer has from n once it is popped, or what it holds from it while it
// is open: a line per chat item showing those rows, the first the chat
// draws for it.
func (m *UI) heldList(n *frameNode, width, height int) []string {
	t := m.com.Styles
	held := m.sessionFrames.heldRows(n)
	title := "what the model no longer has · "
	if !n.popped {
		title = "what the model holds · "
	}
	lines := []string{t.Files.TruncationHint.Render(title + plural(len(held), "row"))}
	var items []string
	for _, row := range held {
		if row.Role == message.Tool {
			continue
		}
		ids := []string{row.ID}
		for _, c := range row.ToolCalls() {
			ids = append(ids, c.ID)
		}
		for _, id := range ids {
			if line := m.chatLine(id); line != "" {
				items = append(items, line)
			}
		}
	}
	if room := max(height-2, 1); len(items) > room {
		items = append(items[:room-1], fmt.Sprintf("… %d more", len(items)-room+1))
	}
	for _, item := range items {
		lines = append(lines, "  "+t.Files.EmptyMessage.Render(ansi.Truncate(item, width-2, "…")))
	}
	if len(held) > 0 {
		lines = append(lines, t.Files.TruncationHint.Render("enter: show these rows in the chat"))
	}
	return lines
}

// chatLine is the first line the chat draws for its item id, without its
// colors; "" where the chat has no such item or it draws nothing.
func (m *UI) chatLine(id string) string {
	item := m.chat.MessageItem(id)
	if item == nil {
		return ""
	}
	for line := range strings.SplitSeq(item.Render(max(m.chat.list.Width(), 1)), "\n") {
		if s := strings.TrimSpace(ansi.Strip(line)); s != "" {
			return s
		}
	}
	return ""
}

// frameDetail is the detail pane's lines for n, the most needed first:
// its name and id, purpose, result, what it freed or holds, when it ran,
// its work, its edits, its thoughts.
func (m *UI) frameDetail(n *frameNode, r *stackRenderer) []string {
	t := m.com.Styles
	label := func(s string) string { return t.Files.TruncationHint.Render(fmt.Sprintf("%-10s", s)) }
	value := t.Files.ArtifactLabel
	muted := t.Files.EmptyMessage
	rows := m.sessionFrames.frameRows(n)
	held := m.sessionFrames.heldRows(n)
	tokens := tokensOf(held)

	marker := t.Files.Additions.Render("✓")
	if n.open() {
		marker = t.LSP.WarningDiagnostic.Render("▸")
	}
	lines := []string{
		marker + " " + value.Bold(true).Render(shortName(n.id, parentID(n.id))) + "   " + muted.Render(n.id),
		label("purpose") + value.Render(n.purpose),
	}
	if n.popped {
		result := n.result
		if result == "" {
			result = "nothing said"
		}
		lines = append(lines, label("result")+value.Render(result))
		freed := muted.Render("nothing: nothing came between its push and its pop")
		if len(held) > 0 {
			freed = t.Files.Additions.Render("−"+shortTokens(tokens)+" tok") +
				muted.Render(fmt.Sprintf(" · %s out of the model's context", plural(len(held), "row")))
		}
		lines = append(lines, label("freed")+freed)
		lines = append(lines, label("ran")+value.Render(fmt.Sprintf("%s → %s · %s",
			time.Unix(n.since, 0).Format("15:04"), time.Unix(n.until, 0).Format("15:04"), runningFor(time.Duration(n.until-n.since)*time.Second))))
	} else {
		lines = append(lines, label("result")+muted.Render("not popped yet"))
		lines = append(lines, label("holds")+value.Render(fmt.Sprintf("≈%s tok", shortTokens(tokens)))+muted.Render(fmt.Sprintf(" in %s", plural(len(held), "row"))))
		ran := "since " + time.Unix(n.since, 0).Format("15:04")
		if r.working() {
			ran += " · " + runningFor(r.look.now.Sub(time.Unix(n.since, 0)))
		}
		lines = append(lines, label("ran")+value.Render(ran))
	}
	var work []string
	for _, u := range frameWork(rows) {
		work = append(work, fmt.Sprintf("%s %d", u.name, u.n))
	}
	if len(work) == 0 {
		lines = append(lines, label("work")+muted.Render("nothing yet"))
	} else {
		lines = append(lines, label("work")+value.Render(strings.Join(work, " · ")))
	}
	if edits := frameEdits(rows, m.com.Workspace.WorkingDir()); len(edits) > 0 {
		lines = append(lines, label("edited")+value.Render(strings.Join(edits, ", ")))
	} else {
		lines = append(lines, label("edited")+muted.Render("nothing"))
	}
	if n.thoughts > 0 {
		lines = append(lines, label("thoughts")+value.Render(plural(n.thoughts, "thought")+" kept"))
	}
	return lines
}

// parentID is the id of the frame id is under, "" for a top-level one.
func parentID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[:i]
	}
	return ""
}

// explorerAt reports whether the point is on the open explorer.
func (m *UI) explorerAt(x, y int) bool {
	return m.explorer.open && image.Pt(x, y).In(m.layout.explorer)
}

// stackLines is where the Stack's lines start in a view, and the frame
// entry each line shows, "" for a line that shows none.
type stackLines struct {
	first int
	keys  []string
}

// at is the entry line shows, if it is one of the Stack's.
func (s stackLines) at(line int) (string, bool) {
	if i := line - s.first; i >= 0 && i < len(s.keys) {
		return s.keys[i], true
	}
	return "", false
}

// stackFrameAt is the frame entry the Stack shows at the screen point x,
// y, "" for the Stack but no entry, with false where no Stack is: the
// sidebar's section, compact mode's trail, or the ctrl+d column.
func (m *UI) stackFrameAt(x, y int) (string, bool) {
	if m.frameEntries() == 0 {
		return "", false
	}
	p := image.Pt(x, y)
	switch {
	case m.isCompact && m.detailsOpen:
		details := m.sessionDetailsIn(m.layout.sessionDetails)
		if !p.In(details.stack) {
			return "", false
		}
		return details.keys[y-details.stack.Min.Y], true
	case m.isCompact:
		return "", p.In(m.layout.stackTrail)
	}
	line, ok := m.sidebarLineAt(x, y)
	if !ok {
		return "", false
	}
	return m.sidebarStack.at(line)
}
