// Command buddysim pre-trains Buddy's two brains, the Q-table and the neural
// network, against a simulated user, compares them, and saves both. Buddy
// then uses whichever scored better.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"buddy/internal/appdir"
	"buddy/internal/rl"
	"buddy/internal/sim"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "buddysim:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := appdir.Default()
	if err != nil {
		return err
	}
	var (
		data      = flag.String("data", dir, "folder to save the brains in")
		brain     = flag.String("brain", "both", "which brain to train: table, network or both")
		tableDays = flag.Int("days", 2000, "simulated days to train the table for")
		netDays   = flag.Int("net-days", 400, "simulated days to train each network for")
		nets      = flag.Int("nets", 4, "how many networks to train side by side; the best is kept")
		explore   = flag.Float64("net-explore", 0.1, "share of random tries while the networks practice")
		hidden    = flag.Int("net-hidden", rl.DefaultHidden, "neurons in each of the network's two hidden layers")
		seed      = flag.Uint64("seed", 1, "random seed")
		force     = flag.Bool("force", false, "overwrite existing brains, losing what Buddy learned from you")
	)
	flag.Parse()
	if *brain != "table" && *brain != "network" && *brain != "both" {
		return fmt.Errorf("-brain must be table, network or both, not %q", *brain)
	}
	if err := os.MkdirAll(*data, 0o755); err != nil {
		return err
	}

	tablePath, netPath := filepath.Join(*data, appdir.QTable), filepath.Join(*data, appdir.Net)
	for _, path := range []string{tablePath, netPath} {
		if _, err := os.Stat(path); err == nil && !*force {
			return fmt.Errorf("%s already exists; pass -force to replace it", path)
		}
	}
	// Every brain is judged on the same simulated days, so scores compare.
	judge := judging{progress: *seed + 1000, final: *seed + 2000}

	var kept []result
	if *brain != "network" {
		r, err := practice(rl.NewAgent(*seed), tablePath, *tableDays, *seed, judge, os.Stdout)
		if err != nil {
			return err
		}
		kept = append(kept, r)
	}
	if *brain != "table" {
		r, err := practiceNets(netPath, *nets, *hidden, *netDays, *explore, *seed, judge)
		if err != nil {
			return err
		}
		kept = append(kept, r)
	}

	fmt.Printf("\n%-8s %8s %7s %7s %7s %7s %7s %10s\n", "brain", "score", "wave", "bounce", "speak", "chirp", "walk", "sent away")
	for _, r := range kept {
		m := r.score.ByAction
		fmt.Printf("%-8s %8.2f %7.1f %7.1f %7.1f %7.1f %7.1f %10.2f\n", r.kind, r.score.Reward,
			m[rl.Wave], m[rl.Bounce], m[rl.Speak], m[rl.Chirp], m[rl.Approach], r.score.Dismissals)
	}
	fmt.Println("(per simulated day, on days no brain practiced on)")
	if len(kept) == 2 {
		winner := kept[0]
		if kept[1].score.Reward > winner.score.Reward {
			winner = kept[1]
		}
		fmt.Printf("Buddy will use the %s.\n", winner.kind)
	}
	return nil
}

// judging holds the seeds of the simulated people brains are judged on:
// one during practice, to pick checkpoints, and a fresh one at the end.
type judging struct{ progress, final uint64 }

type result struct {
	kind  string
	score sim.Score
}

// practice trains a brain for days, keeps its best checkpoint at path, and
// scores that checkpoint on fresh days.
func practice(l rl.Learner, path string, days int, seed uint64, judge judging, out io.Writer) (result, error) {
	fmt.Fprintf(out, "\nTraining the %s for %d simulated days\n", l.Kind(), days)
	fmt.Fprintf(out, "%8s %10s %10s %8s %10s %8s\n", "day", "reward/day", "actions", "clicks", "dismissals", "epsilon")
	report := func(day int) sim.Score {
		e := sim.Evaluate(l, judge.progress, 100)
		fmt.Fprintf(out, "%8d %10.2f %10.1f %8.1f %10.2f %8.3f\n", day, e.Reward, e.Attention, e.Clicks, e.Dismissals, l.Epsilon())
		return e
	}
	start := time.Now()
	user := sim.NewUser(seed)
	report(0)
	// Learning wobbles from checkpoint to checkpoint, the network's most
	// of all, so keep the best checkpoint rather than whichever came last.
	step := max(1, days/10)
	best, bestDay := math.Inf(-1), 0
	for day := 1; day <= days; day++ {
		sim.RunDay(user, l, true)
		if day%step != 0 {
			continue
		}
		if e := report(day); e.Reward > best {
			best, bestDay = e.Reward, day
			if err := l.Save(path); err != nil {
				return result{}, err
			}
		}
	}

	// Picking the best of several checkpoints flatters it, so score the
	// kept brain again on simulated days it has never seen.
	k, err := load(l.Kind(), path, seed)
	if err != nil {
		return result{}, err
	}
	score := sim.Evaluate(k, judge.final, 100)
	k.SetSimScore(score.Reward)
	if err := k.Save(path); err != nil {
		return result{}, err
	}
	fmt.Fprintf(out, "kept the %s from day %d; it scores %.2f on fresh days (%s)\n",
		k.Kind(), bestDay, score.Reward, time.Since(start).Round(time.Second))
	return result{kind: k.Kind(), score: score}, nil
}

// practiceNets trains several networks side by side, each from its own
// random start, and keeps the one that does best on fresh days. Networks
// vary a lot from one start to the next; trying a few smooths that out.
func practiceNets(path string, n, hidden, days int, explore float64, seed uint64, judge judging) (result, error) {
	type try struct {
		res  result
		path string
		log  strings.Builder
		err  error
	}
	tries := make([]*try, n)
	var wg sync.WaitGroup
	for i := range tries {
		t := &try{path: fmt.Sprintf("%s.try%d", path, i)}
		tries[i] = t
		s := seed + uint64(i)*7919
		wg.Go(func() {
			net := rl.NewNetSized(s, hidden)
			net.SetTraining(4, 1)       // a simulated day has thousands of steps; train on every fourth
			net.SetExploration(explore) // keep trying every action, walking included
			fmt.Fprintf(&t.log, "\nNetwork %d of %d", i+1, n)
			t.res, t.err = practice(net, t.path, days, s, judge, &t.log)
		})
	}
	wg.Wait()

	var best *try
	for _, t := range tries {
		fmt.Print(t.log.String())
		if t.err != nil {
			return result{}, t.err
		}
		if best == nil || t.res.score.Reward > best.res.score.Reward {
			best = t
		}
	}
	for _, t := range tries {
		if t != best {
			os.Remove(t.path)
		}
	}
	if err := os.Rename(best.path, path); err != nil {
		return result{}, err
	}
	fmt.Printf("\n%-10s %8s %7s %7s %7s %7s %7s %10s\n", "network", "score", "wave", "bounce", "speak", "chirp", "walk", "sent away")
	for i, t := range tries {
		m := t.res.score.ByAction
		fmt.Printf("%-10d %8.2f %7.1f %7.1f %7.1f %7.1f %7.1f %10.2f\n", i+1, t.res.score.Reward,
			m[rl.Wave], m[rl.Bounce], m[rl.Speak], m[rl.Chirp], m[rl.Approach], t.res.score.Dismissals)
	}
	fmt.Printf("kept network %d of %d\n", slices.Index(tries, best)+1, n)
	return best.res, nil
}

// load reads back a saved brain.
func load(kind, path string, seed uint64) (rl.Learner, error) {
	if kind == "network" {
		return rl.LoadNet(path, seed)
	}
	return rl.Load(path, seed)
}
