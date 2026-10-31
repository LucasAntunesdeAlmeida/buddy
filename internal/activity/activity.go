// Package activity describes what the user is doing, in terms coarse enough
// to learn from and safe to store: an app category and a few timings, never
// window titles or keystrokes.
package activity

import (
	"path/filepath"
	"strings"
	"time"
)

// AwayAfter is how long without input before the user counts as away.
const AwayAfter = 5 * time.Minute

// Category is a coarse kind of foreground app.
type Category uint8

const (
	Other Category = iota
	Code
	Browser
	Game
	Media
	Chat
	NumCategories
)

var categoryNames = [NumCategories]string{"other", "code", "browser", "game", "media", "chat"}

func (c Category) String() string {
	if c >= NumCategories {
		return "unknown"
	}
	return categoryNames[c]
}

// Snapshot is the user's activity at one moment.
type Snapshot struct {
	Category Category
	// Idle is the time since the last keyboard or mouse input.
	Idle time.Duration
	// Activity is the fraction of the last minute with any input, from 0 to 1.
	Activity float64
	// InApp is how long the current foreground app has been in front.
	InApp time.Duration
	// Back is how long since the user came back from being away, or
	// negative if they haven't been away since Buddy started.
	Back time.Duration
	// Busy is set when Buddy must stay still and silent no matter what it
	// learned: during calls, presentations and full-screen apps.
	Busy bool
	// BusyWhy says what made the user busy, for the log.
	BusyWhy string
}

// Away reports whether the user has left the computer.
func (s Snapshot) Away() bool { return s.Idle >= AwayAfter }

var byProcess = map[string]Category{
	// The desktop and taskbar cover the whole screen but aren't games.
	"explorer.exe": Other, "searchhost.exe": Other, "startmenuexperiencehost.exe": Other,
	// Code and terminals.
	"code.exe": Code, "cursor.exe": Code, "devenv.exe": Code, "goland64.exe": Code,
	"idea64.exe": Code, "rider64.exe": Code, "pycharm64.exe": Code, "webstorm64.exe": Code,
	"sublime_text.exe": Code, "notepad++.exe": Code, "nvim.exe": Code, "zed.exe": Code,
	"windowsterminal.exe": Code, "wt.exe": Code, "powershell.exe": Code, "pwsh.exe": Code,
	"cmd.exe": Code, "mintty.exe": Code,
	// Browsers.
	"chrome.exe": Browser, "msedge.exe": Browser, "firefox.exe": Browser, "brave.exe": Browser,
	"opera.exe": Browser, "vivaldi.exe": Browser, "arc.exe": Browser,
	// Media players.
	"vlc.exe": Media, "spotify.exe": Media, "mpc-hc64.exe": Media, "mpv.exe": Media,
	"wmplayer.exe": Media, "microsoft.media.player.exe": Media, "potplayermini64.exe": Media,
	// Chat and calls.
	"discord.exe": Chat, "slack.exe": Chat, "teams.exe": Chat, "ms-teams.exe": Chat,
	"whatsapp.exe": Chat, "telegram.exe": Chat, "signal.exe": Chat, "zoom.exe": Chat,
}

// Browser tabs whose title marks them as watching rather than browsing.
var mediaTitleWords = []string{"youtube", "netflix", "twitch", "prime video", "disney+", "crunchyroll"}

// Classify maps a foreground window to a category. exe may be a full path.
// The title is only used here and is never stored.
func Classify(exe, title string, fullscreen bool) Category {
	name := strings.ToLower(filepath.Base(exe))
	cat, ok := byProcess[name]
	if cat == Browser {
		t := strings.ToLower(title)
		for _, w := range mediaTitleWords {
			if strings.Contains(t, w) {
				return Media
			}
		}
	}
	if ok {
		return cat
	}
	// Unknown apps that take the whole screen are almost always games.
	if fullscreen {
		return Game
	}
	return Other
}
