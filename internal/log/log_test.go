package log

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/natefinch/lumberjack.v2"
)

// A log file removed with the data directory it lives in is opened again
// at its path; one still there is written on, not rotated aside.
func TestReopenIfRemoved(t *testing.T) {
	cases := map[string]struct {
		remove bool
		want   string
	}{
		"removed":     {remove: true, want: "after\n"},
		"still there": {remove: false, want: "before\nafter\n"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "logs")
			path := filepath.Join(dir, "gnosh.log")
			l := &lumberjack.Logger{Filename: path}
			t.Cleanup(func() { _ = l.Close() })
			fileLogger.Store(l)
			t.Cleanup(func() { fileLogger.Store(nil) })

			_, err := l.Write([]byte("before\n"))
			require.NoError(t, err)
			if tc.remove {
				require.NoError(t, os.RemoveAll(dir))
			}

			require.NoError(t, ReopenIfRemoved())
			_, err = l.Write([]byte("after\n"))
			require.NoError(t, err)

			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(got))
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Len(t, entries, 1, "no rotated copy beside the log file")
		})
	}
}
