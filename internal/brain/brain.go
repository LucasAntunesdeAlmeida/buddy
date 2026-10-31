// Package brain runs Buddy's decide, act, observe, learn loop against the
// real desktop.
package brain

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"buddy/internal/activity"
	"buddy/internal/rl"
	"buddy/internal/store"
)

// Timing of the loop.
const (
	Tick         = 30 * time.Second // decide at least this often
	MinGap       = 10 * time.Second // and at most this often
	ReactWindow  = 10 * time.Second // how long a reaction counts toward an action
	MuteFor      = 30 * time.Minute // asleep after being dismissed
	SaveEvery    = 5 * time.Minute
	stalePending = 5 * time.Minute // a transition older than this is learned without waiting
)

// Command tells the UI what to show.
type Command struct {
	Action rl.Action
	Phrase string // for Speak
	Asleep bool   // true while the user is away or Buddy was dismissed
}

// Sensor is what the brain needs from the activity watcher.
type Sensor interface {
	Snapshot() activity.Snapshot
	Changes() <-chan struct{}
}

// Logger records each step.
type Logger interface {
	Add(context.Context, store.Step) error
}

// Speaker reads phrases aloud.
type Speaker interface{ Say(string) }

// Brain is the learning loop.
type Brain struct {
	Agent     *rl.Agent
	Sensor    Sensor
	Log       Logger
	Save      func() error
	Commands  chan<- Command
	Reactions <-chan rl.Reaction
	Speaker   Speaker // optional
	Phrase    func(rl.Observation, *rand.Rand) string

	rng        *rand.Rand
	annoyance  rl.Annoyance
	lastAction time.Time
	lastDecide time.Time
	mutedUntil time.Time
	asleep     bool
	pending    *transition
}

type transition struct {
	at     time.Time
	state  rl.State
	action rl.Action
	reward float64
}

// Run loops until ctx is done, then saves what Buddy learned.
func (b *Brain) Run(ctx context.Context) {
	b.rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 7))
	b.lastAction = time.Now().Add(-time.Hour)

	tick := time.NewTicker(Tick)
	defer tick.Stop()
	save := time.NewTicker(SaveEvery)
	defer save.Stop()
	defer b.save()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			b.decide(ctx)
		case <-b.Sensor.Changes():
			if time.Since(b.lastDecide) >= MinGap {
				b.decide(ctx)
			}
		case r := <-b.Reactions:
			// A reaction outside any action's window: the user came to Buddy.
			b.spontaneous(ctx, r)
		case <-save.C:
			b.save()
		}
	}
}

func (b *Brain) observe(now time.Time) rl.Observation {
	return rl.Observation{
		Snapshot:    b.Sensor.Snapshot(),
		Hour:        now.Hour(),
		Annoyance:   b.annoyance.Level(now),
		SinceAction: now.Sub(b.lastAction),
	}
}

func (b *Brain) decide(ctx context.Context) {
	now := time.Now()
	b.lastDecide = now
	o := b.observe(now)

	sleep := now.Before(b.mutedUntil) || o.Away()
	if sleep != b.asleep {
		b.asleep = sleep
		b.send(ctx, Command{Asleep: sleep})
	}
	if sleep {
		// Nobody is watching, so there is nothing to learn from acting now.
		// Close the last transition so it isn't held forever.
		if b.pending != nil && now.Sub(b.pending.at) > stalePending {
			b.learn(rl.Encode(o))
		}
		return
	}

	s := rl.Encode(o)
	b.learn(s)

	act, explored := b.Agent.Choose(s, o.Focused())
	cmd := Command{Action: act}
	if act == rl.Speak {
		cmd.Phrase = b.Phrase(o, b.rng)
		if b.Speaker != nil {
			b.Speaker.Say(cmd.Phrase)
		}
	}
	if act != rl.Quiet {
		b.send(ctx, cmd)
	}
	if act.Attention() {
		b.lastAction = now
	}

	reaction := b.await(ctx, act)
	reward := rl.Reward(act, reaction, o.Focused())
	b.annoyance.Observe(time.Now(), act, reaction)
	if reaction == rl.Dismissed {
		b.dismiss(ctx)
	}
	b.pending = &transition{at: now, state: s, action: act, reward: reward}

	if act != rl.Quiet || reaction != rl.Ignored {
		slog.Debug("step", "state", s, "action", act, "explored", explored, "reaction", reaction, "reward", reward)
		if err := b.Log.Add(ctx, store.Step{At: now, State: s, Action: act, Explored: explored, Reaction: reaction, Reward: reward}); err != nil {
			slog.Warn("could not log step", "err", err)
		}
	}
}

// await waits for the user's reaction to an action.
func (b *Brain) await(ctx context.Context, act rl.Action) rl.Reaction {
	if act == rl.Quiet {
		// Nothing to react to. A click before the next decision still counts, see spontaneous.
		return rl.Ignored
	}
	t := time.NewTimer(ReactWindow)
	defer t.Stop()
	select {
	case r := <-b.Reactions:
		return r
	case <-t.C:
		return rl.Ignored
	case <-ctx.Done():
		return rl.Ignored
	}
}

// spontaneous handles a reaction that arrived between decisions. It is
// credited to the last decision if that one is still waiting to be learned.
func (b *Brain) spontaneous(ctx context.Context, r rl.Reaction) {
	now := time.Now()
	if b.pending != nil && now.Sub(b.pending.at) < Tick {
		b.pending.reward += rl.Reward(rl.Quiet, r, false)
	}
	b.annoyance.Observe(now, rl.Quiet, r)
	switch {
	case r == rl.Dismissed:
		b.dismiss(ctx)
	case r == rl.Clicked && now.Before(b.mutedUntil):
		// Poking a sleeping Buddy wakes it up.
		b.mutedUntil = time.Time{}
		b.asleep = false
		b.send(ctx, Command{})
	}
}

func (b *Brain) dismiss(ctx context.Context) {
	b.mutedUntil = time.Now().Add(MuteFor)
	b.asleep = true
	b.send(ctx, Command{Asleep: true})
}

// learn closes the pending transition now that its next state is known.
func (b *Brain) learn(next rl.State) {
	if b.pending == nil {
		return
	}
	b.Agent.Update(b.pending.state, b.pending.action, b.pending.reward, next)
	b.pending = nil
}

func (b *Brain) send(ctx context.Context, c Command) {
	select {
	case b.Commands <- c:
	case <-ctx.Done():
	}
}

func (b *Brain) save() {
	if err := b.Save(); err != nil {
		slog.Error("could not save what Buddy learned", "err", err)
	}
}
