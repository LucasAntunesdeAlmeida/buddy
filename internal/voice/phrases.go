// Package voice gives Buddy something to say and, optionally, a way to say it
// aloud with the speech voices built into Windows.
package voice

import (
	"math/rand/v2"
	"time"

	"buddy/internal/activity"
	"buddy/internal/rl"
)

var (
	longSession = map[activity.Category][]string{
		activity.Code:    {"You've been coding a while. Stretch?", "Long session! Water break?", "Still on that bug? Try explaining it to me."},
		activity.Browser: {"Find anything good?", "That's a lot of tabs, probably.", "Rabbit hole?"},
		activity.Game:    {"GG. Hydrate!", "One more round?"},
		activity.Media:   {"Good episode?", "Popcorn status?"},
		activity.Chat:    {"Chatty today!", "Say hi from me."},
		activity.Other:   {"How's it going?", "Need a break?"},
	}
	switched = []string{"Oh, something new!", "What are we doing now?", "New task?"}
	night    = []string{"It's late... sleep soon?", "The moon is out. So am I.", "Night owl mode."}
	morning  = []string{"Good morning!", "Coffee first?", "Fresh start!"}
	generic  = []string{"Hi!", "Hey, I'm here.", "Psst.", "Hello there!", "*waves*"}
)

// Pick chooses a line that fits what the user is doing.
func Pick(o rl.Observation, rng *rand.Rand) string {
	switch {
	case o.Hour >= 0 && o.Hour < 5:
		return one(night, rng)
	case o.Hour >= 6 && o.Hour < 10 && o.SinceAction > time.Hour:
		return one(morning, rng)
	case o.InApp < 3*time.Minute:
		return one(switched, rng)
	case o.InApp >= time.Hour:
		return one(longSession[o.Category], rng)
	default:
		return one(generic, rng)
	}
}

func one(lines []string, rng *rand.Rand) string {
	if len(lines) == 0 {
		return one(generic, rng)
	}
	return lines[rng.IntN(len(lines))]
}
