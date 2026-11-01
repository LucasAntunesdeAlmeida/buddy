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

	"github.com/LucasAntunesdeAlmeida/buddy/internal/appdir"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/autostart"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/brain"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/report"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/rl"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/sense"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/settings"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/store"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/ui"
	"github.com/LucasAntunesdeAlmeida/buddy/internal/voice"
)

// logLimit keeps the log file small: it starts over past this size.
const logLimit = 1 << 20

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "buddy:", err)
		notify(err.Error())
		os.Exit(1)
	}
}

func run() (err error) {
	def, err := appdir.Default()
	if err != nil {
		return err
	}
	var (
		dir     = flag.String("data", def, "folder for what Buddy learns")
		speak   = flag.Bool("voice", false, "also read speech bubbles aloud with the Windows voice")
		choice  = flag.String("brain", "network", "which brain decides: network (with the table as its diary) or table")
		verbose = flag.Bool("v", false, "log every decision")
		show    = flag.Bool("report", false, "print what Buddy has learned and exit")
		htmlOut = flag.String("report-html", "", "write the visual \"what has Buddy learned?\" page to this file and exit")
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
	defer func() {
		// Log the error here, while the log file is still open.
		if err != nil {
			slog.Error("buddy stopped", "err", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	shipped, release, err := claim(*show || *htmlOut != "")
	if err != nil {
		return err
	}
	defer release()

	learner, savePath, err := loadBrain(*dir, shipped, *choice)
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
	if *htmlOut != "" {
		f, err := os.Create(*htmlOut)
		if err != nil {
			return err
		}
		defer f.Close()
		return report.WriteHTML(ctx, f, locked, log, time.Now())
	}

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
		Autostart: autostart.New(startArgs(flag.CommandLine)...),
		Report: func() (string, error) {
			path := filepath.Join(*dir, "what-buddy-learned.html")
			f, err := os.Create(path)
			if err != nil {
				return "", err
			}
			defer f.Close()
			return path, report.WriteHTML(ctx, f, locked, log, time.Now())
		},
		OnEnd: finish,
	})
	finish()
	slog.Info("Buddy is asleep for good, see you next time")
	return err
}

// startArgs returns the flags that were set, for starting Buddy the same way
// at sign-in. Windows won't start him from this folder, so -data has to be
// absolute.
func startArgs(fs *flag.FlagSet) []string {
	var args []string
	fs.Visit(func(f *flag.Flag) {
		v := f.Value.String()
		if f.Name == "data" {
			if abs, err := filepath.Abs(v); err == nil {
				v = abs
			}
		}
		args = append(args, "-"+f.Name+"="+v)
	})
	return args
}

// errNoNetwork means there's no network, and none came with this Buddy.
var errNoNetwork = errors.New("Buddy has no trained network yet. Run buddysim first; it takes a few minutes")

// claim makes this the only Buddy, and returns the folder of brains it may
// install. Installing changes the data folder, so it has to happen after the
// claim. A report can still be made beside a running Buddy, but only from
// what's already there, so it gets no folder to install from.
func claim(report bool) (shipped string, release func(), err error) {
	release, err = claimInstance()
	switch {
	case err == nil:
		return shippedBrains(), release, nil
	case report && errors.Is(err, errAlreadyRunning):
		return "", func() {}, nil
	}
	return "", nil, err
}

// shippedBrains returns the folder of brains next to this exe. A go run
// build has none, and looking there finds nothing.
func shippedBrains() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), appdir.ShippedBrains)
}

// loadBrain loads the brain to use and returns where to save it. The network
// decides, with the table as its diary, and on first run it comes from the
// shipped folder. Without one Buddy refuses to start rather than quietly
// running on a table that has never practiced. The table only decides on its
// own when asked to, with -brain table.
func loadBrain(dir, shipped, choice string) (rl.Learner, string, error) {
	if choice != "network" && choice != "table" {
		return nil, "", fmt.Errorf("-brain must be network or table, not %q", choice)
	}
	if choice == "network" {
		if err := appdir.InstallBrains(shipped, dir); err != nil {
			return nil, "", err
		}
	}
	seed := uint64(time.Now().UnixNano())
	tablePath, netPath := filepath.Join(dir, appdir.QTable), filepath.Join(dir, appdir.Net)
	table, err := rl.Load(tablePath, seed)
	if err != nil {
		return nil, "", err
	}
	if choice == "table" {
		return table, tablePath, nil
	}
	net, err := rl.LoadNet(netPath, seed)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", errNoNetwork
	}
	if err != nil {
		return nil, "", err
	}
	return rl.NewHybrid(net, table, tablePath, seed), netPath, nil
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
