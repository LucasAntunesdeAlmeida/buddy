package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s := Load(path)
	if !s.SoundOn() || s.Paused(time.Now()) {
		t.Fatal("defaults should be sound on, not paused")
	}
	until := time.Now().Add(time.Hour).Truncate(time.Second)
	if err := s.SetSound(false); err != nil {
		t.Fatal(err)
	}
	if err := s.PauseUntil(until); err != nil {
		t.Fatal(err)
	}

	r := Load(path)
	if r.SoundOn() {
		t.Error("sound should stay off after reloading")
	}
	if !r.Paused(until.Add(-time.Minute)) || r.Paused(until.Add(time.Minute)) {
		t.Error("pause should last until the saved time")
	}
}

func TestBadFileGivesDefaults(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s := Load(path); !s.SoundOn() {
		t.Error("a bad file should give the defaults")
	}
}
