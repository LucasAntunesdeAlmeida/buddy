package sim

import (
	"testing"

	"buddy/internal/rl"
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
	for range 400 {
		RunDay(u, agent, true)
	}
	after := eval()
	if after < before+1 {
		t.Errorf("reward per day after training = %.2f, want at least 1 more than staying quiet (%.2f)", after, before)
	}
}
