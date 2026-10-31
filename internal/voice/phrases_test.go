package voice

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"buddy/internal/activity"
	"buddy/internal/rl"
)

// library lists every pool by name.
func library() map[string][]string {
	pools := map[string][]string{
		"fedup":       fedUp,
		"welcome":     welcome,
		"night":       lateNight,
		"morning":     morning,
		"lunch":       lunch,
		"friday":      friday,
		"weekend":     weekend,
		"switchedAny": switchedAny,
		"evening":     evening,
		"generic":     generic,
	}
	for c := range activity.NumCategories {
		pools["long/"+c.String()] = longSession[c]
		pools["verylong/"+c.String()] = veryLongSession[c]
		if len(switched[c]) > 0 {
			pools["switched/"+c.String()] = switched[c]
		}
	}
	return pools
}

// day returns a noon-ish date in a fixed week with the given weekday.
func day(w time.Weekday) time.Time {
	d := time.Date(2030, time.March, 3, 10, 0, 0, 0, time.UTC) // any start
	for d.Weekday() != w {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

func newRand() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

func TestLibraryLines(t *testing.T) {
	t.Parallel()
	seen := map[string]string{}
	total := 0
	for name, lines := range library() {
		for _, l := range lines {
			total++
			if l == "" {
				t.Errorf("%s: empty line", name)
				continue
			}
			if len(l) > 70 {
				t.Errorf("%s: %q is %d chars", name, l, len(l))
			}
			for _, r := range l {
				if r < 0x20 || r > 0x7e {
					t.Errorf("%s: %q has non-ASCII rune %q", name, l, r)
				}
			}
			if strings.TrimSpace(l) != l {
				t.Errorf("%s: %q has surrounding space", name, l)
			}
			if !strings.ContainsAny(l[len(l)-1:], ".!?") {
				t.Errorf("%s: %q does not end with punctuation", name, l)
			}
			if other, ok := seen[l]; ok {
				t.Errorf("%q is in both %s and %s", l, other, name)
			}
			seen[l] = name
		}
	}
	if total < 120 {
		t.Errorf("library has %d lines, want at least 120", total)
	}
	for c := range activity.NumCategories {
		if n := len(longSession[c]); n < 6 {
			t.Errorf("long/%s has %d lines, want 6", c, n)
		}
		if n := len(veryLongSession[c]); n < 3 {
			t.Errorf("verylong/%s has %d lines, want 3", c, n)
		}
	}
	if len(generic) < 20 {
		t.Errorf("generic has %d lines, want 20", len(generic))
	}
}

func TestChoose(t *testing.T) {
	t.Parallel()
	wed, fri, sat := day(time.Wednesday), day(time.Friday), day(time.Saturday)
	never := -time.Second
	base := func(hour int, inApp time.Duration, cat activity.Category) rl.Observation {
		return rl.Observation{
			Snapshot: activity.Snapshot{Category: cat, InApp: inApp, Back: never},
			Hour:     hour,
		}
	}
	tests := []struct {
		name   string
		o      rl.Observation
		now    time.Time
		want   string
		forced bool
	}{
		{"fed up beats everything", func() rl.Observation {
			o := base(2, 10*time.Second, activity.Code)
			o.Annoyance, o.Back = 1.5, 30*time.Second
			return o
		}(), sat, "fedup", true},
		{"welcome back", func() rl.Observation {
			o := base(2, 3*time.Hour, activity.Game)
			o.Back = 30 * time.Second
			return o
		}(), wed, "welcome", true},
		{"back long ago is not welcome", func() rl.Observation {
			o := base(10, 30*time.Minute, activity.Code)
			o.Back = 10 * time.Minute
			return o
		}(), wed, "generic", false},
		{"irritated is not fed up", func() rl.Observation {
			o := base(10, 30*time.Minute, activity.Code)
			o.Annoyance = 1.4
			return o
		}(), wed, "generic", false},
		{"late night", base(2, time.Minute, activity.Code), wed, "night", false},
		{"midnight", base(0, 30*time.Minute, activity.Code), wed, "night", false},
		{"early morning", base(7, 30*time.Minute, activity.Code), wed, "morning", false},
		{"lunch", base(12, 30*time.Minute, activity.Code), wed, "lunch", false},
		{"lunch beats weekend", base(13, 30*time.Minute, activity.Code), sat, "lunch", false},
		{"friday afternoon", base(16, 3*time.Hour, activity.Code), fri, "friday", false},
		{"friday morning is not friday", base(10, 30*time.Minute, activity.Code), fri, "generic", false},
		{"weekend", base(10, 2*time.Hour, activity.Browser), sat, "weekend", false},
		{"long code", base(10, 90*time.Minute, activity.Code), wed, "long/code", false},
		{"long chat", base(15, time.Hour, activity.Chat), wed, "long/chat", false},
		{"very long game", base(20, 2*time.Hour, activity.Game), wed, "verylong/game", false},
		{"unknown category", base(10, 90*time.Minute, activity.NumCategories+3), wed, "long/other", false},
		{"switched to media", base(10, time.Minute, activity.Media), wed, "switched/media", false},
		{"switched to other", base(10, time.Minute, activity.Other), wed, "switched/other", false},
		{"evening", base(19, 30*time.Minute, activity.Browser), wed, "evening", false},
		{"generic", base(10, 30*time.Minute, activity.Browser), wed, "generic", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := choose(tt.o, tt.now)
			if r.name != tt.want || r.forced != tt.forced {
				t.Errorf("choose = %s (forced %v), want %s (forced %v)", r.name, r.forced, tt.want, tt.forced)
			}
			if len(r.lines) == 0 {
				t.Errorf("pool %s is empty", r.name)
			}
		})
	}
}

func TestPickUsesPool(t *testing.T) {
	t.Parallel()
	const samples = 50
	tests := []struct {
		name    string
		o       rl.Observation
		pool    []string
		minHits int
	}{
		{"fed up always", rl.Observation{Annoyance: 2, Hour: 10, Snapshot: activity.Snapshot{Back: -1}}, fedUp, samples},
		{"welcome always", rl.Observation{Hour: 10, Snapshot: activity.Snapshot{Back: 30 * time.Second}}, welcome, samples},
		{"late night mostly", rl.Observation{Hour: 2, Snapshot: activity.Snapshot{Back: -1}}, lateNight, 35},
		{"very long code mostly", rl.Observation{Hour: 10, Snapshot: activity.Snapshot{Category: activity.Code, InApp: 3 * time.Hour, Back: -1}},
			append(slices.Clone(veryLongSession[activity.Code]), longSession[activity.Code]...), 35},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rng := newRand()
			hits := 0
			for range samples {
				// A fresh picker each time so history never pushes picks elsewhere.
				l := NewPicker("", rng).Pick(tt.o, day(time.Wednesday))
				switch {
				case slices.Contains(tt.pool, l):
					hits++
				case !slices.Contains(generic, l):
					t.Errorf("%q is from neither the pool nor generic", l)
				}
			}
			if hits < tt.minHits {
				t.Errorf("%d of %d picks from the pool, want at least %d", hits, samples, tt.minHits)
			}
		})
	}
}

func TestPickNoRepeat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		o    rl.Observation
	}{
		{"generic", rl.Observation{Hour: 10, Snapshot: activity.Snapshot{InApp: 30 * time.Minute, Back: -1}}},
		{"very long code", rl.Observation{Hour: 10, Snapshot: activity.Snapshot{Category: activity.Code, InApp: 3 * time.Hour, Back: -1}}},
		{"fed up", rl.Observation{Hour: 10, Annoyance: 3, Snapshot: activity.Snapshot{Back: -1}}},
		{"late night", rl.Observation{Hour: 3, Snapshot: activity.Snapshot{InApp: 30 * time.Minute, Back: -1}}},
	}
	now := day(time.Wednesday)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := choose(tt.o, now)
			unique := map[string]bool{}
			for _, l := range append(slices.Clone(r.lines), generic...) {
				unique[l] = true
			}
			n := min(noRepeat, len(unique))
			p := NewPicker("", newRand())
			seen := map[string]int{}
			for i := range n {
				l := p.Pick(tt.o, now)
				if j, ok := seen[l]; ok {
					t.Fatalf("pick %d repeats pick %d: %q", i, j, l)
				}
				seen[l] = i
			}
			// Once everything is used, Buddy keeps going with the stalest lines.
			for range 3 * keepRecent {
				if l := p.Pick(tt.o, now); l == "" {
					t.Fatal("empty pick after exhausting the pool")
				}
			}
			if len(p.recent) > keepRecent {
				t.Errorf("history has %d entries, want at most %d", len(p.recent), keepRecent)
			}
		})
	}
}

func TestPickExhaustedFallsBackToLeastRecent(t *testing.T) {
	t.Parallel()
	o := rl.Observation{Hour: 10, Annoyance: 3, Snapshot: activity.Snapshot{Back: -1}}
	p := NewPicker("", newRand())
	// Fill the window with every fed-up and generic line, oldest first.
	p.recent = append(slices.Clone(generic), fedUp...)
	want := generic[0]
	if got := p.Pick(o, day(time.Wednesday)); got != want {
		t.Errorf("Pick = %q, want the least recent line %q", got, want)
	}
}

func TestSaveAndReload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "phrases.json")
	o := rl.Observation{Hour: 10, Annoyance: 3, Snapshot: activity.Snapshot{Back: -1}}
	now := day(time.Wednesday)

	p := NewPicker(path, newRand())
	first := p.Pick(o, now)
	if err := p.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	q := NewPicker(path, newRand())
	if !slices.Equal(q.recent, []string{first}) {
		t.Fatalf("reloaded history = %q, want [%q]", q.recent, first)
	}
	for i := range len(fedUp) - 1 {
		if l := q.Pick(o, now); l == first {
			t.Fatalf("pick %d after reload repeated %q", i, first)
		}
	}
	if err := q.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d files after Save, want just the history", len(entries))
	}
}

func TestNewPickerBadFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tests := []struct {
		name    string
		content string // "" means no file
	}{
		{"missing", ""},
		{"corrupt", "{not json"},
		{"wrong shape", `{"recent": 42}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, tt.name+".json")
			if tt.content != "" {
				if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			p := NewPicker(path, newRand())
			if len(p.recent) != 0 {
				t.Errorf("history = %q, want empty", p.recent)
			}
			if l := p.Pick(rl.Observation{Hour: 10}, day(time.Wednesday)); l == "" {
				t.Error("empty pick")
			}
		})
	}
}

func TestNewPickerTrimsHistory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "phrases.json")
	p := NewPicker(path, newRand())
	for range 2 * keepRecent {
		p.recent = append(p.recent, generic[0])
	}
	if err := p.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if n := len(NewPicker(path, newRand()).recent); n != keepRecent {
		t.Errorf("reloaded %d entries, want %d", n, keepRecent)
	}
}
