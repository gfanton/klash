package cliprelay

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

// scan runs the pieces of an output through a Scanner and returns what it
// copied.
func scan(pieces ...string) []string {
	var copied []string
	s := &Scanner{Copy: func(text []byte) { copied = append(copied, string(text)) }}
	for _, p := range pieces {
		n, err := s.Write([]byte(p))
		if n != len(p) || err != nil {
			panic("a Scanner takes all it is given")
		}
	}
	return copied
}

// The copies gnosh makes reach the scanner as gnosh writes them, through
// Bubble Tea's tea.SetClipboard, which writes ansi.SetSystemClipboard; and
// inside tmux as tmux writes them on to its client, captured from tmux 3.6
// with set-clipboard on (aGVsbG8= is "hello").
func TestScannerFindsTheCopiesGnoshAndTmuxMake(t *testing.T) {
	t.Parallel()

	text := "a selection, héllo wörld\nand 日本語\n"
	require.Equal(t, []string{text}, scan("before"+ansi.SetSystemClipboard(text)+"after"))

	fromTmux := "\x1b[24;1H\x1b]52;c;aGVsbG8=\x07\x1b[?25l\x1b[30m\x1b[42m"
	require.Equal(t, []string{"hello"}, scan(fromTmux))
}

func TestScannerReadsEveryFormOfAWrite(t *testing.T) {
	t.Parallel()

	for name, out := range map[string]string{
		"ended by ST":             "\x1b]52;c;aGk=\x1b\\",
		"no clipboard named":      "\x1b]52;;aGk=\x07",
		"primary":                 "\x1b]52;p;aGk=\x07",
		"several named":           "\x1b]52;cs0;aGk=\x07",
		"base64 without padding":  "\x1b]52;c;aGk\x07",
		"base64 wrapped in lines": "\x1b]52;c;aG\r\nk=\x07",
		"after a lone escape":     "\x1b\x1b]52;c;aGk=\x07",
	} {
		require.Equal(t, []string{"hi"}, scan(out), name)
	}
}

// A program on the far side may write this computer's clipboard and nothing
// more, and what is not a clipboard write is not taken for one.
func TestScannerPassesOverWhatIsNotAWrite(t *testing.T) {
	t.Parallel()

	for name, out := range map[string]string{
		"a query":                "\x1b]52;c;?\x07",
		"a clear":                "\x1b]52;c;\x07",
		"not base64":             "\x1b]52;c;h*i\x07",
		"no clipboard part":      "\x1b]52;aGk=\x07",
		"an unknown clipboard":   "\x1b]52;x;aGk=\x07",
		"a title":                "\x1b]0;52;c;aGk=\x07",
		"a hyperlink":            "\x1b]8;;https://example.com/52;c;aGk=\x1b\\link\x1b]8;;\x1b\\",
		"OSC 520":                "\x1b]520;c;aGk=\x07",
		"OSC 5":                  "\x1b]5\x07",
		"cancelled":              "\x1b]52;c;aGk=\x18\x07",
		"cut short by an escape": "\x1b]52;c;aGk=\x1b[31m\x07",
		"in plain text":          "]52;c;aGk=\x07",
		"in a CSI":               "\x1b[52;c;aGk=\x07",
		"ended by a lone escape": "\x1b]52;c;aGk=\x1b",
	} {
		require.Empty(t, scan(out), name)
		require.Equal(t, []string{"ok"}, scan(out, "\x07\x1b]52;c;b2s=\x07"), "%s, then a write", name)
	}
	require.Empty(t, scan("\x1b]52;c;aGk="), "a write that has not ended is not copied yet")
	require.Equal(t, []string{"ok"}, scan("\x1b]52;c;aGk=\x1b]52;c;b2s=\x07"), "a write cut short by the next")
}

// A write too long to copy reaches the terminal, is not copied, and the next
// one is.
func TestScannerSkipsAWriteTooLongToCopy(t *testing.T) {
	t.Parallel()

	long := "\x1b]52;c;" + strings.Repeat("A", maxWrite) + "\x07"
	require.Empty(t, scan(long))
	require.Equal(t, []string{"ok"}, scan(long, "\x1b]52;c;b2s=\x07"))

	s := &Scanner{Copy: func([]byte) {}}
	_, _ = s.Write([]byte(long))
	require.LessOrEqual(t, cap(s.body), keepBuffer, "the buffer of a long write is let go")
}

// Output comes in pieces of any size, cut anywhere.
func TestScannerFindsWritesCutIntoPieces(t *testing.T) {
	t.Parallel()

	out := "a\x1b]0;title\x07b" + ansi.SetSystemClipboard("one") +
		"\x1b]52;c;?\x07\x1b[1mc\x1b]52;;dHdv\x1b\\" + "\x1b]8;;x\x1b\\d\x1b]52;c;dGhyZWU=\x07e"
	want := []string{"one", "two", "three"}
	require.Equal(t, want, scan(out))

	oneByOne := make([]string, len(out))
	for i := range out {
		oneByOne[i] = out[i : i+1]
	}
	require.Equal(t, want, scan(oneByOne...), "a byte at a time")
	for cut := range len(out) + 1 {
		require.Equal(t, want, scan(out[:cut], out[cut:]), "cut at %d", cut)
	}
}
