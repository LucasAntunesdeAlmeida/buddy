// Package settings keeps the switches from Buddy's tray menu and remembers
// them between runs.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Settings is safe to use from the window and the brain at once.
type Settings struct {
	path string

	mu          sync.Mutex
	soundOff    bool
	pausedUntil time.Time
}

type file struct {
	SoundOff    bool      `json:"sound_off,omitempty"`
	PausedUntil time.Time `json:"paused_until,omitzero"`
}

// Load reads settings from path. A missing or unreadable file gives the
// defaults, so a bad settings file never stops Buddy from starting.
func Load(path string) *Settings {
	s := &Settings{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var f file
	if json.Unmarshal(b, &f) == nil {
		s.soundOff, s.pausedUntil = f.SoundOff, f.PausedUntil
	}
	return s
}

// SoundOn reports whether Buddy may chirp and talk out loud.
func (s *Settings) SoundOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.soundOff
}

// SetSound turns sound on or off and saves.
func (s *Settings) SetSound(on bool) error {
	s.mu.Lock()
	s.soundOff = !on
	s.mu.Unlock()
	return s.save()
}

// Paused reports whether Buddy is paused at now.
func (s *Settings) Paused(now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return now.Before(s.pausedUntil)
}

// PauseUntil pauses Buddy until t; the zero time resumes it. Saves.
func (s *Settings) PauseUntil(t time.Time) error {
	s.mu.Lock()
	s.pausedUntil = t
	s.mu.Unlock()
	return s.save()
}

func (s *Settings) save() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	b, err := json.MarshalIndent(file{SoundOff: s.soundOff, PausedUntil: s.pausedUntil}, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".settings-*.json")
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("save settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
