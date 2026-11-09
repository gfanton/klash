package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/charmbracelet/crush/internal/clipboard"
	"github.com/charmbracelet/crush/internal/cliprelay"
	"github.com/spf13/cobra"
)

// sshCmd is gnosh ssh in gnosh --help. main runs gnosh ssh through SSH
// before anything else gnosh does at start.
var sshCmd = &cobra.Command{
	Use:   "ssh [ssh arguments]",
	Short: "Run ssh, and bring what is copied on the far side to this computer's clipboard",
	Long: `Run ssh with the arguments as given, and put the text a program on the far
side copies, as gnosh does when you select text with the mouse, on this
computer's clipboard.

A program sets the clipboard of the terminal it is shown in with an escape
code, OSC 52, and some terminals ignore it: Apple's Terminal does, so a copy
made in gnosh over ssh never arrives there, though gnosh says it was made.
gnosh ssh stands between the terminal and ssh, passes everything through
untouched, and puts each copy on the clipboard itself. Inside tmux on the
far side, "set -g set-clipboard on" in ~/.tmux.conf lets the copies out of
tmux. A request to read this computer's clipboard is never answered.`,
	Example: `
# Instead of ssh devbox
gnosh ssh devbox

# In ~/.zshrc or ~/.bashrc, so that ssh typed in Apple's Terminal does this
[[ $TERM_PROGRAM == Apple_Terminal ]] && ssh() { gnosh ssh "$@"; }
  `,
	DisableFlagParsing:    true,
	DisableFlagsInUseLine: true,
}

// sshCmd runs SSH, which shows sshCmd's help, so its Run is set here rather
// than in sshCmd itself, which may not refer to itself.
func init() {
	sshCmd.Run = func(_ *cobra.Command, args []string) {
		os.Exit(SSH(args))
	}
}

// SSH runs gnosh ssh with args, the words after "ssh", and returns its exit
// status: ssh's, or 255, ssh's own for its failures, when ssh did not run.
func SSH(args []string) int {
	if len(args) == 1 && (args[0] == "-h" || args[0] == "--help") {
		_ = sshCmd.Help()
		return 0
	}
	path, err := exec.LookPath("ssh")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gnosh ssh:", err)
		return 255
	}
	ssh := exec.CommandContext(context.Background(), path, args...)
	status, err := cliprelay.Run(ssh, os.Stdin, os.Stdout, setClipboard)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gnosh ssh:", err)
		return 255
	}
	return status
}

// setClipboard puts text on this computer's clipboard the way gnosh's own
// copies go there, and on macOS through pbcopy should that fail.
func setClipboard(text []byte) {
	if clipboard.WriteText(string(text)) == nil || runtime.GOOS != "darwin" {
		return
	}
	pbcopy := exec.CommandContext(context.Background(), "/usr/bin/pbcopy")
	pbcopy.Stdin = bytes.NewReader(text)
	// pbcopy reads its input in the locale's encoding.
	pbcopy.Env = append(os.Environ(), "LC_ALL=en_US.UTF-8")
	_ = pbcopy.Run()
}
