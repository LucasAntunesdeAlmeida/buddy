// Command buddy runs Buddy, a desktop companion that learns with reinforcement
// learning when and how to get your attention. Everything stays on this
// computer: it makes no network connections.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"buddy/internal/appdir"
	"buddy/internal/brain"
	"buddy/internal/rl"
	"buddy/internal/sense"
	"buddy/internal/store"
	"buddy/internal/ui"
	"buddy/internal/voice"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "buddy:", err)
		os.Exit(1)
	}
}

func run() error {
	def, err := appdir.Default()
	if err != nil {
		return err
	}
	var (
		dir     = flag.String("data", def, "folder for what Buddy learns")
		speak   = flag.Bool("voice", false, "read speech bubbles aloud with the Windows voice")
		verbose = flag.Bool("v", false, "log every decision")
	)
	flag.Parse()
	if *verbose {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("create data folder: %w", err)
	}
	qpath := filepath.Join(*dir, appdir.QTable)
	agent, err := rl.Load(qpath, uint64(time.Now().UnixNano()))
	if err != nil {
		return err
	}
	if agent.Steps == 0 {
		slog.Info("Buddy knows nothing yet; run buddysim first for a head start")
	}
	log, err := store.Open(ctx, filepath.Join(*dir, appdir.Log))
	if err != nil {
		return err
	}
	defer log.Close()

	var speaker brain.Speaker
	if *speak {
		s, err := voice.NewSpeaker()
		if err != nil {
			return err
		}
		defer s.Close()
		speaker = s
	}

	watcher := sense.NewWatcher()
	commands := make(chan brain.Command, 4)
	reactions := make(chan rl.Reaction, 8)
	b := &brain.Brain{
		Agent:     agent,
		Sensor:    watcher,
		Log:       log,
		Save:      func() error { return agent.Save(qpath) },
		Commands:  commands,
		Reactions: reactions,
		Speaker:   speaker,
		Phrase:    voice.Pick,
	}

	var wg sync.WaitGroup
	wg.Go(func() { watcher.Run(ctx) })
	wg.Go(func() { b.Run(ctx) })

	slog.Info("Buddy is awake", "data", *dir, "states known", len(agent.Q))
	// The window runs on the main goroutine. It returns when Buddy is quit
	// from its right-click menu or ctx is canceled (Ctrl+C).
	err = ui.Run(ctx, commands, reactions)
	stop()
	wg.Wait() // the brain saves what it learned before returning
	return err
}
