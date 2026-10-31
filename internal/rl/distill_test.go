package rl

import (
	"math/rand/v2"
	"testing"

	"buddy/internal/activity"
)

// Every sampled moment falls back into the state it was drawn from.
func TestSampleStaysInState(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	for s := range State(NumStates) {
		for range 3 {
			if got := Encode(Sample(s, rng)); got != s {
				t.Fatalf("Sample(%v) encodes to %v", s, got)
			}
		}
	}
}

// After distillation the network agrees with the table on what to do.
func TestDistillCopiesTable(t *testing.T) {
	t.Parallel()
	table := NewAgent(1)
	rng := rand.New(rand.NewPCG(3, 4))
	// A table with clear opinions: waving pays in the browser and costs elsewhere.
	var states []State
	for range 300 {
		s := State(rng.IntN(NumStates))
		states = append(states, s)
		for range 5 {
			r := -1.0
			if s.Features().Category == activity.Browser {
				r = 1
			}
			table.Update(s, Wave, r, s)
			table.Update(s, Quiet, 0, s)
		}
	}
	n := NewNet(1)
	n.SetMargin(0)
	n.Distill(table, nil, 150, rng)
	agree := 0
	for _, s := range states {
		// The table only has an opinion about waving; its other choices are ties at zero.
		if (n.Best(Sample(s, rng)) == Wave) == (table.Greedy(s) == Wave) {
			agree++
		}
	}
	if agree < len(states)*95/100 {
		t.Errorf("network agrees with the table in %d of %d states, want at least 95%%", agree, len(states))
	}
}
