//go:build unix

package agent

import (
	"time"

	"golang.org/x/sys/unix"
)

// changedAt is when the file at path last changed: the later of its
// modification time and its status change time, so a file moved or copied
// with its times kept (git mv, mv, cp -p) counts as changed when it was.
func changedAt(path string) (time.Time, bool) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return time.Time{}, false
	}
	m, c := time.Unix(st.Mtim.Unix()), time.Unix(st.Ctim.Unix())
	if c.After(m) {
		return c, true
	}
	return m, true
}
