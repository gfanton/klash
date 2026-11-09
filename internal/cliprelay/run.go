package cliprelay

import (
	"os"
	"os/exec"
	"os/signal"

	"github.com/charmbracelet/x/term"
)

// Run runs cmd and returns its exit status as a shell gives it: 128 and the
// signal's number for a command that a signal ended.
//
// When in and out are a terminal, cmd runs on a pseudo-terminal of its own,
// joined to that terminal until cmd exits, as if cmd ran there itself: the
// terminal goes into raw mode, keys go through to cmd untouched and cmd's
// output comes back untouched, cmd's terminal starts with the terminal's
// settings and follows its window size, and the signals that would end
// gnosh go to cmd instead. The text of each OSC 52 clipboard write that cmd
// prints also goes to setClipboard, in order, from a goroutine of its own,
// so that a slow clipboard never holds up the output; should it fall behind
// by more than a few writes, the oldest waiting give way to the newer, and
// the last always reaches it.
//
// Otherwise cmd runs with in, out and gnosh's standard error as they are,
// and nothing is copied: there is no terminal to stand in for, or, on a
// system without pseudo-terminals, no way to stand in for it.
func Run(cmd *exec.Cmd, in, out *os.File, setClipboard func(text []byte)) (int, error) {
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return plain(cmd, in, out)
	}
	return relay(cmd, in, out, setClipboard)
}

// plain runs cmd with in, out and gnosh's standard error.
func plain(cmd *exec.Cmd, in, out *os.File) (int, error) {
	// A Ctrl-C at the terminal, if there is one, is cmd's to act on; gnosh
	// waits for cmd to finish.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, os.Stderr
	return exitStatus(cmd.Run())
}
