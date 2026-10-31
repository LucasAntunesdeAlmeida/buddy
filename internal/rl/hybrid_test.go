package rl

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
)

// Reactions go into the table right away, and into the network at its next
// study.
func TestHybridLearnsThroughStudy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	h := NewHybrid(NewNet(1), NewAgent(1), filepath.Join(dir, "q.json"), 1)
	h.Net.SetMargin(0)
	o := Observation{Snapshot: activity.Snapshot{Category: activity.Browser, Activity: 0.2, InApp: 20 * time.Minute}, Hour: 15, SinceAction: time.Hour}
	before := h.Scores(o)[Wave] - h.Scores(o)[Quiet]

	for i := range studyEvery * 3 {
		a := Quiet
		r := 0.0
		if i%2 == 0 {
			a, r = Wave, 1 // waving here always works
		}
		h.Learn(o, a, r, o)
	}
	if v := h.Table.Values(Encode(o)); v[Wave] <= v[Quiet] {
		t.Fatalf("table didn't learn that waving works: %v", v)
	}
	if after := h.Scores(o)[Wave] - h.Scores(o)[Quiet]; after <= before+0.1 {
		t.Errorf("network's edge for waving went from %.3f to %.3f, want it to grow", before, after)
	}
	if h.Best(o) != Wave {
		t.Errorf("network picks %v, want wave", h.Best(o))
	}

	if err := h.Save(filepath.Join(dir, "net.gob")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(filepath.Join(dir, "q.json"), 1); err != nil {
		t.Errorf("table wasn't saved: %v", err)
	}
}

// A network trained by studying the table explores as little as the table,
// not as much as a network that has never acted.
func TestHybridExploresLikeItsTable(t *testing.T) {
	t.Parallel()
	table := NewAgent(1)
	table.Steps = 1_000_000
	h := NewHybrid(NewNet(1), table, "", 1)
	if e := h.Epsilon(); e > epsilonMin+0.001 {
		t.Fatalf("exploring %.3f of the time, want %.3f like the table", e, epsilonMin)
	}
	o := Observation{Hour: 15}
	before := h.Net.Steps
	h.Learn(o, Quiet, 0, o)
	if h.Net.Steps != before+1 {
		t.Errorf("steps went from %d to %d, want one more", before, h.Net.Steps)
	}
}
