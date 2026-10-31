# Buddy

A small desktop companion for Windows that notices what you're doing and
learns, with reinforcement learning, when and how to get your attention.

Buddy is 100% local and light: it makes no network connections, stores
everything in one folder on your computer, and while resting it uses
about 15 MB of RAM and next to no CPU.

## Run it

Requires Go 1.26 or newer on Windows. No C compiler is needed.

```sh
# Give Buddy a head start by practicing against a simulated user (a few seconds).
go run ./cmd/buddysim

# Start Buddy.
go run ./cmd/buddy
```

To build an `.exe` that runs without a console window:

```sh
go build -ldflags "-s -w -H=windowsgui" -o buddy.exe ./cmd/buddy
```

Flags for `buddy`:

| Flag | Default | Meaning |
|---|---|---|
| `-voice` | off | Also read speech bubbles aloud with the built-in Windows voice |
| `-data` | `%AppData%\buddy` | Folder for what Buddy learns |
| `-v` | off | Log every decision to the console |

## Using Buddy

| You | Buddy |
|---|---|
| Click Buddy | Counts as a good reaction, and Buddy looks happy |
| Drag Buddy | Moves it; counts as "get out of the way" |
| Right-click → Go away for 30 minutes | Buddy sleeps; counts as a strong "no" |
| Right-click → Quit Buddy | Saves what it learned and exits |
| Leave the computer for 5 minutes | Buddy falls asleep until you're back |

Clicks on the transparent parts of the window go through to the app below,
and clicking Buddy never takes focus away from what you're typing in.

## How it learns

Buddy is a tabular Q-learning agent.

- **State**: the kind of app in front (code, browser, game, media, chat,
  other), how long since your last input, how busy your input is, how long
  you've been in this app, the time of day, how annoyed Buddy thinks you are,
  and how long since it last tried to get your attention. Each is reduced to
  a few buckets, about 7,800 states in all.
- **Actions**: stay quiet, fidget, wave, bounce, speak, chirp, walk to the cursor.
- **Reward**: a click is +1, a drag −0.5, "go away" −2. Every attention action
  costs a little, and interrupting while you're typing fast costs more, so
  Buddy learns restraint as well as timing.
- **Why it's reinforcement learning and not just "what works now"**: Buddy's
  estimate of your annoyance is part of the state, so pestering you now leads
  to worse states later. With a discount factor of 0.9, Buddy learns that
  spamming does not pay off.
- **Exploration** is epsilon-greedy and decays to 2%. While you're focused,
  Buddy only experiments with quiet actions.
- **Learning rate** is 1/visits per state and action, with a floor of 0.05,
  so Buddy settles quickly but keeps adapting when your habits change.

Buddy decides every 30 seconds, and right away when you switch apps, leave or
come back. A reaction within 10 seconds of an action counts toward it.

`buddysim` trains the same agent against a toy model of a person (in
`internal/sim`), so Buddy starts with sensible manners. Its numbers are
invented common sense, not measurements. Your real reactions take over from
there.

## Privacy

- Buddy asks Windows *when* you last pressed a key or moved the mouse, never
  *what* you typed. It installs no keyboard hooks and takes no screenshots.
- Window titles are read only to tell a video site from other browsing, and
  are never stored.
- Everything lives in the data folder:
  - `qtable.json`: what Buddy has learned (readable JSON).
  - `buddy.db`: a SQLite log of Buddy's actions and your reactions, with only
    the bucketed situation (for example `code/active/input-high/app-1h+/evening/calm/last-15m+`).

Delete the folder to make Buddy forget everything.

## Layout

```
cmd/buddy          the app: wiring only
cmd/buddysim       pre-training against the simulated user
internal/activity  app categories and activity snapshots
internal/sense     Windows activity watcher
internal/rl        state encoding, rewards, Q-learning agent
internal/brain     the decide → act → observe → learn loop
internal/sim       the simulated user
internal/store     SQLite experience log
internal/ui        the transparent window and CPU-drawn Buddy
internal/voice     phrases and optional Windows speech
```

## Development

```sh
go vet ./...
go test ./...
BUDDY_SNAPSHOTS=/some/folder go test ./internal/ui   # save every pose as a PNG
```

The race detector needs cgo and a C compiler, so `go test -race` only works
where one is installed.
