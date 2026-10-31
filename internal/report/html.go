package report

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"image/png"
	"io"
	"math"
	"sort"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/sim"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/ui"
)

//go:embed page.html
var pageHTML string

var page = template.Must(template.New("page").Parse(pageHTML))

// A moment is a made-up situation the page asks Buddy about.
type moment struct {
	name, story string
	o           rl.Observation
}

var moments = []moment{
	{"Deep in code", "You've been typing hard in your editor for 45 minutes.",
		obs(activity.Code, 0.85, 0, 45*time.Minute, 15, 0.2, 40*time.Minute)},
	{"Relaxed browsing", "You're scrolling the web at a gentle pace on a quiet afternoon.",
		obs(activity.Browser, 0.25, 5*time.Second, 20*time.Minute, 16, 0, time.Hour)},
	{"Just switched apps", "You've just opened something new, a minute ago.",
		obs(activity.Other, 0.35, 0, time.Minute, 11, 0, 2*time.Hour)},
	{"Gaming", "You're in the middle of a game, keys and mouse busy.",
		obs(activity.Game, 0.9, 0, time.Hour, 21, 0, time.Hour)},
	{"Late-night video", "It's 1 a.m. and you're watching something, barely touching the mouse.",
		obs(activity.Media, 0.03, 40*time.Second, 40*time.Minute, 1, 0, time.Hour)},
	{"Chatting", "You're catching up with friends in a chat app.",
		obs(activity.Chat, 0.4, 0, 10*time.Minute, 13, 0, 30*time.Minute)},
	{"Two hours in", "Same app for two hours straight, steady but not frantic.",
		obs(activity.Other, 0.4, 0, 2*time.Hour, 17, 0, time.Hour)},
	{"Just sent away", "Relaxed browsing again, but you told Buddy to go away a few minutes ago.",
		obs(activity.Browser, 0.25, 5*time.Second, 20*time.Minute, 16, 2.2, 3*time.Minute)},
}

func obs(c activity.Category, act float64, idle, inApp time.Duration, hour int, annoy float64, since time.Duration) rl.Observation {
	return rl.Observation{
		Snapshot:    activity.Snapshot{Category: c, Activity: act, Idle: idle, InApp: inApp, Back: -1},
		Hour:        hour,
		Annoyance:   annoy,
		SinceAction: since,
	}
}

// Names of the network's inputs, in order: the smooth numbers first, then
// the table's buckets.
var inputNames = []string{
	"app: other", "app: code", "app: browser", "app: game", "app: media", "app: chat",
	"idle time", "input level", "time in app", "time of day (a)", "time of day (b)",
	"annoyance", "since last try", "focused",
	"idle: active", "idle: paused", "idle: idle", "idle: away",
	"input: low", "input: mid", "input: high",
	"in app: <10m", "in app: <1h", "in app: 1h+",
	"time: night", "time: morning", "time: afternoon", "time: evening",
	"mood: calm", "mood: irritated", "mood: fed up",
	"last try: <2m", "last try: <15m", "last try: 15m+",
}

type pageData struct {
	Brain      string
	Portrait   template.URL
	Generated  string
	JSON       template.JS
	QuietScore float64
	SimScore   float64
}

type view struct {
	Brain    string     `json:"brain"`
	Actions  []string   `json:"actions"`
	Margin   float64    `json:"margin"`
	Epsilon  float64    `json:"epsilon"`
	Moments  []momentV  `json:"moments"`
	Network  *networkV  `json:"network,omitempty"`
	Playbook playbookV  `json:"playbook"`
	Outcomes []outcomeV `json:"outcomes"`
	Weeks    []weekV    `json:"weeks"`
}

type momentV struct {
	Name   string      `json:"name"`
	Story  string      `json:"story"`
	Inputs []inputV    `json:"inputs"`
	Gains  []float64   `json:"gains"` // each action's value minus staying quiet's
	Best   int         `json:"best"`
	Why    string      `json:"why"`
	Layers [][]float64 `json:"layers,omitempty"` // network only: activations per layer
	Flows  [][]flowV   `json:"flows,omitempty"`  // network only: strongest signals per layer
}

type inputV struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"` // 0..1 as the network sees it
	Text  string  `json:"text"`  // as a person would say it
}

type flowV struct {
	From int     `json:"from"`
	To   int     `json:"to"`
	V    float64 `json:"v"`
}

type networkV struct {
	Sizes  []int    `json:"sizes"`
	Inputs []string `json:"inputs"`
}

type playbookV struct {
	Apps       []string    `json:"apps"`
	Situations []string    `json:"situations"`
	Times      []string    `json:"times"`
	Cells      [][][]cellV `json:"cells"` // [time][app][situation]
}

type cellV struct {
	Action int     `json:"action"`
	Gain   float64 `json:"gain"`
}

type outcomeV struct {
	Action    string `json:"action"`
	Tries     int    `json:"tries"`
	Clicked   int    `json:"clicked"`
	Played    int    `json:"played"`
	Dragged   int    `json:"dragged"`
	Dismissed int    `json:"dismissed"`
	Ignored   int    `json:"ignored"`
}

type weekV struct {
	Start    string  `json:"start"`
	Attempts int     `json:"attempts"`
	Clicks   int     `json:"clicks"`
	Rate     float64 `json:"rate"`
	SentAway int     `json:"sentAway"`
}

// WriteHTML writes the visual report: a single page with everything inline,
// so it opens offline in any browser.
func WriteHTML(ctx context.Context, w io.Writer, l rl.Learner, h History, now time.Time) error {
	v := view{Brain: l.Kind(), Epsilon: l.Epsilon()}
	for a := range rl.NumActions {
		v.Actions = append(v.Actions, a.String())
	}

	// Look inside the learner under its lock: the brain keeps learning meanwhile.
	inspect := func(f func(rl.Learner)) { f(l) }
	if k, ok := l.(*rl.Locked); ok {
		inspect = k.Peek
	}
	inspect(func(inner rl.Learner) {
		net, _ := inner.(*rl.Net)
		if h, ok := inner.(*rl.Hybrid); ok {
			net = h.Net
		}
		if net != nil {
			v.Margin = net.Margin()
			v.Network = &networkV{Inputs: inputNames}
			for _, layer := range net.Online.Layers {
				if len(v.Network.Sizes) == 0 {
					v.Network.Sizes = append(v.Network.Sizes, layer.In)
				}
				v.Network.Sizes = append(v.Network.Sizes, layer.Out)
			}
		}
		for _, m := range moments {
			v.Moments = append(v.Moments, describe(inner, net, m))
		}
		v.Playbook = playbook(inner)
	})

	acts, err := h.Actions(ctx, now.AddDate(0, 0, -30))
	if err != nil {
		return err
	}
	for _, a := range acts {
		if a.Action == "quiet" {
			continue
		}
		o := outcomeV{Action: a.Action, Tries: a.Tries, Clicked: a.Clicked, Played: a.Played, Dragged: a.Dragged, Dismissed: a.Dismissed}
		o.Ignored = max(0, a.Tries-a.Clicked-a.Played-a.Dragged-a.Dismissed)
		v.Outcomes = append(v.Outcomes, o)
	}
	weeks, err := h.Weeks(ctx, 12)
	if err != nil {
		return err
	}
	for _, wk := range weeks {
		rate := 0.0
		if wk.Attempts > 0 {
			rate = float64(wk.Clicks) / float64(wk.Attempts)
		}
		v.Weeks = append(v.Weeks, weekV{Start: wk.Start.Format("Jan 2"), Attempts: wk.Attempts, Clicks: wk.Clicks, Rate: rate, SentAway: wk.Dismisses})
	}

	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	portrait, err := portrait()
	if err != nil {
		return err
	}
	return page.Execute(w, pageData{
		Brain:      l.Kind(),
		Portrait:   portrait,
		Generated:  now.Format("Monday, January 2, 2006 at 15:04"),
		JSON:       template.JS(data),
		QuietScore: sim.Evaluate(quiet{}, 4242, 30).Reward,
		SimScore:   l.SimScore(),
	})
}

// describe asks the brain about one moment and explains its answer.
func describe(l rl.Learner, net *rl.Net, m moment) momentV {
	q := l.Scores(m.o)
	best := l.Best(m.o)
	mv := momentV{Name: m.name, Story: m.story, Best: int(best)}
	for a := range rl.NumActions {
		mv.Gains = append(mv.Gains, q[a]-q[rl.Quiet])
	}
	x := rl.Inputs(m.o)
	for i, name := range inputNames {
		mv.Inputs = append(mv.Inputs, inputV{Name: name, Value: x[i], Text: inputText(i, m.o)})
	}

	topGain, topAction := math.Inf(-1), rl.Quiet
	for a := rl.Quiet + 1; a < rl.NumActions; a++ {
		if g := q[a] - q[rl.Quiet]; g > topGain {
			topGain, topAction = g, a
		}
	}
	switch {
	case best != rl.Quiet:
		mv.Why = fmt.Sprintf("Buddy expects to %s to go %.2f better than staying quiet, so that's what he does.", verb(best), q[best]-q[rl.Quiet])
	case topGain > 0 && net != nil:
		mv.Why = fmt.Sprintf("His best idea, to %s, looks only %.2f better than staying quiet. The network wants at least %.2f before it interrupts you, so he stays quiet.", verb(topAction), topGain, net.Margin())
	default:
		mv.Why = fmt.Sprintf("Every attempt looks worse than staying quiet (the closest, to %s, is %.2f behind), so he leaves you alone.", verb(topAction), -topGain)
	}

	if net != nil {
		mv.Layers = net.Activations(m.o)
		mv.Flows = flows(net, mv.Layers)
	}
	return mv
}

// flows finds, for each layer, the connections carrying the most signal in
// this moment: weight times how active the neuron feeding it is. Each neuron
// sends at most a few, so one very active neuron can't take every line and
// hide the rest.
func flows(net *rl.Net, acts [][]float64) [][]flowV {
	const keep, perNeuron = 24, 3
	var out [][]flowV
	for li, layer := range net.Online.Layers {
		in := acts[li]
		var fs []flowV
		for o := range layer.Out {
			for i := range layer.In {
				if c := layer.W[o*layer.In+i] * in[i]; c != 0 {
					fs = append(fs, flowV{From: i, To: o, V: c})
				}
			}
		}
		sort.Slice(fs, func(a, b int) bool { return math.Abs(fs[a].V) > math.Abs(fs[b].V) })
		sent := make([]int, layer.In)
		var kept []flowV
		for _, f := range fs {
			if len(kept) == keep {
				break
			}
			if sent[f.From] < perNeuron {
				sent[f.From]++
				kept = append(kept, f)
			}
		}
		out = append(out, kept)
	}
	return out
}

func playbook(l rl.Learner) playbookV {
	p := playbookV{Times: []string{"Morning", "Afternoon", "Evening", "Night"}}
	hours := []int{9, 15, 20, 1}
	type sit struct {
		name  string
		act   float64
		inApp time.Duration
	}
	sits := []sit{{"typing hard", 0.85, 30 * time.Minute}, {"relaxed", 0.2, 30 * time.Minute}, {"just switched", 0.3, time.Minute}, {"2 hours in", 0.4, 2 * time.Hour}}
	for _, s := range sits {
		p.Situations = append(p.Situations, s.name)
	}
	for c := range activity.NumCategories {
		p.Apps = append(p.Apps, c.String())
	}
	for _, h := range hours {
		var byApp [][]cellV
		for c := range activity.NumCategories {
			var row []cellV
			for _, s := range sits {
				o := obs(c, s.act, 0, s.inApp, h, 0, time.Hour)
				q := l.Scores(o)
				b := l.Best(o)
				row = append(row, cellV{Action: int(b), Gain: q[b] - q[rl.Quiet]})
			}
			byApp = append(byApp, row)
		}
		p.Cells = append(p.Cells, byApp)
	}
	return p
}

func verb(a rl.Action) string {
	switch a {
	case rl.Fidget:
		return "fidget"
	case rl.Wave:
		return "wave"
	case rl.Bounce:
		return "bounce"
	case rl.Speak:
		return "say something"
	case rl.Chirp:
		return "chirp"
	case rl.Approach:
		return "walk over to you"
	case rl.Peek:
		return "peek out at you"
	case rl.Emote:
		return "show a little bubble"
	}
	return "stay quiet"
}

func inputText(i int, o rl.Observation) string {
	switch {
	case i < 6:
		if int(o.Category) == i {
			return "yes"
		}
		return "no"
	case i == 6:
		return roughly(o.Idle)
	case i == 7:
		return fmt.Sprintf("%.0f%% of the last minute", o.Activity*100)
	case i == 8:
		return roughly(o.InApp)
	case i == 9, i == 10:
		return fmt.Sprintf("%02d:00", o.Hour)
	case i == 11:
		return []string{"calm", "a bit irritated", "fed up"}[min(int(o.Annoyance+0.5), 2)]
	case i == 12:
		return roughly(o.SinceAction)
	case i == 13:
		if o.Focused() {
			return "yes"
		}
		return "no"
	default: // a bucket
		if rl.Inputs(o)[i] > 0 {
			return "yes"
		}
		return "no"
	}
}

func roughly(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%.0f seconds", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%.0f minutes", d.Minutes())
	default:
		return fmt.Sprintf("%.1f hours", d.Hours())
	}
}

// portrait is Buddy's icon as an inline image.
func portrait() (template.URL, error) {
	img, err := ui.IconImage(128)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return "", err
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())), nil
}

// quiet is a Buddy that never says hi, the baseline for the practice score.
type quiet struct{}

func (quiet) Act(rl.Observation) (rl.Action, bool)                     { return rl.Quiet, false }
func (quiet) Best(rl.Observation) rl.Action                            { return rl.Quiet }
func (quiet) Scores(rl.Observation) [rl.NumActions]float64             { return [rl.NumActions]float64{} }
func (quiet) Learn(rl.Observation, rl.Action, float64, rl.Observation) {}
func (quiet) Epsilon() float64                                         { return 0 }
func (quiet) SimScore() float64                                        { return 0 }
func (quiet) SetSimScore(float64)                                      {}
func (quiet) Kind() string                                             { return "quiet" }
func (quiet) Save(string) error                                        { return nil }
