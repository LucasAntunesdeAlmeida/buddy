package store

import (
	"path/filepath"
	"testing"
	"time"

	"buddy/internal/rl"
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
