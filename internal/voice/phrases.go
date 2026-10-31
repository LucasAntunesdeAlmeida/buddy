// Package voice gives Buddy something to say and, optionally, a way to say it
// aloud with the speech voices built into Windows.
package voice

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
)

const (
	// noRepeat is how many recent picks a line must stay out of before it
	// can be said again, as long as something fresher is available.
	noRepeat = 40
	// keepRecent is how many picks the history remembers.
	keepRecent = 60
	// genericMix is the chance of saying a generic line instead of one from
	// the pool that fits, so Buddy isn't too predictable.
	genericMix = 0.15
)

var (
	fedUp = []string{
		"I'll keep it short.",
		"Just checking in, then I'll hush.",
		"Sorry. Quiet mode now.",
		"Tiny wave. That's all.",
		"I'll be over here.",
		"Not a peep after this one.",
		"Okay, okay. Shh.",
		"Just a little hello.",
	}
	welcome = []string{
		"Welcome back!",
		"Oh hey, you're back!",
		"There you are! I kept your seat warm.",
		"Back already? I barely finished my nap.",
		"Hi again! Nothing exploded while you were gone.",
		"You're back! My sprout perked right up.",
		"Missed you. Well, a little. Okay, a lot.",
		"Good timing, I was getting bored.",
	}
	lateNight = []string{
		"It's late... sleep soon?",
		"The moon is out. So am I.",
		"Night owl mode, huh?",
		"Even my sprout is drooping. Bedtime?",
		"Your pillow probably misses you.",
		"Late-night ideas are great. So is sleep.",
		"The stars are out. Maybe wrap up soon?",
		"I'll keep watch if you want to rest.",
	}
	morning = []string{
		"Good morning!",
		"Coffee first?",
		"Fresh start!",
		"Morning! My sprout is soaking up the sun.",
		"Rise and shine. Mostly shine.",
		"Early bird! Want a worm? Just kidding.",
		"New day, new chances.",
	}
	lunch = []string{
		"Lunch o'clock?",
		"Is that a tummy rumble or the fan?",
		"Food break? I'll guard the desk.",
		"Midday already! Snack time?",
		"Plants need water. Humans need lunch.",
		"Eat something tasty, okay?",
	}
	friday = []string{
		"Friday afternoon! Almost there.",
		"It's Friday. You've earned a slow one.",
		"The weekend is peeking around the corner.",
		"Friday vibes: on.",
		"Wrap it up, Friday is calling!",
	}
	weekend = []string{
		"Happy weekend!",
		"Weekend mode! Taking it easy?",
		"No alarms today, I hope.",
		"Saturday or Sunday, either way, hi!",
		"Lazy weekend? I support it fully.",
		"Weekends are for naps. I've done three.",
	}
	longSession = map[activity.Category][]string{
		activity.Code: {
			"You've been coding a while. Stretch?",
			"Long session! Water break?",
			"Still on that bug? Try explaining it to me.",
			"Your code looks busy. So do you.",
			"Commit early, stretch often.",
			"I believe in your semicolons.",
		},
		activity.Browser: {
			"Find anything good?",
			"That's a lot of tabs, probably.",
			"Rabbit hole?",
			"Learning something cool?",
			"Any good finds to share?",
			"The internet is big. You're doing great.",
		},
		activity.Game: {
			"GG. Hydrate!",
			"One more round?",
			"You're on a roll!",
			"Nice moves. I think. I'm small, hard to see.",
			"Winning? Losing? Having fun is what counts.",
			"I'd cheer louder, but I'm tiny.",
		},
		activity.Media: {
			"Good episode?",
			"Popcorn status?",
			"Is it good? Don't spoil it.",
			"I'm watching too. Kind of.",
			"Cozy viewing time.",
			"Plot twist yet?",
		},
		activity.Chat: {
			"Chatty today!",
			"Say hi from me.",
			"Lots to talk about, huh?",
			"Hope the conversation's a good one.",
			"Tell them Buddy says hello.",
			"Typing away! Good chat?",
		},
		activity.Other: {
			"How's it going?",
			"Need a break?",
			"You've been at this a while. Nice focus.",
			"Steady work! I'm impressed.",
			"Still going strong?",
			"Whatever this is, you're on it.",
		},
	}
	veryLongSession = map[activity.Category][]string{
		activity.Code: {
			"Two hours of code! Your eyes deserve a break.",
			"Time to stand up. The bugs will wait.",
			"Big session. Walk around, then come back fresh.",
		},
		activity.Browser: {
			"Long scroll! Maybe rest your eyes a bit?",
			"The tabs will still be here after a break.",
			"Two hours online. Fancy a stretch?",
		},
		activity.Game: {
			"Epic session! Pause and stretch those hands?",
			"Your thumbs could use a little vacation.",
			"Save point? Perfect time for a snack.",
		},
		activity.Media: {
			"Marathon mode! Maybe pause and stretch?",
			"Your eyes earned an intermission.",
			"Next episode can wait a minute. Water?",
		},
		activity.Chat: {
			"Long chat! Rest your fingers for a bit?",
			"Great talk, maybe take a breather?",
			"Two hours of chatting! Stretch break?",
		},
		activity.Other: {
			"Two hours in! A short break might help.",
			"Up you get, stretch a little!",
			"Big stretch of work. Time for a breather?",
		},
	}
	switched = map[activity.Category][]string{
		activity.Code: {
			"Back to the code mines?",
			"Time to build something?",
			"Let's make the computer do things.",
		},
		activity.Browser: {
			"Looking something up?",
			"Off to explore the web?",
		},
		activity.Game: {
			"Game time? Have fun!",
			"Oh, what are we playing?",
		},
		activity.Media: {
			"Show time? Enjoy!",
			"Ooh, what are we watching?",
		},
		activity.Chat: {
			"Catching up with folks?",
			"Who are we talking to?",
		},
	}
	switchedAny = []string{
		"Oh, something new!",
		"What are we doing now?",
		"New task?",
		"Ooh, a change of scenery.",
		"Switching it up, nice.",
	}
	evening = []string{
		"Good evening!",
		"The sun's clocking out. Are you?",
		"Evening already? Time flies.",
		"Cozy evening vibes.",
		"Hope your day was a good one.",
		"Dinner plans?",
		"Winding down soon?",
	}
	generic = []string{
		"Hi!",
		"Hey, I'm here.",
		"Psst.",
		"Hello there!",
		"Just saying hi.",
		"You're doing great.",
		"I watered my sprout today.",
		"Being small is great. Lots of room.",
		"I took a nap. It was excellent.",
		"Don't mind me, just vibing.",
		"Is it snack time? It feels like snack time.",
		"I'm rooting for you. Get it? Sprout?",
		"Hey! Nothing important. Just hey.",
		"Did you know I'm mostly leaf?",
		"Still here, still green.",
		"Fun fact: I can't whistle.",
		"Hope your day's going well.",
		"I'm waving. Tiny arms, big wave.",
		"I'm small but my cheer is big.",
		"Blink twice if you need a break. Kidding!",
		"Thinking happy thoughts for you.",
		"My sprout wiggled. That means good luck.",
		"Hello from the corner of your screen!",
		"Need anything? I'm great at moral support.",
		"I practiced my smile today. How is it?",
		"Mint green is a lucky color, you know.",
		"I'd offer a hug, but I'm very tiny.",
		"Just a sprout saying hello.",
		"You're my favorite human on this screen.",
		"Somewhere, a cat is napping. Good for it.",
	}
)

// route is the pool chosen for a moment. Forced pools are never mixed with
// generic lines.
type route struct {
	name   string
	lines  []string
	forced bool
}

// category clamps c to a known category.
func category(c activity.Category) activity.Category {
	if c >= activity.NumCategories {
		return activity.Other
	}
	return c
}

// choose picks the pool that fits the moment, first match wins.
func choose(o rl.Observation, now time.Time) route {
	hour := (o.Hour%24 + 24) % 24
	day := now.Weekday()
	cat := category(o.Category)
	switch {
	case o.Annoyance >= 1.5:
		return route{name: "fedup", lines: fedUp, forced: true}
	case o.Back >= 0 && o.Back < 2*time.Minute:
		return route{name: "welcome", lines: welcome, forced: true}
	case hour <= 4:
		return route{name: "night", lines: lateNight}
	case hour <= 8:
		return route{name: "morning", lines: morning}
	case hour == 12 || hour == 13:
		return route{name: "lunch", lines: lunch}
	case day == time.Friday && hour >= 15:
		return route{name: "friday", lines: friday}
	case day == time.Saturday || day == time.Sunday:
		return route{name: "weekend", lines: weekend}
	case o.InApp >= 2*time.Hour:
		lines := append(append([]string{}, veryLongSession[cat]...), longSession[cat]...)
		return route{name: "verylong/" + cat.String(), lines: lines}
	case o.InApp >= time.Hour:
		return route{name: "long/" + cat.String(), lines: longSession[cat]}
	case o.InApp < 3*time.Minute:
		lines := append(append([]string{}, switched[cat]...), switchedAny...)
		return route{name: "switched/" + cat.String(), lines: lines}
	case hour >= 18:
		return route{name: "evening", lines: evening}
	default:
		return route{name: "generic", lines: generic}
	}
}

// Picker chooses what Buddy says and remembers recent lines so it doesn't
// repeat itself. It is not safe for concurrent use.
type Picker struct {
	path   string
	rng    *rand.Rand
	recent []string // oldest first
}

// history is the on-disk form of a Picker's memory.
type history struct {
	Recent []string `json:"recent"`
}

// NewPicker loads the recent-lines history from path. A missing or corrupt
// file just means Buddy starts with a fresh memory.
func NewPicker(path string, rng *rand.Rand) *Picker {
	if rng == nil {
		rng = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	p := &Picker{path: path, rng: rng}
	if path == "" {
		return p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	var h history
	if err := json.Unmarshal(b, &h); err != nil {
		return p
	}
	for _, l := range h.Recent {
		if l != "" {
			p.recent = append(p.recent, l)
		}
	}
	p.trim()
	return p
}

// Pick returns a line fitting the moment. now supplies the weekday; the time
// of day comes from o.Hour.
func (p *Picker) Pick(o rl.Observation, now time.Time) string {
	r := choose(o, now)
	lines := r.lines
	if !r.forced && p.rng.Float64() < genericMix {
		lines = generic
	}
	line := p.pickFrom(lines)
	p.recent = append(p.recent, line)
	p.trim()
	return line
}

// pickFrom returns a random line from lines not said in the last noRepeat
// picks, then from the generic lines, then the least recently said one.
func (p *Picker) pickFrom(lines []string) string {
	last := map[string]int{}
	for i, l := range p.recent {
		last[l] = i
	}
	cutoff := len(p.recent) - noRepeat
	fresh := func(pool []string) []string {
		var out []string
		for _, l := range pool {
			if i, ok := last[l]; !ok || i < cutoff {
				out = append(out, l)
			}
		}
		return out
	}
	if f := fresh(lines); len(f) > 0 {
		return f[p.rng.IntN(len(f))]
	}
	if f := fresh(generic); len(f) > 0 {
		return f[p.rng.IntN(len(f))]
	}
	best, bestAt := "", len(p.recent)
	for _, pool := range [][]string{lines, generic} {
		for _, l := range pool {
			if i := last[l]; i < bestAt {
				best, bestAt = l, i
			}
		}
	}
	return best
}

// trim drops the oldest history beyond keepRecent.
func (p *Picker) trim() {
	if n := len(p.recent) - keepRecent; n > 0 {
		p.recent = append([]string(nil), p.recent[n:]...)
	}
}

// Save writes the history to the picker's path atomically.
func (p *Picker) Save() error {
	if p.path == "" {
		return nil
	}
	b, err := json.Marshal(history{Recent: p.recent})
	if err != nil {
		return fmt.Errorf("encode phrase history: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(p.path), ".phrases-*.json")
	if err != nil {
		return fmt.Errorf("save phrase history: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("save phrase history: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save phrase history: %w", err)
	}
	if err := os.Rename(tmp.Name(), p.path); err != nil {
		return fmt.Errorf("save phrase history: %w", err)
	}
	return nil
}
