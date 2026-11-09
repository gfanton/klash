//go:build !unix

package cliprelay

import (
	"errors"
	"os"
	"os/exec"
)

// relay runs cmd plainly: there are no pseudo-terminals here.
func relay(cmd *exec.Cmd, in, out *os.File, _ func(text []byte)) (int, error) {
	return plain(cmd, in, out)
}

func exitStatus(err error) (int, error) {
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0, nil
	case !errors.As(err, &exit):
		return 0, err
	}
	return exit.ExitCode(), nil
}
