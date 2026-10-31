package rl

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
)

func TestInputsInRange(t *testing.T) {
	t.Parallel()
	for _, o := range []Observation{
		{},
		{Snapshot: activity.Snapshot{Category: activity.Chat, Idle: 10 * time.Hour, Activity: 2, InApp: 100 * time.Hour}, Hour: 23, Annoyance: 9, SinceAction: 50 * time.Hour},
		{Snapshot: activity.Snapshot{Category: 99, Activity: -1}, Hour: -3, Annoyance: -1},
	} {
		for i, v := range Inputs(o) {
			if v < -1e-9 || v > 1+1e-9 {
				t.Errorf("Inputs(%+v)[%d] = %v, want within 0..1", o, i, v)
			}
		}
	}
}

// The network should learn a rule the table can only learn one bucket at a
// time: waving pays in the browser and costs in a game.
func TestNetLearnsContext(t *testing.T) {
	t.Parallel()
	n := NewNet(1)
	browser := Observation{Snapshot: activity.Snapshot{Category: activity.Browser, Activity: 0.2, InApp: 20 * time.Minute}, Hour: 15, SinceAction: time.Hour}
	game := browser
	game.Category = activity.Game
	for i := range 3000 {
		o := browser
		o.InApp = time.Duration(i%60) * time.Minute // vary details the rule doesn't depend on
		n.Learn(o, Wave, 1, o)
		n.Learn(o, Quiet, 0, o)
		g := game
		g.InApp = o.InApp
		n.Learn(g, Wave, -1, g)
		n.Learn(g, Quiet, 0, g)
	}
	// A browser session length it never saw.
	unseen := browser
	unseen.InApp = 3 * time.Hour
	if q := n.Scores(unseen); q[Wave] <= q[Quiet] {
		t.Errorf("in the browser wave = %.2f, quiet = %.2f; want wave to win", q[Wave], q[Quiet])
	}
	if q := n.Scores(game); q[Wave] >= q[Quiet] {
		t.Errorf("in a game wave = %.2f, quiet = %.2f; want quiet to win", q[Wave], q[Quiet])
	}
}

func TestNewNetIsQuiet(t *testing.T) {
	t.Parallel()
	n := NewNet(3)
	o := Observation{Snapshot: activity.Snapshot{Category: activity.Code, Activity: 0.3}, Hour: 10, SinceAction: time.Hour}
	if got := n.Best(o); got != Quiet && got != Fidget {
		t.Errorf("an untrained network picks %v, want a calm action", got)
	}
}

func TestNetSaveLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "net.gob")
	n := NewNet(1)
	o := Observation{Snapshot: activity.Snapshot{Category: activity.Media}, Hour: 21}
	for range minReplay + 10 {
		n.Learn(o, Speak, 1, o)
	}
	n.SetSimScore(7.5)
	if err := n.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := LoadNet(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if m.Scores(o) != n.Scores(o) || m.Steps != n.Steps || len(m.QuietMem.Items)+len(m.ActMem.Items) != len(n.QuietMem.Items)+len(n.ActMem.Items) || m.SimScore() != 7.5 {
		t.Error("loaded network differs from the saved one")
	}
	m.Learn(o, Speak, 1, o) // still trainable after loading
	if _, err := LoadNet(filepath.Join(t.TempDir(), "none.gob"), 1); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("LoadNet of a missing file: %v, want fs.ErrNotExist", err)
	}
}

func BenchmarkNetLearn(b *testing.B) {
	n := NewNet(1)
	o := Observation{Snapshot: activity.Snapshot{Category: activity.Code}, Hour: 10}
	for range minReplay {
		n.Learn(o, Quiet, 0, o)
	}
	for b.Loop() {
		n.Learn(o, Wave, 0.5, o)
	}
}
