package sim

import (
	"testing"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

// Training against the simulated user must beat a Buddy that never acts.
func TestTrainingBeatsStayingQuiet(t *testing.T) {
	t.Parallel()
	agent := rl.NewAgent(1)
	eval := func() float64 {
		u := NewUser(99)
		var total float64
		for range 50 {
			total += RunDay(u, agent, false).Reward
		}
		return total / 50
	}
	before := eval() // an untrained agent is always quiet
	u := NewUser(1)
	for range 1000 {
		RunDay(u, agent, true)
	}
	after := eval()
	// The simulated person habituates, so gains are modest; but they must be clear.
	if after < before+0.5 {
		t.Errorf("reward per day after training = %.2f, want at least 0.5 more than staying quiet (%.2f)", after, before)
	}
}

// Moments records real moments: as many as asked, none while away.
func TestMoments(t *testing.T) {
	t.Parallel()
	ms := Moments(rl.NewAgent(1), 5, 10, 500)
	if len(ms) != 500 {
		t.Fatalf("got %d moments, want 500", len(ms))
	}
	cats := map[activity.Category]bool{}
	for _, m := range ms {
		if m.Away() {
			t.Fatal("recorded a moment while the person was away")
		}
		cats[m.Category] = true
	}
	if len(cats) < 4 {
		t.Errorf("moments cover only %d kinds of app, want a spread", len(cats))
	}
}
