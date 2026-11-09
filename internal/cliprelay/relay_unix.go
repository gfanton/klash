//go:build unix

package cliprelay

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

// drainWait is how long relay waits, once the command has exited, for the
// rest of its output; something it left running may hold its terminal open.
const drainWait = 500 * time.Millisecond

// copyWait is how long relay waits after that for the last copy to land.
const copyWait = 2 * time.Second

// copyQueue is how many writes may wait for the clipboard.
const copyQueue = 16

func relay(cmd *exec.Cmd, in, out *os.File, setClipboard func(text []byte)) (int, error) {
	size, err := pty.GetsizeFull(in)
	if err != nil {
		return 0, err
	}
	ptmx, tty, err := pty.Open()
	if err != nil {
		return 0, err
	}
	defer ptmx.Close()
	saved, err := term.MakeRaw(in.Fd())
	if err != nil {
		tty.Close()
		return 0, err
	}
	defer term.Restore(in.Fd(), saved)
	// cmd's terminal starts as in was before raw mode: the same erase key,
	// and the rest.
	_ = term.SetState(tty.Fd(), saved)
	_ = pty.Setsize(ptmx, size)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	err = cmd.Start()
	tty.Close()
	if err != nil {
		return 0, err
	}

	signals := make(chan os.Signal, 8)
	signal.Notify(signals, syscall.SIGWINCH, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for sig := range signals {
			if sig == syscall.SIGWINCH {
				_ = pty.InheritSize(in, ptmx)
				continue
			}
			_ = cmd.Process.Signal(sig)
		}
	}()
	// A resize between the size taken above and the line before.
	_ = pty.InheritSize(in, ptmx)

	// Keys go to cmd for as long as gnosh runs; this goroutine ends with it.
	go func() { _, _ = io.Copy(ptmx, in) }()

	pending := make(chan []byte, copyQueue)
	copied := make(chan struct{})
	go func() {
		defer close(copied)
		for text := range pending {
			setClipboard(text)
		}
	}()
	scanner := &Scanner{Copy: func(text []byte) {
		for {
			select {
			case pending <- text:
				return
			default:
			}
			// The clipboard is a whole queue behind: the oldest write
			// waiting gives way, as the clipboard keeps only the last.
			select {
			case <-pending:
			default:
			}
		}
	}}

	output := make(chan struct{})
	go func() {
		defer close(output)
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_, _ = out.Write(buf[:n])
				_, _ = scanner.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	status, err := exitStatus(cmd.Wait())
	signal.Stop(signals)
	close(signals)
	// What cmd printed last may still be on its way; it comes out unless
	// something cmd left running holds the terminal open.
	select {
	case <-output:
	case <-time.After(drainWait):
		_ = ptmx.Close()
		select {
		case <-output:
		case <-time.After(drainWait):
		}
	}
	select {
	case <-output:
		close(pending)
		select {
		case <-copied:
		case <-time.After(copyWait):
		}
	default:
		// The output is still being read, and so pending stays open; the
		// copy goroutine ends with gnosh.
	}
	return status, err
}

func exitStatus(err error) (int, error) {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case !errors.As(err, &exit):
		return 0, err
	}
	if ws, ok := exit.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), nil
	}
	return exit.ExitCode(), nil
}
