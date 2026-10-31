package brain

import (
	"context"
	"math/rand/v2"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"buddy/internal/activity"
	"buddy/internal/rl"
	"buddy/internal/store"
)

type fakeSensor struct {
	mu   sync.Mutex
	snap activity.Snapshot
}

func (f *fakeSensor) Snapshot() activity.Snapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snap
}

func (f *fakeSensor) Changes() <-chan struct{} { return nil }

type fakeLog struct {
	mu    sync.Mutex
	steps []store.Step
}

func (f *fakeLog) Add(_ context.Context, s store.Step) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, s)
	return nil
}

func newBrain(agent *rl.Agent, snap activity.Snapshot) (*Brain, chan Command, chan rl.Reaction, *fakeLog) {
	cmds := make(chan Command, 4)
	reactions := make(chan rl.Reaction, 4)
	log := &fakeLog{}
	return &Brain{
		Agent:     agent,
		Sensor:    &fakeSensor{snap: snap},
		Log:       log,
		Save:      func() error { return nil },
		Commands:  cmds,
		Reactions: reactions,
		Phrase:    func(rl.Observation, *rand.Rand) string { return "hi" },
	}, cmds, reactions, log
}

// A click right after an action is credited to that action and learned at
// the next decision.
func TestClickRewardsAction(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		snap := activity.Snapshot{Category: activity.Browser, Activity: 0.2, InApp: 20 * time.Minute}
		agent := rl.NewAgent(1)
		// Make waving the obvious choice everywhere this test can land.
		for s := range rl.State(rl.NumStates) {
			agent.Update(s, rl.Wave, 0.5, s)
			agent.Steps = 1e9 // exploration at its minimum
		}
		b, cmds, reactions, log := newBrain(agent, snap)
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { b.Run(ctx); close(done) }()

		var cmd Command
		for cmd.Action != rl.Wave {
			cmd = <-cmds // the first tick fires after Tick
		}
		reactions <- rl.Clicked
		synctest.Wait()

		log.mu.Lock()
		if len(log.steps) == 0 {
			t.Fatal("no step logged")
		}
		step := log.steps[len(log.steps)-1]
		log.mu.Unlock()
		if step.Reaction != rl.Clicked || step.Reward <= 0 {
			t.Fatalf("logged %+v, want a rewarded click", step)
		}

		before := agent.Values(step.State)[rl.Wave]
		time.Sleep(Tick) // next decision closes the transition
		synctest.Wait()
		cancel()
		<-done
		if after := agent.Values(step.State)[rl.Wave]; after <= before {
			t.Errorf("value of wave after a click = %v, want more than %v", after, before)
		}
	})
}

// Dismissing Buddy puts it to sleep and it stays quiet until the mute ends.
func TestDismissMutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		snap := activity.Snapshot{Category: activity.Other, Activity: 0.2, InApp: 20 * time.Minute}
		agent := rl.NewAgent(1)
		b, cmds, reactions, log := newBrain(agent, snap)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go b.Run(ctx)

		time.Sleep(time.Second)
		reactions <- rl.Dismissed
		synctest.Wait()
		if c := <-cmds; !c.Asleep {
			t.Fatalf("command after dismissal = %+v, want asleep", c)
		}

		log.mu.Lock()
		n := len(log.steps)
		log.mu.Unlock()
		time.Sleep(MuteFor - time.Minute)
		synctest.Wait()
		log.mu.Lock()
		defer log.mu.Unlock()
		if len(log.steps) != n {
			t.Errorf("Buddy acted %d times while muted", len(log.steps)-n)
		}
		select {
		case c := <-cmds:
			t.Errorf("command while muted: %+v", c)
		default:
		}
	})
}
