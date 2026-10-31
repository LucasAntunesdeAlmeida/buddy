package ui

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"buddy/internal/brain"
	"buddy/internal/rl"
	"buddy/internal/sense"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	winmm    = windows.NewLazySystemDLL("winmm.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procUpdateLayeredWindow           = user32.NewProc("UpdateLayeredWindow")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSetCapture                    = user32.NewProc("SetCapture")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenu                = user32.NewProc("TrackPopupMenu")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procCreateCompatibleDC            = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection              = gdi32.NewProc("CreateDIBSection")
	procSelectObject                  = gdi32.NewProc("SelectObject")
	procDeleteObject                  = gdi32.NewProc("DeleteObject")
	procDeleteDC                      = gdi32.NewProc("DeleteDC")
	procPlaySoundW                    = winmm.NewProc("PlaySoundW")
	procGetModuleHandleW              = kernel32.NewProc("GetModuleHandleW")
)

const (
	wsPopup           = 0x80000000
	wsExTopmost       = 0x00000008
	wsExToolWindow    = 0x00000080
	wsExLayered       = 0x00080000
	wsExNoActivate    = 0x08000000
	swShowNoActive    = 4
	swpNoSize         = 0x0001
	swpNoActivate     = 0x0010
	ulwAlpha          = 0x00000002
	acSrcAlpha        = 0x01
	spiGetWorkArea    = 0x0030
	idcHand           = 32649
	mfString          = 0x0000
	mfSeparator       = 0x0800
	tpmRightButton    = 0x0002
	tpmNoNotify       = 0x0080
	tpmReturnCmd      = 0x0100
	sndAsync          = 0x0001
	sndNoDefault      = 0x0002
	sndMemory         = 0x0004
	wmNull            = 0x0000
	wmDestroy         = 0x0002
	wmTimer           = 0x0113
	wmMouseMove       = 0x0200
	wmLButtonDown     = 0x0201
	wmLButtonUp       = 0x0202
	wmRButtonUp       = 0x0205
	wmCaptureChange   = 0x0215
	wmDPIChanged      = 0x02E0
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016

	// Frame intervals in milliseconds. Buddy only redraws fast while something moves.
	frameMoving = 33  // about 30 frames a second
	frameIdle   = 125 // breathing and blinking
	frameAsleep = 200 // slow enough to rest, fast enough for drifting Zs
	dragSlop    = 4
	walkSpeed   = 9.0 // pixels per frame at 100% scaling

	menuAway = 1
	menuQuit = 2
)

// dpiAwarePerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4.
var dpiAwarePerMonitorV2 = ^uintptr(3)

// hwndTopmost is HWND_TOPMOST, (HWND)-1.
var hwndTopmost = ^uintptr(0)

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type bitmapInfo struct {
	size          uint32
	width, height int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPerMeter     int32
	yPerMeter     int32
	clrUsed       uint32
	clrImportant  uint32
	colors        [1]uint32
}

type blendFunction struct{ op, flags, constantAlpha, alphaFormat byte }

// window is Buddy's on-screen window. All its fields belong to the thread
// running the message loop.
type window struct {
	ctx       context.Context
	commands  <-chan brain.Command
	reactions chan<- rl.Reaction
	scene     *Scene
	opts      Options

	hwnd     uintptr
	trayIcon uintptr
	memDC    uintptr
	bitmap   uintptr
	bits     []byte
	scale    float64
	x, y     int // window position, physical pixels

	interval   uintptr // current frame interval
	pressing   bool
	dragged    bool
	pressAt    point // cursor when the button went down
	startAt    point // window position when the button went down
	lastMove   point // cursor at the last drag speed sample
	lastMoveAt time.Time
	pathLen    float64 // how far the cursor travelled during this drag
	lastPath   point
	grabbedAt  time.Time
	walking    bool
	walkX      float64
	walkY      float64
	targetX    float64
	targetY    float64
}

var (
	// current is the one window; the window procedure finds it here.
	current *window
	wndProc = windows.NewCallback(func(hwnd, m, wp, lp uintptr) uintptr {
		return current.handle(hwnd, uint32(m), wp, lp)
	})
	chirp = chirpWAV()
)

// Run shows Buddy and runs its window until Buddy is quit or ctx is done.
// It must be called from the main goroutine.
func Run(ctx context.Context, commands <-chan brain.Command, reactions chan<- rl.Reaction, opts Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Draw sharply on scaled displays instead of letting Windows stretch a blurry bitmap.
	procSetProcessDpiAwarenessContext.Call(dpiAwarePerMonitorV2)

	scene, err := NewScene(1)
	if err != nil {
		return err
	}
	w := &window{ctx: ctx, commands: commands, reactions: reactions, scene: scene, opts: opts}
	current = w

	instance, _, _ := procGetModuleHandleW.Call(0)
	className, _ := windows.UTF16PtrFromString("BuddyWindow")
	title, _ := windows.UTF16PtrFromString("Buddy")
	cursor, _, _ := procLoadCursorW.Call(0, idcHand)
	wc := wndClassEx{wndProc: wndProc, instance: instance, cursor: cursor, className: className}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("register window class: %w", err)
	}
	// Layered: per-pixel transparency, and clicks on transparent pixels go
	// to whatever is below. Tool window: no taskbar button. No-activate:
	// clicking Buddy doesn't steal focus from what you're doing.
	w.hwnd, _, err = procCreateWindowExW.Call(
		wsExLayered|wsExTopmost|wsExToolWindow|wsExNoActivate,
		uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		wsPopup, 0, 0, Width, Height, 0, 0, instance, 0)
	if w.hwnd == 0 {
		return fmt.Errorf("create window: %w", err)
	}

	w.memDC, _, _ = procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(w.memDC)
	defer func() {
		if w.bitmap != 0 {
			procDeleteObject.Call(w.bitmap)
		}
	}()
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	if err := w.resize(float64(max(dpi, 96)) / 96); err != nil {
		procDestroyWindow.Call(w.hwnd)
		return err
	}

	// Start at the bottom right of the main monitor, just above the taskbar.
	work := workAreaAt(point{})
	sw, sh := w.scene.Size()
	w.move(int(work.right)-sw-int(20*w.scale), int(work.bottom)-sh)

	w.present()
	procShowWindow.Call(w.hwnd, swShowNoActive)
	w.addTray()
	if opts.paused(time.Now()) {
		w.scene.Sleep(time.Now())
	}
	w.pace(time.Now())

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 is WM_QUIT, -1 an error
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (w *window) handle(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmTimer:
		w.tick()
		return 0
	case wmLButtonDown:
		procSetCapture.Call(hwnd)
		w.pressing, w.dragged = true, false
		w.pace(time.Now())
		w.pressAt = cursor()
		w.startAt = point{int32(w.x), int32(w.y)}
		return 0
	case wmMouseMove:
		if w.pressing {
			now, c := time.Now(), cursor()
			dx, dy := int(c.x-w.pressAt.x), int(c.y-w.pressAt.y)
			if !w.dragged && abs(dx)+abs(dy) > int(dragSlop*w.scale) {
				w.dragged = true
				w.walking = false // a drag cancels walking toward the cursor
				w.scene.Grab(now)
				w.lastMove, w.lastMoveAt = c, now
				w.lastPath, w.pathLen, w.grabbedAt = w.pressAt, 0, now
			}
			if w.dragged {
				w.move(int(w.startAt.x)+dx, int(w.startAt.y)+dy)
				w.pathLen += math.Hypot(float64(c.x-w.lastPath.x), float64(c.y-w.lastPath.y))
				w.lastPath = c
				// Tell the scene how fast Buddy is carried, so it swings.
				if dt := now.Sub(w.lastMoveAt).Seconds(); dt > 0.005 {
					w.scene.Drag(float64(c.x-w.lastMove.x) / w.scale / dt)
					w.lastMove, w.lastMoveAt = c, now
				}
			}
		}
		return 0
	case wmLButtonUp:
		if !w.pressing {
			return 0
		}
		w.pressing = false
		procReleaseCapture.Call()
		if w.dragged {
			w.scene.Release(time.Now())
			w.send(w.dragKind(cursor()))
		} else {
			w.send(rl.Clicked)
			w.scene.Poke(time.Now())
			w.present()
			w.pace(time.Now())
		}
		return 0
	case wmCaptureChange:
		if w.pressing && w.dragged {
			w.scene.Release(time.Now())
		}
		w.pressing = false
		return 0
	case wmRButtonUp:
		w.menu(true)
		return 0
	case wmTray:
		if uint32(lp) == wmRButtonUp || uint32(lp) == wmLButtonUp {
			w.menu(false)
		}
		return 0
	case wmQueryEndSession:
		return 1 // yes, Windows may sign out or shut down
	case wmEndSession:
		if wp != 0 && w.opts.OnEnd != nil {
			w.opts.OnEnd() // Windows ends the app once this returns, so save now
		}
		return 0
	case wmDPIChanged:
		// Redraw at the new monitor's scale; Buddy keeps its position.
		if err := w.resize(float64(wp&0xffff) / 96); err == nil {
			w.present()
		}
		return 0
	case wmDestroy:
		procKillTimer.Call(hwnd, 1)
		w.removeTray()
		procPostQuitMessage.Call(0)
		return 0
	}
	if m == taskbarCreated && m != 0 {
		w.addTray() // Explorer restarted and lost the icon
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), wp, lp)
	return r
}

func (w *window) tick() {
	select {
	case <-w.ctx.Done():
		procDestroyWindow.Call(w.hwnd)
		return
	default:
	}
	now := time.Now()
	for drained := false; !drained; {
		select {
		case c := <-w.commands:
			e := w.scene.Apply(c, now)
			if e.Sound && w.opts.soundOn() {
				procPlaySoundW.Call(uintptr(unsafe.Pointer(&chirp[0])), 0, sndMemory|sndAsync|sndNoDefault)
			}
			if e.Walk {
				w.startWalk()
			}
		default:
			drained = true
		}
	}
	w.walk(now)

	w.present()
	w.pace(now)
}

// pace sets how often the window wakes up, so a resting Buddy costs almost nothing.
func (w *window) pace(now time.Time) {
	interval := uintptr(frameIdle)
	switch {
	case w.pressing || w.walking || w.scene.Animating(now):
		interval = frameMoving
	case w.scene.Asleep():
		interval = frameAsleep
	}
	if interval != w.interval {
		w.interval = interval
		procSetTimer.Call(w.hwnd, 1, interval, 0)
	}
}

func (w *window) send(r rl.Reaction) {
	select {
	case w.reactions <- r:
	default: // the brain is busy; one lost click is not worth freezing the window
	}
}

func (w *window) startWalk() {
	c := cursor()
	work := workAreaAt(c) // the monitor the cursor is on
	sw, sh := w.scene.Size()
	// Stop a little to the left of the cursor, Buddy's feet at its height.
	tx := float64(c.x) - (bodyX+bodyRX+50)*w.scale
	ty := float64(c.y) - (ground-bodyH/2)*w.scale
	w.targetX = math.Max(float64(work.left), math.Min(tx, float64(int(work.right)-sw)))
	w.targetY = math.Max(float64(work.top), math.Min(ty, float64(int(work.bottom)-sh)))
	w.walkX, w.walkY = float64(w.x), float64(w.y)
	w.walking = true
	w.scene.Face(w.targetX - w.walkX)
}

func (w *window) walk(now time.Time) {
	if !w.walking {
		return
	}
	dx, dy := w.targetX-w.walkX, w.targetY-w.walkY
	dist := math.Hypot(dx, dy)
	step := walkSpeed * w.scale
	if dist < step || !w.scene.Walking(now) {
		w.walking = false
		return
	}
	w.walkX += dx / dist * step
	w.walkY += dy / dist * step
	w.move(int(w.walkX), int(w.walkY))
}

func (w *window) move(x, y int) {
	w.x, w.y = x, y
	procSetWindowPos.Call(w.hwnd, hwndTopmost, uintptr(x), uintptr(y), 0, 0, swpNoSize|swpNoActivate)
}

// resize makes a bitmap for a new display scale.
func (w *window) resize(scale float64) error {
	if err := w.scene.SetScale(scale); err != nil {
		return err
	}
	w.scale = scale
	sw, sh := w.scene.Size()
	bi := bitmapInfo{width: int32(sw), height: -int32(sh), planes: 1, bitCount: 32} // negative height: top-down rows
	bi.size = uint32(unsafe.Offsetof(bi.colors))
	var bits unsafe.Pointer
	bmp, _, err := procCreateDIBSection.Call(w.memDC, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 {
		return fmt.Errorf("create bitmap: %w", err)
	}
	procSelectObject.Call(w.memDC, bmp)
	if w.bitmap != 0 {
		procDeleteObject.Call(w.bitmap)
	}
	w.bitmap = bmp
	w.bits = unsafe.Slice((*byte)(bits), sw*sh*4)
	return nil
}

// present draws the scene and hands the pixels to Windows.
func (w *window) present() {
	img := w.scene.Render(time.Now())
	// Both are premultiplied alpha; Windows wants BGRA order.
	for i := 0; i+3 < len(img.Pix) && i+3 < len(w.bits); i += 4 {
		w.bits[i], w.bits[i+1], w.bits[i+2], w.bits[i+3] = img.Pix[i+2], img.Pix[i+1], img.Pix[i], img.Pix[i+3]
	}
	sw, sh := w.scene.Size()
	size := point{int32(sw), int32(sh)}
	var src point
	blend := blendFunction{constantAlpha: 255, alphaFormat: acSrcAlpha}
	screen, _, _ := procGetDC.Call(0)
	procUpdateLayeredWindow.Call(w.hwnd, screen, 0, uintptr(unsafe.Pointer(&size)),
		w.memDC, uintptr(unsafe.Pointer(&src)), 0, uintptr(unsafe.Pointer(&blend)), ulwAlpha)
	procReleaseDC.Call(0, screen)
}

func cursor() point {
	x, y, _ := sense.CursorPos()
	return point{int32(x), int32(y)}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// dragKind tells moving Buddy out of the way from playing with it. Out of
// the way is a clear move somewhere else; play is a short wiggle, a long
// carry, or a winding path that ends near where it started.
func (w *window) dragKind(end point) rl.Reaction {
	moved := math.Hypot(float64(end.x-w.pressAt.x), float64(end.y-w.pressAt.y)) / w.scale
	path := w.pathLen / w.scale
	held := time.Since(w.grabbedAt)
	if moved < 40 || path > 2.5*moved || held > 3*time.Second {
		return rl.Played
	}
	return rl.Dragged
}
