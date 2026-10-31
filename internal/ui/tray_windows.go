package ui

import (
	"fmt"
	"log/slog"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"buddy/internal/brain"
	"buddy/internal/rl"
)

var (
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW          = shell32.NewProc("ShellExecuteW")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	procCreateBitmap           = gdi32.NewProc("CreateBitmap")
	procMonitorFromPoint       = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
)

const (
	nimAdd         = 0
	nimDelete      = 2
	nifMessage     = 0x1
	nifIcon        = 0x2
	nifTip         = 0x4
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	mfGrayed       = 0x0001
	mfChecked      = 0x0008
	smCXSmIcon     = 49
	swShowNormal   = 1
	monitorNearest = 2
	menuPause      = 3
	menuResume     = 4
	menuSound      = 5
	menuAutostart  = 6
	menuReport     = 7
	pauseFor       = time.Hour
)

type notifyIconData struct {
	size            uint32
	wnd             uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        windows.GUID
	balloonIcon     uintptr
}

type iconInfo struct {
	isIcon   int32
	hotspotX uint32
	hotspotY uint32
	mask     uintptr
	color    uintptr
}

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

// taskbarCreated is sent when Explorer restarts; the tray icon must be added again.
var taskbarCreated = func() uint32 {
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	return uint32(r)
}()

// addTray puts Buddy's icon in the notification area.
func (w *window) addTray() {
	if w.trayIcon == 0 {
		dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
		size, _, _ := procGetSystemMetricsForDpi.Call(smCXSmIcon, max(dpi, 96))
		icon, err := makeIcon(int(max(size, 16)))
		if err != nil {
			slog.Warn("no tray icon", "err", err)
			return
		}
		w.trayIcon = icon
	}
	nid := w.trayData()
	nid.flags = nifMessage | nifIcon | nifTip
	nid.callbackMessage = wmTray
	nid.icon = w.trayIcon
	copy(nid.tip[:], windows.StringToUTF16("Buddy"))
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

func (w *window) removeTray() {
	nid := w.trayData()
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	if w.trayIcon != 0 {
		procDestroyIcon.Call(w.trayIcon)
		w.trayIcon = 0
	}
}

func (w *window) trayData() notifyIconData {
	nid := notifyIconData{wnd: w.hwnd, id: 1}
	nid.size = uint32(unsafe.Sizeof(nid))
	return nid
}

// makeIcon turns a drawing of Buddy into a Windows icon.
func makeIcon(size int) (uintptr, error) {
	img, err := IconImage(size)
	if err != nil {
		return 0, err
	}
	bi := bitmapInfo{width: int32(size), height: -int32(size), planes: 1, bitCount: 32}
	bi.size = uint32(unsafe.Offsetof(bi.colors))
	var bits unsafe.Pointer
	screen, _, _ := procGetDC.Call(0)
	color, _, err := procCreateDIBSection.Call(screen, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	procReleaseDC.Call(0, screen)
	if color == 0 {
		return 0, fmt.Errorf("icon bitmap: %w", err)
	}
	defer procDeleteObject.Call(color)
	px := unsafe.Slice((*byte)(bits), size*size*4)
	for i := 0; i < len(px); i += 4 {
		// Icons want straight alpha in BGRA order; the drawing is premultiplied RGBA.
		r, g, b, a := img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
		if a > 0 && a < 255 {
			r, g, b = byte(int(r)*255/int(a)), byte(int(g)*255/int(a)), byte(int(b)*255/int(a))
		}
		px[i], px[i+1], px[i+2], px[i+3] = b, g, r, a
	}
	mask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	defer procDeleteObject.Call(mask)
	ii := iconInfo{isIcon: 1, mask: mask, color: color}
	icon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if icon == 0 {
		return 0, fmt.Errorf("create icon: %w", err)
	}
	return icon, nil
}

// menu shows Buddy's menu at the cursor. From Buddy itself it also offers
// "go away", which teaches Buddy; the rest are plain switches.
func (w *window) menu(onBuddy bool) {
	now := time.Now()
	m, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(m)
	add := func(flags, id uintptr, text string) {
		p, _ := windows.UTF16PtrFromString(text)
		procAppendMenuW.Call(m, mfString|flags, id, uintptr(unsafe.Pointer(p)))
	}
	sep := func() { procAppendMenuW.Call(m, mfSeparator, 0, 0) }
	checked := func(on bool) uintptr {
		if on {
			return mfChecked
		}
		return 0
	}

	if onBuddy {
		add(0, menuAway, fmt.Sprintf("Go away for %d minutes", int(brain.MuteFor.Minutes())))
		sep()
	}
	if w.opts.paused(now) {
		add(0, menuResume, "Resume Buddy")
	} else {
		add(0, menuPause, "Pause for 1 hour")
	}
	add(checked(w.opts.soundOn()), menuSound, "Sound")
	if a := w.opts.Autostart; a != nil {
		if a.Available() {
			add(checked(a.Enabled()), menuAutostart, "Start with Windows")
		} else {
			add(mfGrayed, menuAutostart, "Start with Windows (needs a built buddy.exe)")
		}
	}
	if w.opts.Report != nil {
		add(0, menuReport, "What has Buddy learned?")
	}
	sep()
	add(0, menuQuit, "Quit Buddy")

	c := cursor()
	// A popup menu only closes properly when its window is in the foreground.
	procSetForegroundWindow.Call(w.hwnd)
	choice, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton|tpmNoNotify, uintptr(c.x), uintptr(c.y), 0, w.hwnd, 0)
	procPostMessageW.Call(w.hwnd, wmNull, 0, 0)

	var err error
	switch choice {
	case menuAway:
		w.send(rl.Dismissed)
		w.scene.Sleep(now)
		w.walking = false
	case menuPause:
		err = w.opts.Settings.PauseUntil(now.Add(pauseFor))
		w.scene.Sleep(now)
		w.walking = false
	case menuResume:
		err = w.opts.Settings.PauseUntil(time.Time{})
		w.scene.Apply(brain.Command{}, now) // wakes up with a stretch
	case menuSound:
		err = w.opts.Settings.SetSound(!w.opts.soundOn())
	case menuAutostart:
		err = w.opts.Autostart.Set(!w.opts.Autostart.Enabled())
	case menuReport:
		var path string
		if path, err = w.opts.Report(); err == nil {
			p, _ := windows.UTF16PtrFromString(path)
			verb, _ := windows.UTF16PtrFromString("open")
			procShellExecuteW.Call(w.hwnd, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(p)), 0, 0, swShowNormal)
		}
	case menuQuit:
		procDestroyWindow.Call(w.hwnd)
		return
	}
	if err != nil {
		slog.Error("menu action failed", "err", err)
	}
	w.present()
	w.pace(now)
}

// workAreaAt returns the usable part (without the taskbar) of the monitor
// nearest to p.
func workAreaAt(p point) rect {
	mon, _, _ := procMonitorFromPoint.Call(uintptr(uint32(p.x))|uintptr(uint32(p.y))<<32, monitorNearest)
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ok, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); ok == 0 {
		var work rect
		procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0)
		return work
	}
	return mi.work
}
