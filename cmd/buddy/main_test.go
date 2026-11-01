package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/appdir"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

// trainedBrains returns a folder like the one a release ships.
func trainedBrains(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := rl.NewNet(1).Save(filepath.Join(dir, appdir.Net)); err != nil {
		t.Fatal(err)
	}
	if err := rl.NewAgent(1).Save(filepath.Join(dir, appdir.QTable)); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadBrainUsesTheShippedNetworkOnFirstRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l, path, err := loadBrain(dir, trainedBrains(t), "network")
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind() != "network" {
		t.Errorf("first run decides with %q, want the network", l.Kind())
	}
	if want := filepath.Join(dir, appdir.Net); path != want {
		t.Errorf("saves to %s, want %s", path, want)
	}
}

// Running on a table that never practiced would teach a real person nothing
// but annoyance, so Buddy must not quietly fall back to one.
func TestLoadBrainRefusesWithoutANetwork(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, _, err := loadBrain(dir, t.TempDir(), "network"); !errors.Is(err, errNoNetwork) {
		t.Errorf("err = %v, want errNoNetwork", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left files behind for buddysim to trip over: %v", entries)
	}
}

func TestLoadBrainReportsAnOldNetwork(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A network with no layers doesn't fit, like one from an older Buddy.
	if err := (&rl.Net{}).Save(filepath.Join(dir, appdir.Net)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadBrain(dir, "", "network"); !errors.Is(err, rl.ErrOldBrain) {
		t.Errorf("err = %v, want rl.ErrOldBrain", err)
	}
}

func TestLoadBrainTableOnRequest(t *testing.T) {
	t.Parallel()
	shipped := trainedBrains(t)
	dir := t.TempDir()
	l, path, err := loadBrain(dir, shipped, "table")
	if err != nil {
		t.Fatal(err)
	}
	if l.Kind() == "network" {
		t.Errorf("-brain table decides with %q", l.Kind())
	}
	if want := filepath.Join(dir, appdir.QTable); path != want {
		t.Errorf("saves to %s, want %s", path, want)
	}
}

func TestLoadBrainRejectsUnknownChoice(t *testing.T) {
	t.Parallel()
	if _, _, err := loadBrain(t.TempDir(), "", "lizard"); err == nil {
		t.Error("no error for an unknown brain")
	}
}

func TestStartArgs(t *testing.T) {
	t.Parallel()
	fs := flag.NewFlagSet("buddy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Bool("voice", false, "")
	fs.Bool("v", false, "")
	fs.String("data", "", "")
	if err := fs.Parse([]string{"-voice", "-data", "brains"}); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs("brains")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-data=" + abs, "-voice=true"}
	if got := startArgs(fs); !slices.Equal(got, want) {
		t.Errorf("startArgs = %q, want %q", got, want)
	}
}
