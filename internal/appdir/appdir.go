// Package appdir locates Buddy's data folder. Everything Buddy keeps lives
// there and nowhere else.
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// File names inside the data folder.
const (
	QTable = "qtable.json"
	Log    = "buddy.db"
)

// Default returns %AppData%\buddy on Windows (and the equivalent elsewhere).
func Default() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find data folder: %w", err)
	}
	return filepath.Join(base, "buddy"), nil
}
