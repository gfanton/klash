//go:build unix

package cliprelay

import (
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

// terminal stands in for the terminal gnosh ssh runs in: tty is what gnosh
// reads keys from and writes to, the test types on keys, the other side, and
// screen holds what the terminal was sent.
type terminal struct {
	tty  *os.File
	keys *os.File

	mu     sync.Mutex
	screen bytes.Buffer
}

func newTerminal(t *testing.T, rows, cols uint16) *terminal {
	t.Helper()
	ptmx, tty, err := pty.Open()
	require.NoError(t, err)
	require.NoError(t, pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}))
	tm := &terminal{tty: tty, keys: ptmx}
	read := make(chan struct{})
	go func() {
		defer close(read)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			tm.mu.Lock()
			tm.screen.Write(buf[:n])
			tm.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		// Run leaves a goroutine reading keys, as gnosh ssh exits once
		// Run returns; a line lets it go.
		_, _ = ptmx.Write([]byte("\n"))
		tty.Close()
		ptmx.Close()
		select {
		case <-read:
		case <-time.After(5 * time.Second):
		}
	})
	return tm
}

func (tm *terminal) shown() string {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.screen.String()
}

func (tm *terminal) waitFor(t *testing.T, s string) {
	t.Helper()
	require.Eventually(t, func() bool { return strings.Contains(tm.shown(), s) },
		10*time.Second, 10*time.Millisecond, "the terminal never showed %q; it shows %q", s, tm.shown())
}

func (tm *terminal) typeKeys(t *testing.T, keys string) {
	t.Helper()
	_, err := tm.keys.Write([]byte(keys))
	require.NoError(t, err)
}

// clipboard records what gnosh ssh would put on the clipboard.
type clipboard struct {
	mu     sync.Mutex
	copied []string
}

func (c *clipboard) set(text []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.copied = append(c.copied, string(text))
}

func (c *clipboard) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.copied...)
}

// relayed starts sh -c script through Run on tm and returns the exit status
// Run gives once the script ends.
func relayed(t *testing.T, tm *terminal, clip *clipboard, script string, env ...string) <-chan int {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
	cmd.Env = append(os.Environ(), env...)
	status := make(chan int, 1)
	go func() {
		code, err := Run(cmd, tm.tty, tm.tty, clip.set)
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		status <- code
	}()
	return status
}

func exited(t *testing.T, status <-chan int) int {
	t.Helper()
	select {
	case code := <-status:
		return code
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return once the command ended")
		return 0
	}
}

// A copy the command makes, written as gnosh writes it, goes on the
// clipboard, and the terminal is sent the output untouched, the copy
// included.
func TestRunCopiesWhatTheCommandCopies(t *testing.T) {
	t.Parallel()

	tm := newTerminal(t, 24, 80)
	var clip clipboard
	text := "a selection, héllo wörld\nand 日本語"
	status := relayed(t, tm, &clip, `printf '%s' "$COPY"; printf 'done\n'; printf '\033]52;c;%s\007' "$LAST"`,
		"COPY="+ansi.SetSystemClipboard(text),
		"LAST="+base64.StdEncoding.EncodeToString([]byte("and the last")))
	require.Equal(t, 0, exited(t, status))
	require.Equal(t, []string{text, "and the last"}, clip.texts())
	tm.waitFor(t, ansi.SetSystemClipboard(text)+"done\r\n")
}

// A clipboard that is slow, or stuck, never holds up the output, and once
// it takes writes again the last one reaches it.
func TestRunNeverWaitsOnTheClipboard(t *testing.T) {
	t.Parallel()

	tm := newTerminal(t, 24, 80)
	var clip clipboard
	release := make(chan struct{})
	stuck := func(text []byte) {
		<-release
		clip.set(text)
	}
	cmd := exec.CommandContext(t.Context(), "sh", "-c",
		`i=1; while [ $i -le 40 ]; do printf '\033]52;c;%s\007' "$(printf 'copy %d' $i | base64)"; i=$((i+1)); done; printf 'done\n'`)
	status := make(chan int, 1)
	go func() {
		code, err := Run(cmd, tm.tty, tm.tty, stuck)
		if err != nil {
			t.Errorf("Run: %v", err)
		}
		status <- code
	}()
	tm.waitFor(t, "done")
	close(release)
	require.Equal(t, 0, exited(t, status))
	copied := clip.texts()
	require.NotEmpty(t, copied)
	require.Equal(t, "copy 40", copied[len(copied)-1])
	require.LessOrEqual(t, len(copied), copyQueue+1)
}

// Keys reach the command as typed, and Run gives the command's exit status.
func TestRunPassesKeysAndTheExitStatus(t *testing.T) {
	t.Parallel()

	tm := newTerminal(t, 24, 80)
	var clip clipboard
	status := relayed(t, tm, &clip, `printf 'ready\n'; IFS= read -r line; printf 'got:%s\n' "$line"; exit 7`)
	tm.waitFor(t, "ready")
	tm.typeKeys(t, "typed \x1b[A\r")
	tm.waitFor(t, "got:typed \x1b[A")
	require.Equal(t, 7, exited(t, status))
	require.Empty(t, clip.texts())

	tm = newTerminal(t, 24, 80)
	status = relayed(t, tm, &clip, `kill -TERM $$`)
	require.Equal(t, 128+int(syscall.SIGTERM), exited(t, status), "a command a signal ended")
}

// The command's window is the terminal's size, and follows it when the
// terminal is resized.
func TestRunGivesTheCommandTheWindowSize(t *testing.T) {
	t.Parallel()

	tm := newTerminal(t, 40, 100)
	var clip clipboard
	status := relayed(t, tm, &clip, `stty size; while IFS= read -r line; do [ "$line" = q ] && exit 0; stty size; done`)
	tm.waitFor(t, "40 100")

	require.NoError(t, pty.Setsize(tm.keys, &pty.Winsize{Rows: 50, Cols: 120}))
	// A terminal sends its foreground process SIGWINCH; here the test does.
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGWINCH))
	require.Eventually(t, func() bool {
		_, _ = tm.keys.Write([]byte("\r"))
		return strings.Contains(tm.shown(), "50 120")
	}, 10*time.Second, 50*time.Millisecond)
	tm.typeKeys(t, "q\r")
	require.Equal(t, 0, exited(t, status))
}

// The command's terminal starts with the settings the terminal had, and the
// terminal has them back once Run returns.
func TestRunKeepsTheTerminalSettings(t *testing.T) {
	t.Parallel()

	tm := newTerminal(t, 24, 80)
	stty := exec.CommandContext(t.Context(), "stty", "erase", "^H")
	stty.Stdin = tm.tty
	require.NoError(t, stty.Run())
	before, err := getState(tm.tty)
	require.NoError(t, err)

	var clip clipboard
	status := relayed(t, tm, &clip, `stty -a`)
	require.Equal(t, 0, exited(t, status))
	tm.waitFor(t, "erase = ^H")
	after, err := getState(tm.tty)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func getState(f *os.File) (*term.State, error) {
	return term.GetState(f.Fd())
}

// With no terminal, the command runs on what it is given, and nothing is
// copied: there is no terminal to stand in for.
func TestRunWithoutATerminal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "in"))
	require.NoError(t, err)
	_, err = in.WriteString("piped\n")
	require.NoError(t, err)
	_, err = in.Seek(0, io.SeekStart)
	require.NoError(t, err)
	out, err := os.Create(filepath.Join(dir, "out"))
	require.NoError(t, err)
	defer in.Close()
	defer out.Close()

	var clip clipboard
	cmd := exec.CommandContext(t.Context(), "sh", "-c", `printf '\033]52;c;aGk=\007'; cat; exit 3`)
	status, err := Run(cmd, in, out, clip.set)
	require.NoError(t, err)
	require.Equal(t, 3, status)
	require.Empty(t, clip.texts())
	got, err := os.ReadFile(filepath.Join(dir, "out"))
	require.NoError(t, err)
	require.Equal(t, "\x1b]52;c;aGk=\x07piped\n", string(got))
}
