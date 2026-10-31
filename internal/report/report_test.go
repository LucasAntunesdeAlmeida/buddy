package report

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/store"
)

type fakeHistory struct {
	acts  []store.ActionStats
	weeks []store.Week
}

func (f fakeHistory) Actions(context.Context, time.Time) ([]store.ActionStats, error) {
	return f.acts, nil
}

func (f fakeHistory) Weeks(context.Context, int) ([]store.Week, error) { return f.weeks, nil }

func TestWrite(t *testing.T) {
	t.Parallel()
	h := fakeHistory{
		acts:  []store.ActionStats{{Action: "wave", Tries: 10, Clicked: 4, Dismissed: 1}},
		weeks: []store.Week{{Start: time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC), Attempts: 10, Clicks: 4, Dismisses: 1}},
	}
	var b strings.Builder
	if err := Write(t.Context(), &b, rl.NewNet(1), h, time.Now()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{"Brain: network", "typing hard", "browser", "wave", "40%"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
}

func TestWriteEmpty(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	if err := Write(t.Context(), &b, rl.NewAgent(1), fakeHistory{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Nothing yet") {
		t.Errorf("an empty log should say so:\n%s", b.String())
	}
}

func TestWriteHTML(t *testing.T) {
	t.Parallel()
	h := fakeHistory{
		acts:  []store.ActionStats{{Action: "wave", Tries: 10, Clicked: 4, Dismissed: 1}},
		weeks: []store.Week{{Start: time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC), Attempts: 10, Clicks: 4}},
	}
	for _, l := range []rl.Learner{rl.NewLocked(rl.NewNet(1)), rl.NewAgent(1)} {
		var b strings.Builder
		if err := WriteHTML(t.Context(), &b, l, h, time.Now()); err != nil {
			t.Fatalf("%s: %v", l.Kind(), err)
		}
		out := b.String()
		for _, want := range []string{"<!DOCTYPE html>", "data:image/png;base64,", `"brain":"` + l.Kind() + `"`, "Deep in code", "Buddy's playbook"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s report lacks %q", l.Kind(), want)
			}
		}
		if strings.Contains(out, "http://") && !strings.Contains(out, `"http://www.w3.org/2000/svg"`) {
			t.Errorf("%s report links to the web; it must work offline", l.Kind())
		}
		shape := fmt.Sprintf(`"network":{"sizes":[%d,%d,%d,%d]`, rl.NumInputs, rl.DefaultHidden, rl.DefaultHidden, rl.NumActions)
		if l.Kind() == "network" && !strings.Contains(out, shape) {
			t.Error("network report lacks the network's shape")
		}
	}
}

func TestFlowsSpreadAcrossNeurons(t *testing.T) {
	t.Parallel()
	o := rl.Observation{Snapshot: activity.Snapshot{Activity: 0.6, InApp: 20 * time.Minute, Back: -1}, Hour: 15}
	// A new network's output weights start at zero; a little study gives
	// every layer some signal.
	table := rl.NewAgent(1)
	table.Learn(o, rl.Wave, 1, o)
	net := rl.NewNet(1)
	net.Distill(table, []rl.Observation{o}, 20, rand.New(rand.NewPCG(1, 2)))
	for li, fs := range flows(net, net.Activations(o)) {
		if len(fs) == 0 {
			t.Errorf("layer %d: no lines", li)
		}
		sent := map[int]int{}
		for _, f := range fs {
			if sent[f.From]++; sent[f.From] > 3 {
				t.Errorf("layer %d: neuron %d sends more than 3 lines", li, f.From)
			}
		}
	}
}
