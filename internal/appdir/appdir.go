// Package appdir locates Buddy's data folder. Everything Buddy keeps lives
// there and nowhere else.
package appdir

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// ShippedBrains is the folder next to buddy.exe with the brains a release
// comes with.
const ShippedBrains = "brain"

// File names inside the data folder.
const (
	QTable   = "qtable.json"
	OldTable = QTable + ".old" // a table that was found without a network
	Log      = "buddy.db"
	Net      = "network.gob"
	Settings = "settings.json"
	Said     = "said.json" // what Buddy said lately, so it doesn't repeat itself
	LogFile  = "buddy.log"
)

// Default returns %AppData%\buddy on Windows (and the equivalent elsewhere).
func Default() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find data folder: %w", err)
	}
	return filepath.Join(base, "buddy"), nil
}

// InstallBrains copies the brains in the shipped folder into the data folder
// dir, unless dir already has a network: that one has learned from its owner
// and is never replaced. It does nothing if shipped is empty or has no brains.
func InstallBrains(shipped, dir string) error {
	if shipped == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, Net)); !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(filepath.Join(shipped, Net)); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if _, err := SetAsideTable(dir); err != nil {
		return err
	}
	// The network goes last, so a copy cut short is simply tried again.
	for _, name := range []string{QTable, Net} {
		if err := copyFile(filepath.Join(shipped, name), filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	return nil
}

// SetAsideTable renames a table to OldTable, to make way for a new table and
// the network that learns from it. A table without its network comes from an
// interrupted practice or an older Buddy, and it may hold real reactions, so
// one set aside earlier is never overwritten: later ones get a number. It
// returns where the table went, or "" if there was none.
func SetAsideTable(dir string) (string, error) {
	from := filepath.Join(dir, QTable)
	if _, err := os.Stat(from); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("set aside the old table: %w", err)
	}
	to := filepath.Join(dir, OldTable)
	for n := 2; ; n++ {
		_, err := os.Stat(to)
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("set aside the old table: %w", err)
		}
		to = filepath.Join(dir, fmt.Sprintf("%s.%d", OldTable, n))
	}
	if err := os.Rename(from, to); err != nil {
		return "", fmt.Errorf("set aside the old table: %w", err)
	}
	return to, nil
}

// copyFile copies src to dst atomically, so a crash never leaves half a brain.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".install-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
