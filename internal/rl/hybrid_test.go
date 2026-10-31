package rl

import (
	"path/filepath"
	"testing"
	"time"

	"buddy/internal/activity"
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
