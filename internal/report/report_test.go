package report

import (
	"context"
	"strings"
	"testing"
	"time"

	"buddy/internal/rl"
	"buddy/internal/store"
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
