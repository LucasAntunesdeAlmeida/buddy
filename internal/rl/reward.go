package rl

import (
	"math"
	"time"
)

// Action is something Buddy can do.
type Action uint8

const (
	Quiet    Action = iota // do nothing
	Fidget                 // small idle animation, barely noticeable
	Wave                   // wave an arm
	Bounce                 // hop in place
	Speak                  // show a speech bubble
	Chirp                  // play a short sound
	Approach               // walk toward the cursor
	NumActions
)

var actionNames = [NumActions]string{"quiet", "fidget", "wave", "bounce", "speak", "chirp", "approach"}

func (a Action) String() string {
	if a >= NumActions {
		return "unknown"
	}
	return actionNames[a]
}

// Attention reports whether the action tries to get the user's attention.
func (a Action) Attention() bool { return a != Quiet && a != Fidget }

// Reaction is how the user responded to an action.
type Reaction uint8

const (
	Ignored   Reaction = iota
	Clicked            // clicked Buddy
	Dragged            // moved Buddy out of the way
	Dismissed          // told Buddy to go away
)

var reactionNames = [...]string{"ignored", "clicked", "dragged", "dismissed"}

func (r Reaction) String() string {
	if int(r) >= len(reactionNames) {
		return "unknown"
	}
	return reactionNames[r]
}

// Reward values. Every attention action costs a little, so Buddy only acts
// when it has learned that it pays off.
const (
	rewardClick    = 1.0
	rewardDrag     = -0.5
	rewardDismiss  = -2.0
	costAttention  = 0.05
	penaltyFocused = 0.3
	annoyIgnored   = 0.3
	annoyDrag      = 1.0
	annoyDismiss   = 2.0
	annoyClick     = -0.5
	annoyHalfLife  = 20 * time.Minute
)

// Reward scores one action and the user's reaction to it.
func Reward(a Action, r Reaction, focused bool) float64 {
	var v float64
	switch r {
	case Clicked:
		v = rewardClick
	case Dragged:
		v = rewardDrag
	case Dismissed:
		v = rewardDismiss
	}
	if a.Attention() {
		v -= costAttention
		if focused {
			v -= penaltyFocused
		}
	}
	return v
}

// Annoyance estimates how fed up the user is. It rises when Buddy is ignored
// or pushed away and halves every annoyHalfLife. Because it is part of the
// state, pestering now makes Buddy's future states worse, which is what lets
// Q-learning discover that spamming does not pay.
type Annoyance struct {
	level float64
	at    time.Time
}

// Level returns the annoyance at now.
func (a *Annoyance) Level(now time.Time) float64 {
	if a.at.IsZero() || !now.After(a.at) {
		return a.level
	}
	return a.level * math.Exp2(-float64(now.Sub(a.at))/float64(annoyHalfLife))
}

// Observe updates the annoyance after an action and its reaction.
func (a *Annoyance) Observe(now time.Time, act Action, r Reaction) {
	var d float64
	switch r {
	case Clicked:
		d = annoyClick
	case Dragged:
		d = annoyDrag
	case Dismissed:
		d = annoyDismiss
	case Ignored:
		if act.Attention() {
			d = annoyIgnored
		}
	}
	a.level = max(0, a.Level(now)+d)
	a.at = now
}
