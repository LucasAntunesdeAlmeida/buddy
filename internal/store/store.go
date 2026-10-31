// Package store keeps Buddy's experience log in a local SQLite file. Rows hold
// only buckets, categories and names of actions, so the log can be read or
// shared without exposing what the user was doing in detail.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver

	"buddy/internal/rl"
)

const schema = `
CREATE TABLE IF NOT EXISTS steps (
	at        INTEGER NOT NULL, -- unix seconds
	state     INTEGER NOT NULL,
	situation TEXT    NOT NULL, -- the state, readable
	action    TEXT    NOT NULL,
	explored  INTEGER NOT NULL, -- 1 if the action was a random experiment
	reaction  TEXT    NOT NULL,
	reward    REAL    NOT NULL
);`

// Store is the experience log.
type Store struct{ db *sql.DB }

// Step is one decision and its outcome.
type Step struct {
	At       time.Time
	State    rl.State
	Action   rl.Action
	Explored bool
	Reaction rl.Reaction
	Reward   float64
}

// Open opens or creates the log at path.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(2000)")
	if err != nil {
		return nil, fmt.Errorf("open log %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create log %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Add records one step.
func (s *Store) Add(ctx context.Context, st Step) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO steps (at, state, situation, action, explored, reaction, reward) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		st.At.Unix(), int(st.State), st.State.String(), st.Action.String(), st.Explored, st.Reaction.String(), st.Reward)
	if err != nil {
		return fmt.Errorf("log step: %w", err)
	}
	return nil
}

// Close closes the log.
func (s *Store) Close() error { return s.db.Close() }
