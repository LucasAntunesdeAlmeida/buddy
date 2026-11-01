// Package sense watches the desktop: which app is in front, how long since the
// last input, and how busy the user is. It asks Windows when the last input
// happened, never what was typed, so it needs no keyboard hooks.
package sense

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo         = user32.NewProc("GetLastInputInfo")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetTickCount             = kernel32.NewProc("GetTickCount")
	procMonitorFromWindow        = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	procSHQueryUserNotification  = windows.NewLazySystemDLL("shell32.dll").NewProc("SHQueryUserNotificationState")
)

const (
	poll        = 250 * time.Millisecond
	window      = time.Minute // activity is measured over this span
	historySize = int(window / poll)
	busyEvery   = 2 * time.Second // how often to check for calls and presentations

	monitorDefaultToNearest = 2
)

type lastInputInfo struct {
	size uint32
	time uint32
}

type rect struct{ left, top, right, bottom int32 }

type point struct{ x, y int32 }

// Watcher polls the desktop and keeps the latest Snapshot.
type Watcher struct {
	self    string // our own exe name; Buddy in front does not change the category
	changes chan struct{}

	mu   sync.Mutex
	snap activity.Snapshot

	// Only touched by Run.
	hwnd      uintptr
	hwndExe   string // process of hwnd
	exe       string // process of the last app that wasn't Buddy
	cat       activity.Category
	since     time.Time
	lastInput uint32
	history   [historySize]bool
	next      int
	away      bool
	backAt    time.Time // when the user last came back from being away
	busyAt    time.Time // when busy was last checked
	busyWhy   string
}

// NewWatcher returns a watcher. Call Run to start it.
func NewWatcher() *Watcher {
	self, _ := os.Executable()
	return &Watcher{
		self:    strings.ToLower(filepath.Base(self)),
		changes: make(chan struct{}, 1),
		since:   time.Now(),
	}
}

// Snapshot returns the latest view of the user's activity.
func (w *Watcher) Snapshot() activity.Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.snap
}

// Changes signals notable moments: switching apps, leaving, coming back.
func (w *Watcher) Changes() <-chan struct{} { return w.changes }

// Run polls until ctx is done.
func (w *Watcher) Run(ctx context.Context) {
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		w.poll()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *Watcher) poll() {
	now := time.Now()
	notable := false

	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd != w.hwnd {
		w.hwnd = hwnd
		w.hwndExe = processName(hwnd)
	}
	// While Buddy itself is in front, keep describing the app the user came from.
	if w.hwndExe != w.self {
		if w.hwndExe != w.exe {
			w.exe = w.hwndExe
			w.since = now
			notable = true
		}
		title := func() string { return windowTitle(hwnd) }
		w.cat = activity.Classify(w.exe, title, fullscreen(hwnd))
	}

	info := lastInputInfo{size: uint32(unsafe.Sizeof(lastInputInfo{}))}
	procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	tick, _, _ := procGetTickCount.Call()
	idle := time.Duration(uint32(tick)-info.time) * time.Millisecond // wraps safely

	w.history[w.next] = info.time != w.lastInput
	w.next = (w.next + 1) % historySize
	w.lastInput = info.time
	busy := 0
	for _, b := range w.history {
		if b {
			busy++
		}
	}

	away := idle >= activity.AwayAfter
	if away != w.away {
		w.away = away
		notable = true
		if !away {
			w.backAt = now
		}
	}
	if now.Sub(w.busyAt) >= busyEvery {
		w.busyAt = now
		if why := busyReason(); why != w.busyWhy {
			w.busyWhy = why
			notable = true
		}
	}
	back := time.Duration(-1)
	if !w.backAt.IsZero() {
		back = now.Sub(w.backAt)
	}

	w.mu.Lock()
	w.snap = activity.Snapshot{
		Category: w.cat,
		Idle:     idle,
		Activity: float64(busy) / float64(historySize),
		InApp:    now.Sub(w.since),
		Back:     back,
		Busy:     w.busyWhy != "",
		BusyWhy:  w.busyWhy,
	}
	w.mu.Unlock()

	if notable {
		select {
		case w.changes <- struct{}{}:
		default:
		}
	}
}

func windowTitle(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var buf [256]uint16
	n, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:n])
}

func processName(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return strings.ToLower(filepath.Base(windows.UTF16ToString(buf[:n])))
}

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

// fullscreen reports whether the window covers the whole monitor it's on.
func fullscreen(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return false
	}
	m, ok := monitorOf(hwnd)
	if !ok {
		return false
	}
	return r.left <= m.monitor.left && r.top <= m.monitor.top && r.right >= m.monitor.right && r.bottom >= m.monitor.bottom
}

// monitorOf returns the monitor a window is on.
func monitorOf(hwnd uintptr) (monitorInfo, bool) {
	mon, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		return mi, false
	}
	return mi, true
}

// Values of QUERY_USER_NOTIFICATION_STATE that mean "don't disturb".
const (
	qunsBusy             = 2 // a full-screen app
	qunsD3DFullScreen    = 3 // a full-screen game
	qunsPresentationMode = 4
	qunsApp              = 7 // a full-screen store app
)

// busyReason says why Buddy must keep still, or "" if it needn't. It never
// names the app, so the reason is safe to log.
func busyReason() string {
	if capabilityInUse("microphone") {
		return "microphone in use"
	}
	if capabilityInUse("webcam") {
		return "camera in use"
	}
	var state int32
	if hr, _, _ := procSHQueryUserNotification.Call(uintptr(unsafe.Pointer(&state))); hr == 0 {
		switch state {
		case qunsPresentationMode:
			return "presenting"
		case qunsBusy, qunsD3DFullScreen, qunsApp:
			return "full screen"
		}
	}
	return ""
}

// capabilityInUse reports whether any app is using the microphone or camera
// right now. Windows records each app's last use under the privacy consent
// store; a start time without a stop time means it's still in use. Calls are
// what this catches.
func capabilityInUse(name string) bool {
	base := `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\` + name
	var check func(path string) bool
	check = func(path string) bool {
		k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			return false
		}
		defer k.Close()
		apps, _ := k.ReadSubKeyNames(-1)
		for _, app := range apps {
			if app == "NonPackaged" { // desktop apps live one level down
				if check(path + `\NonPackaged`) {
					return true
				}
				continue
			}
			ak, err := registry.OpenKey(registry.CURRENT_USER, path+`\`+app, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			start, _, errStart := ak.GetIntegerValue("LastUsedTimeStart")
			stop, _, errStop := ak.GetIntegerValue("LastUsedTimeStop")
			ak.Close()
			if errStart == nil && errStop == nil && start > 0 && stop == 0 {
				return true
			}
		}
		return false
	}
	return check(base)
}

// CursorPos returns the mouse position on screen in physical pixels.
func CursorPos() (x, y int, ok bool) {
	var p point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); r == 0 {
		return 0, 0, false
	}
	return int(p.x), int(p.y), true
}
