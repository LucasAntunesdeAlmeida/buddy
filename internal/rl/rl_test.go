package rl

import (
	"path/filepath"
	"testing"
	"time"

	"buddy/internal/activity"
)

func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	seen := map[State]bool{}
	for c := range activity.NumCategories {
		for _, idle := range []time.Duration{0, time.Minute, 3 * time.Minute, time.Hour} {
			for _, act := range []float64{0, 0.3, 0.9} {
				for _, hour := range []int{1, 7, 13, 23} {
					o := Observation{
						Snapshot:    activity.Snapshot{Category: c, Idle: idle, Activity: act, InApp: 2 * time.Hour},
						Hour:        hour,
						Annoyance:   2,
						SinceAction: time.Minute,
					}
					s := Encode(o)
					if int(s) >= NumStates {
						t.Fatalf("Encode(%+v) = %d, want < %d", o, s, NumStates)
					}
					if got, want := s.Features(), o.Features(); got != want {
						t.Fatalf("round trip of %+v: got %+v, want %+v", o, got, want)
					}
					seen[s] = true
				}
			}
		}
	}
	if want := int(activity.NumCategories) * 4 * 3 * 4; len(seen) != want {
		t.Errorf("got %d distinct states, want %d", len(seen), want)
	}
}

func TestReward(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		act     Action
		r       Reaction
		focused bool
		want    float64
	}{
		{"quiet costs nothing", Quiet, Ignored, false, 0},
		{"ignored wave costs a little", Wave, Ignored, false, -costAttention},
		{"clicked speech pays", Speak, Clicked, false, rewardClick - costAttention},
		{"interrupting focus is penalized", Chirp, Ignored, true, -costAttention - penaltyFocused},
		{"dismissal hurts", Bounce, Dismissed, false, rewardDismiss - costAttention},
		{"fidgeting is not an interruption", Fidget, Ignored, true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Reward(tt.act, tt.r, tt.focused); got != tt.want {
				t.Errorf("Reward(%v, %v, %v) = %v, want %v", tt.act, tt.r, tt.focused, got, tt.want)
			}
		})
	}
}

func TestAnnoyanceDecays(t *testing.T) {
	t.Parallel()
	var a Annoyance
	t0 := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	a.Observe(t0, Wave, Dismissed)
	if got := a.Level(t0); got != annoyDismiss {
		t.Fatalf("level after dismissal = %v, want %v", got, annoyDismiss)
	}
	if got := a.Level(t0.Add(annoyHalfLife)); got < annoyDismiss/2-1e-9 || got > annoyDismiss/2+1e-9 {
		t.Errorf("level after one half-life = %v, want %v", got, annoyDismiss/2)
	}
	a.Observe(t0, Wave, Clicked)
	a.Observe(t0, Wave, Clicked)
	a.Observe(t0, Wave, Clicked)
	a.Observe(t0, Wave, Clicked)
	a.Observe(t0, Wave, Clicked)
	if got := a.Level(t0); got != 0 {
		t.Errorf("level never goes below zero, got %v", got)
	}
}

func TestUnknownStateIsQuiet(t *testing.T) {
	t.Parallel()
	a := NewAgent(1)
	if got := a.Greedy(42); got != Quiet {
		t.Errorf("Greedy on an unseen state = %v, want quiet", got)
	}
}

func TestFocusedExplorationIsCalm(t *testing.T) {
	t.Parallel()
	a := NewAgent(1)
	for range 5000 {
		if act, _ := a.Choose(7, true); act.Attention() {
			t.Fatalf("Choose while focused returned %v", act)
		}
	}
}

func TestUpdateLearnsAction(t *testing.T) {
	t.Parallel()
	a := NewAgent(1)
	const s, next State = 10, 11
	for range 50 {
		a.Update(s, Wave, 1, next)
		a.Update(s, Bounce, -1, next)
	}
	if got := a.Greedy(s); got != Wave {
		t.Errorf("Greedy after rewarding wave = %v, want wave (values %v)", got, a.Values(s))
	}
	if v := a.Values(s)[Wave]; v < 0.9 || v > 1.1 {
		t.Errorf("value of a reliably rewarded action = %v, want about 1", v)
	}
}

func TestSaveLoad(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "q.json")
	a := NewAgent(1)
	a.Update(3, Speak, 1, 4)
	if err := a.Save(path); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if b.Steps != a.Steps || b.Values(3) != a.Values(3) {
		t.Errorf("loaded agent differs: steps %d vs %d, values %v vs %v", b.Steps, a.Steps, b.Values(3), a.Values(3))
	}
	fresh, err := Load(filepath.Join(t.TempDir(), "missing.json"), 1)
	if err != nil || fresh.Steps != 0 {
		t.Errorf("Load of a missing file = %v, %v; want a fresh agent", fresh, err)
	}
}
