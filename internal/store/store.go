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

	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

const schema = `
CREATE TABLE IF NOT EXISTS steps (
	at        INTEGER NOT NULL, -- unix seconds
	state     INTEGER NOT NULL,
	situation TEXT    NOT NULL, -- the state, readable
	action    TEXT    NOT NULL,
	explored  INTEGER NOT NULL, -- 1 if the action was a random experiment
	reaction  TEXT    NOT NULL,
	reward    REAL    NOT NULL,
	late      INTEGER NOT NULL DEFAULT 0 -- 1 if the reaction came after the action's window
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
	Late     bool // the reaction came after the action's window and was credited to it
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
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("upgrade log %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Add records one step.
func (s *Store) Add(ctx context.Context, st Step) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO steps (at, state, situation, action, explored, reaction, reward, late) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		st.At.Unix(), int(st.State), st.State.String(), st.Action.String(), st.Explored, st.Reaction.String(), st.Reward, st.Late)
	if err != nil {
		return fmt.Errorf("log step: %w", err)
	}
	return nil
}

// Close closes the log.
func (s *Store) Close() error { return s.db.Close() }

// migrate adds columns that logs from older versions of Buddy lack.
func migrate(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT name FROM pragma_table_info('steps')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	has := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		has[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !has["late"] {
		if _, err := db.ExecContext(ctx, `ALTER TABLE steps ADD COLUMN late INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	return nil
}

// ActionStats counts how one action went.
type ActionStats struct {
	Action                                    string
	Tries, Explored                           int
	Clicked, Dragged, Dismissed, Played, Late int
}

// Actions summarizes every attempt since a time, one row per action.
func (s *Store) Actions(ctx context.Context, since time.Time) ([]ActionStats, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT action,
			SUM(NOT late), SUM(explored AND NOT late),
			SUM(reaction = 'clicked'), SUM(reaction = 'dragged'), SUM(reaction = 'dismissed'),
			SUM(reaction = 'played'), SUM(late)
		FROM steps WHERE at >= ? GROUP BY action ORDER BY SUM(NOT late) DESC`, since.Unix())
	if err != nil {
		return nil, fmt.Errorf("summarize log: %w", err)
	}
	defer rows.Close()
	var out []ActionStats
	for rows.Next() {
		var a ActionStats
		if err := rows.Scan(&a.Action, &a.Tries, &a.Explored, &a.Clicked, &a.Dragged, &a.Dismissed, &a.Played, &a.Late); err != nil {
			return nil, fmt.Errorf("summarize log: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Week is how attempts to get attention went in one week.
type Week struct {
	Start                       time.Time
	Attempts, Clicks, Dismisses int
}

// Weeks returns the last n weeks with any attempts, oldest first, to show
// whether Buddy is getting better at picking its moments.
func (s *Store) Weeks(ctx context.Context, n int) ([]Week, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT at / 604800 AS week,
			SUM(NOT late AND action NOT IN ('quiet', 'fidget')),
			SUM(reaction = 'clicked' AND action NOT IN ('quiet', 'fidget')),
			SUM(reaction = 'dismissed')
		FROM steps GROUP BY week ORDER BY week DESC LIMIT ?`, n)
	if err != nil {
		return nil, fmt.Errorf("summarize weeks: %w", err)
	}
	defer rows.Close()
	var out []Week
	for rows.Next() {
		var w Week
		var week int64
		if err := rows.Scan(&week, &w.Attempts, &w.Clicks, &w.Dismisses); err != nil {
			return nil, fmt.Errorf("summarize weeks: %w", err)
		}
		w.Start = time.Unix(week*604800, 0)
		out = append([]Week{w}, out...)
	}
	return out, rows.Err()
}
