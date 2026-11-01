// Package autostart starts Buddy when you sign in to Windows, through the
// per-user Run key. It needs no admin rights and is easy to undo.
package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	name   = "Buddy"
)

// Entry is Buddy's sign-in entry.
type Entry struct {
	exe  string
	args []string
}

// New returns the entry for the running buddy.exe, started with args.
func New(args ...string) *Entry {
	exe, _ := os.Executable()
	return &Entry{exe: exe, args: args}
}

// Available reports whether this exe will still exist after a restart. A
// go run build lives in a temporary folder and is deleted on exit.
func (e *Entry) Available() bool {
	if e.exe == "" {
		return false
	}
	exe, tmp := strings.ToLower(e.exe), strings.ToLower(os.TempDir())
	return !strings.HasPrefix(exe, tmp) && !strings.Contains(exe, `\go-build`)
}

func (e *Entry) quotedExe() string { return `"` + filepath.Clean(e.exe) + `"` }

func (e *Entry) command() string {
	cmd := e.quotedExe()
	for _, a := range e.args {
		cmd += " " + windows.EscapeArg(a)
	}
	return cmd
}

// starts reports whether a sign-in command starts this exe, with any flags.
func (e *Entry) starts(command string) bool {
	command, exe := strings.ToLower(command), strings.ToLower(e.quotedExe())
	return command == exe || strings.HasPrefix(command, exe+" ")
}

// Enabled reports whether Buddy starts with Windows, as this exe. The flags
// don't matter: Buddy started by hand with other flags still starts with
// Windows, and the tray menu should say so.
func (e *Entry) Enabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return err == nil && e.starts(v)
}

// Set turns starting with Windows on or off.
func (e *Entry) Set(on bool) error {
	if on && !e.Available() {
		return errors.New("start with Windows needs a built buddy.exe, not go run")
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open sign-in programs: %w", err)
	}
	defer k.Close()
	if on {
		err = k.SetStringValue(name, e.command())
	} else if err = k.DeleteValue(name); errors.Is(err, registry.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("change sign-in programs: %w", err)
	}
	return nil
}
