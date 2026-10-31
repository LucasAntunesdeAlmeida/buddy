package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

func TestAddAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buddy.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	step := Step{At: time.Unix(1_900_000_000, 0), State: 12, Action: rl.Wave, Reaction: rl.Clicked, Reward: 0.95}
	if err := s.Add(t.Context(), step); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var action, reaction string
	var n int
	if err := s.db.QueryRowContext(t.Context(), `SELECT COUNT(*), MAX(action), MAX(reaction) FROM steps`).Scan(&n, &action, &reaction); err != nil {
		t.Fatal(err)
	}
	if n != 1 || action != "wave" || reaction != "clicked" {
		t.Errorf("got %d rows, action %q, reaction %q; want 1, wave, clicked", n, action, reaction)
	}
}

// A log written by an older Buddy, without the late column, still opens.
func TestUpgradeOldLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `DROP TABLE steps`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), `CREATE TABLE steps (at INTEGER NOT NULL, state INTEGER NOT NULL, situation TEXT NOT NULL,
		action TEXT NOT NULL, explored INTEGER NOT NULL, reaction TEXT NOT NULL, reward REAL NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(t.Context(), path)
	if err != nil {
		t.Fatalf("opening an old log: %v", err)
	}
	defer s.Close()
	if err := s.Add(t.Context(), Step{At: time.Unix(1_900_000_000, 0), Action: rl.Wave, Late: true}); err != nil {
		t.Fatal(err)
	}
}

func TestSummaries(t *testing.T) {
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "buddy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Unix(1_900_000_000, 0)
	for _, st := range []Step{
		{At: at, Action: rl.Wave, Reaction: rl.Clicked, Reward: 0.95},
		{At: at, Action: rl.Wave, Reaction: rl.Ignored, Explored: true},
		{At: at.Add(time.Minute), Action: rl.Wave, Reaction: rl.Dismissed, Reward: -2, Late: true},
		{At: at, Action: rl.Chirp, Reaction: rl.Played},
	} {
		if err := s.Add(t.Context(), st); err != nil {
			t.Fatal(err)
		}
	}
	acts, err := s.Actions(t.Context(), at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	want := []ActionStats{
		{Action: "wave", Tries: 2, Explored: 1, Clicked: 1, Dismissed: 1, Late: 1},
		{Action: "chirp", Tries: 1, Played: 1},
	}
	if len(acts) != len(want) || acts[0] != want[0] || acts[1] != want[1] {
		t.Errorf("Actions = %+v, want %+v", acts, want)
	}
	weeks, err := s.Weeks(t.Context(), 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(weeks) != 1 || weeks[0].Attempts != 3 || weeks[0].Clicks != 1 || weeks[0].Dismisses != 1 {
		t.Errorf("Weeks = %+v, want one week with 3 attempts, 1 click, 1 dismissal", weeks)
	}
}
