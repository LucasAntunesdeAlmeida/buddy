// Command buddysim lets Buddy practice against a simulated person before he
// meets you. First the table learns by trial and error, then the neural
// network learns from the table and from its own mistakes. Buddy runs on the
// network; the table stays on as his diary.
package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
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
		tableDays = flag.Int("days", 2000, "simulated days for the table to practice")
		rounds    = flag.Int("rounds", 6, "rounds in which the network drives and learns from its mistakes")
		hidden    = flag.Int("net-hidden", rl.DefaultHidden, "neurons in each of the network's two hidden layers")
		seed      = flag.Uint64("seed", 1, "random seed")
		force     = flag.Bool("force", false, "overwrite existing brains, losing what Buddy learned from you")
	)
	flag.Parse()
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

	quiet := sim.Evaluate(quietBuddy{}, judge.final, 100)
	tr, err := practice(rl.NewAgent(*seed), tablePath, *tableDays, *seed, judge, os.Stdout)
	if err != nil {
		return err
	}
	table, err := rl.Load(tablePath, *seed)
	if err != nil {
		return err
	}
	nr, err := study(table, netPath, *hidden, *rounds, *seed, judge)
	if err != nil {
		return err
	}

	fmt.Printf("\n%-9s %8s %7s %7s %7s %7s %7s %7s %7s %10s\n", "", "score", "wave", "bounce", "speak", "chirp", "walk", "peek", "emote", "sent away")
	for _, r := range []result{{"silent", quiet}, tr, nr} {
		m := r.score.ByAction
		fmt.Printf("%-9s %8.2f %7.1f %7.1f %7.1f %7.1f %7.1f %7.1f %7.1f %10.2f\n", r.kind, r.score.Reward,
			m[rl.Wave], m[rl.Bounce], m[rl.Speak], m[rl.Chirp], m[rl.Approach], m[rl.Peek], m[rl.Emote], r.score.Dismissals)
	}
	fmt.Println("(per simulated day, on days neither brain practiced on; \"silent\" is a Buddy who never says hi)")
	fmt.Println("Buddy will decide with the network; the table keeps his diary.")
	return nil
}

// judging holds the seeds of the simulated people brains are judged on:
// one during practice, to pick checkpoints, and a fresh one at the end.
type judging struct{ progress, final uint64 }

type result struct {
	kind  string
	score sim.Score
}

// practice trains the table by trial and error for days, keeps its best
// checkpoint at path, and scores that checkpoint on fresh days.
func practice(l rl.Learner, path string, days int, seed uint64, judge judging, out io.Writer) (result, error) {
	fmt.Fprintf(out, "The table practices for %d simulated days\n", days)
	fmt.Fprintf(out, "%8s %10s %10s %8s %10s\n", "day", "reward/day", "attempts", "clicks", "sent away")
	report := func(day int) sim.Score {
		e := sim.Evaluate(l, judge.progress, 100)
		fmt.Fprintf(out, "%8d %10.2f %10.1f %8.1f %10.2f\n", day, e.Reward, e.Attention, e.Clicks, e.Dismissals)
		return e
	}
	start := time.Now()
	user := sim.NewUser(seed)
	report(0)
	// Learning wobbles from checkpoint to checkpoint, so keep the best one
	// rather than whichever came last.
	step := max(1, days/10)
	best := math.Inf(-1)
	for day := 1; day <= days; day++ {
		sim.RunDay(user, l, true)
		if day%step != 0 {
			continue
		}
		if e := report(day); e.Reward > best {
			best = e.Reward
			if err := l.Save(path); err != nil {
				return result{}, err
			}
		}
	}
	return finish("table", path, seed, judge, start, out)
}

// study teaches the network from the table: first it copies the table's
// scores on moments the table meets, then for rounds it drives Buddy
// itself, and the table labels the moments it gets into. A copy alone goes
// wrong: small errors lead to moments the table rarely saw, where the copy
// is worse, which lead to more errors. Learning on its own trips (the
// "DAgger" method) teaches the network to recover.
func study(table *rl.Agent, path string, hidden, rounds int, seed uint64, judge judging) (result, error) {
	start := time.Now()
	fmt.Printf("\nThe network (%d neurons per layer) learns from the table\n", hidden)
	fmt.Printf("%8s %10s %10s %8s %10s %10s\n", "round", "reward/day", "attempts", "clicks", "sent away", "moments")
	n := rl.NewNetSized(seed, hidden)
	rng := rand.New(rand.NewPCG(seed, 9))
	moments := sim.Moments(table, seed+3000, 100, 40000)
	n.Distill(table, moments, 20, rng)

	best := math.Inf(-1)
	for round := 0; round <= rounds; round++ {
		if round > 0 {
			moments = append(moments, sim.Moments(n, seed+3000+uint64(round), 50, 20000)...)
			n.Distill(table, moments, 4, rng)
		}
		e := sim.Evaluate(n, judge.progress, 100)
		fmt.Printf("%8d %10.2f %10.1f %8.1f %10.2f %9dk\n", round, e.Reward, e.Attention, e.Clicks, e.Dismissals, len(moments)/1000)
		if e.Reward > best {
			best = e.Reward
			if err := n.Save(path); err != nil {
				return result{}, err
			}
		}
	}
	return finish("network", path, seed, judge, start, os.Stdout)
}

// finish rescores the kept brain on days it never practiced on, since
// picking the best of several checkpoints flatters it, and saves the score.
func finish(kind, path string, seed uint64, judge judging, start time.Time, out io.Writer) (result, error) {
	var k rl.Learner
	var err error
	if kind == "network" {
		k, err = rl.LoadNet(path, seed)
	} else {
		k, err = rl.Load(path, seed)
	}
	if err != nil {
		return result{}, err
	}
	score := sim.Evaluate(k, judge.final, 100)
	k.SetSimScore(score.Reward)
	if err := k.Save(path); err != nil {
		return result{}, err
	}
	fmt.Fprintf(out, "the %s scores %.2f on fresh days; saved to %s (%s)\n", kind, score.Reward, path, time.Since(start).Round(time.Second))
	return result{kind: kind, score: score}, nil
}

// quietBuddy never says hi; the baseline every brain must beat.
type quietBuddy struct{}

func (quietBuddy) Act(rl.Observation) (rl.Action, bool)                     { return rl.Quiet, false }
func (quietBuddy) Best(rl.Observation) rl.Action                            { return rl.Quiet }
func (quietBuddy) Scores(rl.Observation) [rl.NumActions]float64             { return [rl.NumActions]float64{} }
func (quietBuddy) Learn(rl.Observation, rl.Action, float64, rl.Observation) {}
func (quietBuddy) Epsilon() float64                                         { return 0 }
func (quietBuddy) SimScore() float64                                        { return 0 }
func (quietBuddy) SetSimScore(float64)                                      {}
func (quietBuddy) Kind() string                                             { return "silent" }
func (quietBuddy) Save(string) error                                        { return nil }
