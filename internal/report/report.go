// Package report explains in plain text what Buddy has learned: what it
// would do in typical situations, and how its attempts have gone.
package report

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/store"
)

// History is the experience log the report reads.
type History interface {
	Actions(ctx context.Context, since time.Time) ([]store.ActionStats, error)
	Weeks(ctx context.Context, n int) ([]store.Week, error)
}

type situation struct {
	name string
	snap activity.Snapshot
}

// The moments the report asks the brain about.
var situations = []situation{
	{"typing hard", activity.Snapshot{Activity: 0.8, InApp: 30 * time.Minute, Back: -1}},
	{"relaxed", activity.Snapshot{Activity: 0.2, InApp: 30 * time.Minute, Back: -1}},
	{"just switched", activity.Snapshot{Activity: 0.3, InApp: time.Minute, Back: -1}},
	{"2h in", activity.Snapshot{Activity: 0.4, InApp: 2 * time.Hour, Back: -1}},
}

// Write writes the report.
func Write(ctx context.Context, w io.Writer, l rl.Learner, h History, now time.Time) error {
	p := &printer{w: w}
	p.f("What Buddy has learned\n")
	p.f("======================\n\n")
	p.f("Brain: %s", l.Kind())
	if s := l.SimScore(); s != 0 {
		p.f(" (scored %.1f per day against the simulated user)", s)
	}
	p.f("\nExperimenting: %.0f%% of decisions are random tries\n\n", l.Epsilon()*100)

	p.f("What Buddy would do now, on a calm afternoon\n")
	p.f("--------------------------------------------\n")
	p.f("(\"quiet\" means it would leave you alone. A number is how much better\n")
	p.f("than staying quiet it expects the action to go.)\n\n")
	p.table(l, 15, 0)

	p.f("\n...and the same if it thinks you're fed up with it\n")
	p.f("---------------------------------------------------\n\n")
	p.table(l, 15, 2)

	p.f("\nHow its attempts went in the last 30 days\n")
	p.f("-----------------------------------------\n\n")
	acts, err := h.Actions(ctx, now.AddDate(0, 0, -30))
	if err != nil {
		return err
	}
	if len(acts) == 0 {
		p.f("Nothing yet. Buddy only logs what it does, not every quiet moment.\n")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(tw, "action\ttries\trandom tries\tclicked\tdragged away\tsent away\tplayed with\tlate replies\t")
		for _, a := range acts {
			fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t\n", a.Action, a.Tries, a.Explored, a.Clicked, a.Dragged, a.Dismissed, a.Played, a.Late)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	p.f("\nIs it getting better? Week by week\n")
	p.f("----------------------------------\n\n")
	weeks, err := h.Weeks(ctx, 8)
	if err != nil {
		return err
	}
	if len(weeks) == 0 {
		p.f("No weeks yet.\n")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
		fmt.Fprintln(tw, "week of\tattempts\tclicked\tclick rate\tsent away\t")
		for _, wk := range weeks {
			rate := "-"
			if wk.Attempts > 0 {
				rate = fmt.Sprintf("%.0f%%", 100*float64(wk.Clicks)/float64(wk.Attempts))
			}
			fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%d\t\n", wk.Start.Format("Jan 2"), wk.Attempts, wk.Clicks, rate, wk.Dismisses)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		p.f("\nA rising click rate with fewer \"sent away\" means Buddy is learning your moments.\n")
	}
	return p.err
}

// table prints the best action for each app and situation.
func (p *printer) table(l rl.Learner, hour int, annoyance float64) {
	if p.err != nil {
		return
	}
	tw := tabwriter.NewWriter(p.w, 0, 0, 2, ' ', 0)
	head := []string{"app"}
	for _, s := range situations {
		head = append(head, s.name)
	}
	fmt.Fprintln(tw, strings.Join(head, "\t")+"\t")
	for c := activity.Category(0); c < activity.NumCategories; c++ {
		row := []string{c.String()}
		for _, s := range situations {
			snap := s.snap
			snap.Category = c
			o := rl.Observation{Snapshot: snap, Hour: hour, Annoyance: annoyance, SinceAction: time.Hour}
			best := l.Best(o)
			cell := best.String()
			if best != rl.Quiet {
				q := l.Scores(o)
				cell += fmt.Sprintf(" (+%.2f)", q[best]-q[rl.Quiet])
			}
			row = append(row, cell)
		}
		fmt.Fprintln(tw, strings.Join(row, "\t")+"\t")
	}
	p.err = tw.Flush()
}

type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}
