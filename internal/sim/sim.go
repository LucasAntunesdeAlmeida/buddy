// Package sim is a toy model of a person at a computer. It lets Buddy practice
// thousands of days in seconds, so it is not clueless on its first real day.
// The numbers are invented: they encode common sense (don't interrupt games,
// people are open to a hello after a break), not measurements of anyone.
package sim

import (
	"math"
	"math/rand/v2"
	"time"

	"buddy/internal/activity"
	"buddy/internal/rl"
)

// Tick is the time between Buddy's decisions in the simulation.
const Tick = 30 * time.Second

// MuteFor is how long Buddy stays asleep after being dismissed.
const MuteFor = 30 * time.Minute

// habituation is how soon after Buddy's last move the next is easy to miss.
const habituation = 2 * time.Minute

type app struct {
	cat      activity.Category
	weight   float64 // how often the user picks it
	activity float64 // typical input level
	minutes  [2]int  // session length range
}

var apps = []app{
	{activity.Code, 3, 0.7, [2]int{20, 120}},
	{activity.Browser, 3, 0.35, [2]int{5, 40}},
	{activity.Game, 1, 0.85, [2]int{30, 150}},
	{activity.Media, 1.5, 0.03, [2]int{15, 90}},
	{activity.Chat, 1.5, 0.4, [2]int{3, 20}},
	{activity.Other, 1, 0.3, [2]int{5, 30}},
}

// User is a simulated person.
type User struct {
	rng *rand.Rand
	now time.Time

	app       app
	appStart  time.Time
	appEnd    time.Time
	awayUntil time.Time
	lastInput time.Time
	lastSeen  time.Time // when Buddy last did anything visible
}

// NewUser starts a user at 08:00 on an arbitrary day.
func NewUser(seed uint64) *User {
	u := &User{
		rng: rand.New(rand.NewPCG(seed, seed+1)),
		now: time.Date(2030, 1, 1, 8, 0, 0, 0, time.UTC),
	}
	u.lastInput = u.now
	u.nextApp()
	return u
}

// Now is the simulated clock.
func (u *User) Now() time.Time { return u.now }

func (u *User) nextApp() {
	var total float64
	for _, a := range apps {
		total += a.weight
	}
	pick := u.rng.Float64() * total
	for _, a := range apps {
		pick -= a.weight
		if pick <= 0 {
			u.app = a
			break
		}
	}
	mins := u.app.minutes[0] + u.rng.IntN(u.app.minutes[1]-u.app.minutes[0]+1)
	u.appStart = u.now
	u.appEnd = u.now.Add(time.Duration(mins) * time.Minute)
}

func (u *User) asleep() bool {
	h := u.now.Hour()
	return h < 8 // away from the computer at night
}

// Snapshot is what the activity watcher would see now.
func (u *User) Snapshot() activity.Snapshot {
	s := activity.Snapshot{
		Category: u.app.cat,
		Idle:     u.now.Sub(u.lastInput),
		InApp:    u.now.Sub(u.appStart),
	}
	if !u.away() {
		s.Activity = clamp(u.app.activity+0.15*u.rng.NormFloat64(), 0, 1)
	}
	return s
}

func (u *User) away() bool { return u.asleep() || u.now.Before(u.awayUntil) }

// Advance moves the clock forward by d.
func (u *User) Advance(d time.Duration) {
	u.now = u.now.Add(d)
	if u.away() {
		return
	}
	u.lastInput = u.now
	if !u.now.Before(u.appEnd) {
		// Between apps, sometimes take a break.
		if u.rng.Float64() < 0.25 {
			u.awayUntil = u.now.Add(time.Duration(5+u.rng.IntN(40)) * time.Minute)
		}
		u.nextApp()
	}
}

// How noticeable each action is, and how irritating it is when unwanted.
var (
	noticeable = [rl.NumActions]float64{rl.Quiet: 0, rl.Fidget: 0.05, rl.Wave: 0.35, rl.Bounce: 0.45, rl.Speak: 0.55, rl.Chirp: 0.7, rl.Approach: 0.55}
	// Walking over is about as welcome as a wave: a character coming to say hi
	// is charming, not intrusive, as long as it picks its moment.
	irritating = [rl.NumActions]float64{rl.Quiet: 0, rl.Fidget: 0.2, rl.Wave: 0.6, rl.Bounce: 0.8, rl.Speak: 0.7, rl.Chirp: 1.5, rl.Approach: 0.6}
	// How welcome Buddy is in each kind of app.
	openness = [activity.NumCategories]float64{
		activity.Other: 0.55, activity.Code: 0.3, activity.Browser: 0.6,
		activity.Game: 0.03, activity.Media: 0.15, activity.Chat: 0.4,
	}
)

// Respond decides how the user reacts to an action. annoyance is the user's
// real mood, which Buddy can only estimate.
func (u *User) Respond(act rl.Action, o rl.Observation, annoyance float64) rl.Reaction {
	if u.away() {
		return rl.Ignored
	}
	if act == rl.Quiet {
		// Now and then people poke Buddy just because it is there.
		if u.rng.Float64() < 0.003 {
			return rl.Clicked
		}
		return rl.Ignored
	}
	// People stop noticing something that moves all the time: anything
	// Buddy does soon after his last move is much easier to miss. Without
	// this, fidgeting every 30 seconds wins more clicks than any real hello.
	notice := noticeable[act]
	if u.now.Sub(u.lastSeen) < habituation {
		notice *= 0.2
	}
	u.lastSeen = u.now
	if u.rng.Float64() >= notice {
		return rl.Ignored
	}

	open := openness[o.Category]
	if o.Focused() {
		open *= 0.3
	}
	if o.InApp < 3*time.Minute && o.SinceAction > 15*time.Minute {
		open *= 1.6 // just came back or switched tasks: a good moment
	}
	if o.SinceAction < 2*time.Minute {
		open *= 0.4 // again?
	}
	open *= math.Exp(-annoyance / 1.5)
	open = clamp(open, 0, 0.95)

	irritation := irritating[act] * (0.08 + 0.12*annoyance)
	if o.Focused() {
		irritation *= 2.5
	}
	if o.Category == activity.Game || o.Category == activity.Media {
		irritation *= 2
	}

	switch roll := u.rng.Float64(); {
	case roll < open:
		return rl.Clicked
	case roll < open+irritation:
		return rl.Dismissed
	case roll < open+irritation+0.05:
		return rl.Dragged
	default:
		return rl.Ignored
	}
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }

// DayStats summarizes one simulated day.
type DayStats struct {
	Reward     float64
	Attention  int // attention actions taken
	ByAction   [rl.NumActions]int
	Clicks     int
	Dismissals int
}

// Score is DayStats averaged over several days.
type Score struct {
	Reward, Attention, Clicks, Dismissals float64
	ByAction                              [rl.NumActions]float64 // per day
}

// Evaluate runs the learner's best policy, without exploring or learning,
// for days on a separate user, so luck during training doesn't count.
func Evaluate(l rl.Learner, seed uint64, days int) Score {
	var s Score
	u := NewUser(seed)
	for range days {
		d := RunDay(u, l, false)
		s.Reward += d.Reward
		s.Attention += float64(d.Attention)
		s.Clicks += float64(d.Clicks)
		s.Dismissals += float64(d.Dismissals)
		for a, n := range d.ByAction {
			s.ByAction[a] += float64(n)
		}
	}
	n := float64(days)
	for a := range s.ByAction {
		s.ByAction[a] /= n
	}
	return Score{s.Reward / n, s.Attention / n, s.Clicks / n, s.Dismissals / n, s.ByAction}
}

// RunDay simulates one 24-hour day. If learn is true the agent is updated
// after every step.
func RunDay(u *User, agent rl.Learner, learn bool) DayStats {
	var (
		stats      DayStats
		mood       rl.Annoyance // the user's real mood
		estimate   rl.Annoyance // Buddy's estimate, which only sees reactions
		lastAction = u.Now().Add(-time.Hour)
		mutedUntil time.Time
		end        = u.Now().Add(24 * time.Hour)
	)
	observe := func() rl.Observation {
		now := u.Now()
		return rl.Observation{
			Snapshot:    u.Snapshot(),
			Hour:        now.Hour(),
			Annoyance:   estimate.Level(now),
			SinceAction: now.Sub(lastAction),
		}
	}
	for u.Now().Before(end) {
		now := u.Now()
		o := observe()
		if now.Before(mutedUntil) || o.Away() {
			u.Advance(Tick)
			continue
		}
		var act rl.Action
		if learn {
			act, _ = agent.Act(o)
		} else {
			act = agent.Best(o)
		}
		// Extra irritation when Buddy acts again soon after the last time.
		if act.Attention() && o.SinceAction < 2*time.Minute {
			mood.Observe(now, act, rl.Ignored)
		}
		r := u.Respond(act, o, mood.Level(now))
		reward := rl.Reward(act, r, o.Focused())
		mood.Observe(now, act, r)
		estimate.Observe(now, act, r)
		if act.Attention() {
			lastAction = now
			stats.Attention++
			stats.ByAction[act]++
		}
		switch r {
		case rl.Clicked:
			stats.Clicks++
		case rl.Dismissed:
			stats.Dismissals++
			mutedUntil = now.Add(MuteFor)
		}
		stats.Reward += reward

		u.Advance(Tick)
		if learn {
			agent.Learn(o, act, reward, observe())
		}
	}
	return stats
}
