package backend

import (
	"fmt"
	"os"

	"github.com/charmbracelet/crush/internal/db"
)

func createDotCrushDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create data directory: %q %w", dir, err)
	}

	return db.WriteGitIgnore(dir)
}
