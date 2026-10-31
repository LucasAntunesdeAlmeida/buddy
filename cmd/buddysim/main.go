// Command buddysim pre-trains Buddy against a simulated user and writes the
// Q-table that buddy loads on start.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
		days  = flag.Int("days", 2000, "simulated days to train for")
		out   = flag.String("out", filepath.Join(dir, appdir.QTable), "where to write the Q-table")
		seed  = flag.Uint64("seed", 1, "random seed")
		force = flag.Bool("force", false, "overwrite an existing Q-table, losing what Buddy learned from you")
	)
	flag.Parse()

	if _, err := os.Stat(*out); err == nil && !*force {
		return fmt.Errorf("%s already exists; pass -force to replace it", *out)
	}

	agent := rl.NewAgent(*seed)
	user := sim.NewUser(*seed)
	fmt.Printf("%8s %10s %10s %8s %10s %8s\n", "day", "reward/day", "actions", "clicks", "dismissals", "epsilon")
	report := func(day int) {
		// Evaluate the greedy policy on a separate user so training luck doesn't count.
		var reward, actions, clicks, dismissals float64
		u := sim.NewUser(*seed + 1000)
		const n = 100
		for range n {
			d := sim.RunDay(u, agent, false)
			reward += d.Reward
			actions += float64(d.Attention)
			clicks += float64(d.Clicks)
			dismissals += float64(d.Dismissals)
		}
		fmt.Printf("%8d %10.2f %10.1f %8.1f %10.2f %8.3f\n",
			day, reward/n, actions/n, clicks/n, dismissals/n, agent.Epsilon())
	}
	report(0)
	step := max(1, *days/10)
	for day := 1; day <= *days; day++ {
		sim.RunDay(user, agent, true)
		if day%step == 0 {
			report(day)
		}
	}

	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := agent.Save(*out); err != nil {
		return err
	}
	fmt.Printf("saved %d states to %s\n", len(agent.Q), *out)
	return nil
}
