// Command buddysim pre-trains Buddy's two brains, the Q-table and the neural
// network, against a simulated user, compares them, and saves both. Buddy
// then uses whichever scored better.
package main

import (
	"flag"
	"fmt"
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
		brain     = flag.String("brain", "both", "which brain to train: table, network or both")
		tableDays = flag.Int("days", 2000, "simulated days to train the table for")
		netDays   = flag.Int("net-days", 400, "simulated days to train the network for")
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

	type job struct {
		learner rl.Learner
		path    string
		days    int
	}
	var jobs []job
	if *brain != "network" {
		jobs = append(jobs, job{rl.NewAgent(*seed), filepath.Join(*data, appdir.QTable), *tableDays})
	}
	if *brain != "table" {
		n := rl.NewNet(*seed)
		n.SetTraining(4, 1) // a simulated day has thousands of steps; train on every fourth
		jobs = append(jobs, job{n, filepath.Join(*data, appdir.Net), *netDays})
	}
	for _, j := range jobs {
		if _, err := os.Stat(j.path); err == nil && !*force {
			return fmt.Errorf("%s already exists; pass -force to replace it", j.path)
		}
	}

	scores := map[string]float64{}
	for _, j := range jobs {
		fmt.Printf("\nTraining the %s for %d simulated days\n", j.learner.Kind(), j.days)
		fmt.Printf("%8s %10s %10s %8s %10s %8s\n", "day", "reward/day", "actions", "clicks", "dismissals", "epsilon")
		report := func(day int) sim.Score {
			e := sim.Evaluate(j.learner, *seed+1000, 100)
			fmt.Printf("%8d %10.2f %10.1f %8.1f %10.2f %8.3f\n", day, e.Reward, e.Attention, e.Clicks, e.Dismissals, j.learner.Epsilon())
			return e
		}
		start := time.Now()
		user := sim.NewUser(*seed)
		report(0)
		step := max(1, j.days/10)
		var last sim.Score
		for day := 1; day <= j.days; day++ {
			sim.RunDay(user, j.learner, true)
			if day%step == 0 {
				last = report(day)
			}
		}
		j.learner.SetSimScore(last.Reward)
		scores[j.learner.Kind()] = last.Reward
		if err := j.learner.Save(j.path); err != nil {
			return err
		}
		fmt.Printf("saved to %s (%s)\n", j.path, time.Since(start).Round(time.Second))
	}

	if len(scores) == 2 {
		winner := "table"
		if scores["network"] > scores["table"] {
			winner = "network"
		}
		fmt.Printf("\nreward per simulated day: table %.2f, network %.2f; Buddy will use the %s\n",
			scores["table"], scores["network"], winner)
	}
	return nil
}
