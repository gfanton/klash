//go:build !unix

package agent

import (
	"os"
	"time"
)

// changedAt is when the file at path last changed, its modification time.
func changedAt(path string) (time.Time, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}, false
	}
	return st.ModTime(), true
}
