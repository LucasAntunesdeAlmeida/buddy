// Package brain runs Buddy's decide, act, observe, learn loop against the
// real desktop.
package brain

import (
	"context"
	"log/slog"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/store"
)

// Timing of the loop.
const (
	Tick        = 30 * time.Second // decide at least this often
	MinGap      = 10 * time.Second // and at most this often
	ReactWindow = 10 * time.Second // how long Buddy watches for a reaction to an action
	MuteFor     = 30 * time.Minute // asleep after being dismissed
	SaveEvery   = 5 * time.Minute
	// CreditWindow is how long an attempt stays open to late reactions. A
	// drag or dismissal a little after Buddy waved is about the wave, even
	// if Buddy has decided to stay quiet a few times since.
	CreditWindow = time.Minute
)

// Command tells the UI what to show.
type Command struct {
	Action rl.Action
	Phrase string // for Speak
	Asleep bool   // true while the user is away, Buddy was dismissed, or it's paused
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

// Settings are the user's switches from the tray menu.
type Settings interface {
	Paused(now time.Time) bool
	SoundOn() bool
}

// Brain is the learning loop.
type Brain struct {
	Learner   rl.Learner
	Sensor    Sensor
	Log       Logger
	Save      func() error
	Commands  chan<- Command
	Reactions <-chan rl.Reaction
	Phrase    func(rl.Observation, time.Time) string
	Speaker   Speaker  // optional
	Settings  Settings // optional

	annoyance  rl.Annoyance
	lastAction time.Time
	lastDecide time.Time
	mutedUntil time.Time
	asleep     bool
	busyWhy    string
	open       []*transition // steps not learned yet, oldest first
}

// transition is one step waiting to be learned: it needs the state that
// followed it, and attempts also wait out the credit window.
type transition struct {
	at     time.Time
	obs    rl.Observation
	action rl.Action
	reward float64
	next   *rl.Observation
}

// Run loops until ctx is done, then saves what Buddy learned.
func (b *Brain) Run(ctx context.Context) {
	b.lastAction = time.Now().Add(-time.Hour)

	tick := time.NewTicker(Tick)
	defer tick.Stop()
	save := time.NewTicker(SaveEvery)
	defer save.Stop()
	defer func() {
		b.flush(b.observe(time.Now()), true)
		b.save()
	}()

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
			// A reaction outside any action's window: the user came to Buddy,
			// or answered an earlier attempt late.
			b.late(ctx, r)
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

func (b *Brain) paused(now time.Time) bool {
	return b.Settings != nil && b.Settings.Paused(now)
}

func (b *Brain) decide(ctx context.Context) {
	now := time.Now()
	b.lastDecide = now
	o := b.observe(now)
	b.flush(o, false)

	sleep := now.Before(b.mutedUntil) || o.Away() || b.paused(now)
	if sleep != b.asleep {
		b.asleep = sleep
		b.send(ctx, Command{Asleep: sleep})
	}
	if sleep {
		return // nobody is watching, so acting now teaches nothing
	}
	if o.BusyWhy != b.busyWhy {
		b.busyWhy = o.BusyWhy
		if o.Busy {
			slog.Info("staying still and silent", "because", o.BusyWhy)
		} else {
			slog.Info("no longer busy")
		}
	}
	if o.Busy {
		return // a hard rule, not something to learn: never interrupt calls or presentations
	}

	act, explored := b.Learner.Act(o)
	cmd := Command{Action: act}
	if act == rl.Speak {
		cmd.Phrase = b.Phrase(o, now)
		if b.Speaker != nil && (b.Settings == nil || b.Settings.SoundOn()) {
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
	b.open = append(b.open, &transition{at: now, obs: o, action: act, reward: reward})

	if act != rl.Quiet || reaction != rl.Ignored {
		s := rl.Encode(o)
		slog.Debug("step", "brain", b.Learner.Kind(), "state", s, "action", act, "explored", explored, "reaction", reaction, "reward", reward)
		b.record(ctx, store.Step{At: now, State: s, Action: act, Explored: explored, Reaction: reaction, Reward: reward})
	}
}

// await waits for the user's reaction to an action.
func (b *Brain) await(ctx context.Context, act rl.Action) rl.Reaction {
	if act == rl.Quiet {
		return rl.Ignored // nothing to react to; a later click is handled by late
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

// late handles a reaction between decisions. It's credited to the most
// recent attempt still in its credit window, or else to the latest step,
// which then means "the user came to Buddy on their own".
func (b *Brain) late(ctx context.Context, r rl.Reaction) {
	now := time.Now()
	var to *transition
	for i := len(b.open) - 1; i >= 0; i-- {
		if t := b.open[i]; t.action.Attention() && now.Sub(t.at) < CreditWindow {
			to = t
			break
		}
	}
	if to == nil && len(b.open) > 0 {
		to = b.open[len(b.open)-1]
	}
	if to != nil {
		extra := rl.Reward(rl.Quiet, r, false)
		to.reward += extra
		b.record(ctx, store.Step{At: now, State: rl.Encode(to.obs), Action: to.action, Reaction: r, Reward: extra, Late: true})
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

// flush gives waiting steps the state that followed them and learns every
// step that's ready. Attempts wait out their credit window unless all is true.
func (b *Brain) flush(o rl.Observation, all bool) {
	now := time.Now()
	keep := b.open[:0]
	for _, t := range b.open {
		if t.next == nil {
			next := o
			t.next = &next
		}
		if !all && t.action.Attention() && now.Sub(t.at) < CreditWindow {
			keep = append(keep, t)
			continue
		}
		b.Learner.Learn(t.obs, t.action, t.reward, *t.next)
	}
	clear(b.open[len(keep):])
	b.open = keep
}

func (b *Brain) record(ctx context.Context, s store.Step) {
	// A step that happens as Buddy quits still belongs in the log.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := b.Log.Add(ctx, s); err != nil {
		slog.Warn("could not log step", "err", err)
	}
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
