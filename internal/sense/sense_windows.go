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

	"buddy/internal/activity"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetLastInputInfo         = user32.NewProc("GetLastInputInfo")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetTickCount             = kernel32.NewProc("GetTickCount")
)

const (
	poll        = 250 * time.Millisecond
	window      = time.Minute // activity is measured over this span
	smCXScreen  = 0
	smCYScreen  = 1
	historySize = int(window / poll)
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
		w.cat = activity.Classify(w.exe, windowTitle(hwnd), fullscreen(hwnd))
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
	}

	w.mu.Lock()
	w.snap = activity.Snapshot{
		Category: w.cat,
		Idle:     idle,
		Activity: float64(busy) / float64(historySize),
		InApp:    now.Sub(w.since),
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

// fullscreen reports whether the window covers the whole primary screen.
func fullscreen(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var r rect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ok == 0 {
		return false
	}
	w, _, _ := procGetSystemMetrics.Call(smCXScreen)
	h, _, _ := procGetSystemMetrics.Call(smCYScreen)
	return r.left <= 0 && r.top <= 0 && r.right >= int32(w) && r.bottom >= int32(h)
}

// CursorPos returns the mouse position on screen in physical pixels.
func CursorPos() (x, y int, ok bool) {
	var p point
	if r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&p))); r == 0 {
		return 0, 0, false
	}
	return int(p.x), int(p.y), true
}
