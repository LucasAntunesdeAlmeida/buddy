// Command buddy runs Buddy, a desktop companion that learns with reinforcement
// learning when and how to get your attention. Everything stays on this
// computer: it makes no network connections.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"time"

	"buddy/internal/appdir"
	"buddy/internal/autostart"
	"buddy/internal/brain"
	"buddy/internal/report"
	"buddy/internal/rl"
	"buddy/internal/sense"
	"buddy/internal/settings"
	"buddy/internal/store"
	"buddy/internal/ui"
	"buddy/internal/voice"
)

// logLimit keeps the log file small: it starts over past this size.
const logLimit = 1 << 20

func main() {
	if err := run(); err != nil {
		slog.Error("buddy stopped", "err", err)
		fmt.Fprintln(os.Stderr, "buddy:", err)
		notify(err.Error())
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
		speak   = flag.Bool("voice", false, "also read speech bubbles aloud with the Windows voice")
		choice  = flag.String("brain", "auto", "which brain to use: table, network, or auto for the one that scored better in buddysim")
		verbose = flag.Bool("v", false, "log every decision")
		show    = flag.Bool("report", false, "print what Buddy has learned and exit")
	)
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("create data folder: %w", err)
	}
	closeLog, err := setupLog(filepath.Join(*dir, appdir.LogFile), *verbose)
	if err != nil {
		return err
	}
	defer closeLog()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	learner, savePath, err := loadBrain(*dir, *choice)
	if err != nil {
		return err
	}
	locked := rl.NewLocked(learner)
	log, err := store.Open(ctx, filepath.Join(*dir, appdir.Log))
	if err != nil {
		return err
	}
	defer log.Close()

	if *show {
		return report.Write(ctx, os.Stdout, locked, log, time.Now())
	}

	release, err := claimInstance()
	if err != nil {
		return err
	}
	defer release()

	var speaker brain.Speaker
	if *speak {
		s, err := voice.NewSpeaker()
		if err != nil {
			return err
		}
		defer s.Close()
		speaker = s
	}

	prefs := settings.Load(filepath.Join(*dir, appdir.Settings))
	phrases := voice.NewPicker(filepath.Join(*dir, appdir.Said), rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 11)))
	watcher := sense.NewWatcher()
	commands := make(chan brain.Command, 4)
	reactions := make(chan rl.Reaction, 8)
	b := &brain.Brain{
		Learner: locked,
		Sensor:  watcher,
		Log:     log,
		Save: func() error {
			return errors.Join(locked.Save(savePath), phrases.Save())
		},
		Commands:  commands,
		Reactions: reactions,
		Phrase:    phrases.Pick,
		Speaker:   speaker,
		Settings:  prefs,
	}

	var wg sync.WaitGroup
	wg.Go(func() { watcher.Run(ctx) })
	wg.Go(func() { b.Run(ctx) })
	finish := sync.OnceFunc(func() {
		stop()
		wg.Wait() // the brain saves what it learned before returning
	})

	slog.Info("Buddy is awake", "brain", learner.Kind(), "data", *dir)
	// The window runs on the main goroutine. It returns when Buddy is quit
	// from its menu or ctx is canceled (Ctrl+C).
	err = ui.Run(ctx, commands, reactions, ui.Options{
		Settings:  prefs,
		Autostart: autostart.New(),
		Report: func() (string, error) {
			path := filepath.Join(*dir, "what-buddy-learned.txt")
			f, err := os.Create(path)
			if err != nil {
				return "", err
			}
			defer f.Close()
			return path, report.Write(ctx, f, locked, log, time.Now())
		},
		OnEnd: finish,
	})
	finish()
	slog.Info("Buddy is asleep for good, see you next time")
	return err
}

// loadBrain loads the brain to use and returns where to save it.
func loadBrain(dir, choice string) (rl.Learner, string, error) {
	seed := uint64(time.Now().UnixNano())
	tablePath, netPath := filepath.Join(dir, appdir.QTable), filepath.Join(dir, appdir.Net)
	table, err := rl.Load(tablePath, seed)
	if err != nil {
		return nil, "", err
	}
	net, err := rl.LoadNet(netPath, seed)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	switch choice {
	case "table":
		return table, tablePath, nil
	case "network":
		if net == nil {
			net = rl.NewNet(seed)
		}
		return net, netPath, nil
	case "auto":
		if net != nil && net.SimScore() > table.SimScore() {
			return net, netPath, nil
		}
		if table.Steps == 0 {
			slog.Info("Buddy knows nothing yet; run buddysim first for a head start")
		}
		return table, tablePath, nil
	}
	return nil, "", fmt.Errorf("-brain must be table, network or auto, not %q", choice)
}

// setupLog sends logs to a file in the data folder, and to the console when
// there is one. The file starts over once it grows past logLimit.
func setupLog(path string, verbose bool) (func(), error) {
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > logLimit {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	// The file comes first: without a console, writing to stderr fails, and
	// MultiWriter stops at the first failure.
	w := io.MultiWriter(f, os.Stderr)
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})))
	return func() { f.Close() }, nil
}
