package log

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/crush/internal/event"
	"github.com/charmbracelet/x/term"
	"gopkg.in/natefinch/lumberjack.v2"
)

var (
	initOnce    sync.Once
	initialized atomic.Bool
	// fileLogger writes the log file Setup was given.
	fileLogger atomic.Pointer[lumberjack.Logger]
)

func Setup(logFile string, debug bool, ws ...io.Writer) {
	initOnce.Do(func() {
		logRotator := &lumberjack.Logger{
			Filename:   logFile,
			MaxSize:    10,    // Max size in MB
			MaxBackups: 0,     // Number of backups
			MaxAge:     30,    // Days
			Compress:   false, // Enable compression
		}
		fileLogger.Store(logRotator)

		level := slog.LevelInfo
		if debug {
			level = slog.LevelDebug
		}

		opts := &slog.HandlerOptions{
			Level:     level,
			AddSource: true,
		}

		var handlers []slog.Handler
		handlers = append(handlers, slog.NewJSONHandler(logRotator, opts))

		for _, w := range ws {
			if w == nil {
				continue
			}
			if f, ok := w.(term.File); ok && term.IsTerminal(f.Fd()) {
				handlers = append(handlers, slog.NewTextHandler(w, opts))
			} else {
				handlers = append(handlers, slog.NewJSONHandler(w, opts))
			}
		}

		final := slog.NewMultiHandler(handlers...)
		early.buf.drainTo(final)
		slog.SetDefault(slog.New(final))
		initialized.Store(true)
	})
}

func Initialized() bool {
	return initialized.Load()
}

// ReopenIfRemoved opens a new log file at Setup's path when the one being
// written was removed, since logging would otherwise go on into the
// removed file. It does nothing before Setup.
func ReopenIfRemoved() error {
	l := fileLogger.Load()
	if l == nil {
		return nil
	}
	if _, err := os.Stat(l.Filename); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	// Rotate moves aside only a file still at the path, so here it just
	// opens a new one.
	return l.Rotate()
}

func RecoverPanic(name string, cleanup func()) {
	if r := recover(); r != nil {
		event.Error(r, "panic", true, "name", name)

		// Create a timestamped panic log file
		timestamp := time.Now().Format("20060102-150405")
		filename := fmt.Sprintf("crush-panic-%s-%s.log", name, timestamp)

		file, err := os.Create(filename)
		if err == nil {
			defer file.Close()

			// Write panic information and stack trace
			fmt.Fprintf(file, "Panic in %s: %v\n\n", name, r)
			fmt.Fprintf(file, "Time: %s\n\n", time.Now().Format(time.RFC3339))
			fmt.Fprintf(file, "Stack Trace:\n%s\n", debug.Stack())

			// Execute cleanup function if provided
			if cleanup != nil {
				cleanup()
			}
		}
	}
}
