package appdir

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func check(t *testing.T, dir, name, want string) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s = %q, want %q", name, got, want)
	}
}

func shipped(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, Net, "shipped net")
	write(t, dir, QTable, "shipped table")
	return dir
}

func TestInstallBrainsOnFirstRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := InstallBrains(shipped(t), dir); err != nil {
		t.Fatal(err)
	}
	check(t, dir, Net, "shipped net")
	check(t, dir, QTable, "shipped table")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("data folder has %d files, want the 2 brains: %v", len(entries), entries)
	}
}

func TestInstallBrainsKeepsWhatBuddyLearned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, Net, "my net")
	write(t, dir, QTable, "my table")
	if err := InstallBrains(shipped(t), dir); err != nil {
		t.Fatal(err)
	}
	check(t, dir, Net, "my net")
	check(t, dir, QTable, "my table")
}

func TestInstallBrainsSetsAsideALoneTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write(t, dir, QTable, "my table")
	if err := InstallBrains(shipped(t), dir); err != nil {
		t.Fatal(err)
	}
	check(t, dir, Net, "shipped net")
	check(t, dir, QTable, "shipped table")
	check(t, dir, OldTable, "my table")
}

func TestInstallBrainsWithNothingShipped(t *testing.T) {
	t.Parallel()
	for _, from := range []string{"", t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		dir := t.TempDir()
		write(t, dir, QTable, "my table")
		if err := InstallBrains(from, dir); err != nil {
			t.Errorf("InstallBrains(%q): %v", from, err)
		}
		check(t, dir, QTable, "my table")
	}
}

func TestSetAsideTable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if to, err := SetAsideTable(dir); to != "" || err != nil {
		t.Errorf("with no table: moved to %q, err %v", to, err)
	}
	write(t, dir, QTable, "table")
	want := filepath.Join(dir, OldTable)
	if to, err := SetAsideTable(dir); to != want || err != nil {
		t.Errorf("with a table: moved to %q, err %v, want %q", to, err, want)
	}
	check(t, dir, OldTable, "table")
	if _, err := os.Stat(filepath.Join(dir, QTable)); !os.IsNotExist(err) {
		t.Errorf("table still there: %v", err)
	}
}

// A practice interrupted twice must not cost the real diary set aside first.
func TestSetAsideTableNeverOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, table := range []string{"real diary", "practice 1", "practice 2"} {
		write(t, dir, QTable, table)
		if _, err := SetAsideTable(dir); err != nil {
			t.Fatal(err)
		}
	}
	check(t, dir, OldTable, "real diary")
	check(t, dir, OldTable+".2", "practice 1")
	check(t, dir, OldTable+".3", "practice 2")
}
