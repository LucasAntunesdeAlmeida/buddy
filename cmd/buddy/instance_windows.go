package main

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errAlreadyRunning = errors.New("Buddy is already running")

// claimInstance makes sure only one Buddy runs per user, since two would
// fight over the same files. release gives the claim back.
func claimInstance() (release func(), err error) {
	name, _ := windows.UTF16PtrFromString(`Local\BuddyDesktopCompanion`)
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		windows.CloseHandle(h)
		return nil, errAlreadyRunning
	}
	if err != nil {
		return nil, fmt.Errorf("check for another Buddy: %w", err)
	}
	return func() { windows.CloseHandle(h) }, nil
}

// notify shows a message box, for errors a console-less Buddy can't print.
func notify(text string) {
	t, _ := windows.UTF16PtrFromString(text)
	c, _ := windows.UTF16PtrFromString("Buddy")
	const mbIconInformation = 0x40
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), mbIconInformation)
}
