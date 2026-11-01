package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommand(t *testing.T) {
	t.Parallel()
	const exe = `C:\Apps\My Buddy\buddy.exe`
	tests := []struct {
		args []string
		want string
	}{
		{nil, `"C:\Apps\My Buddy\buddy.exe"`},
		{[]string{"-voice=true"}, `"C:\Apps\My Buddy\buddy.exe" -voice=true`},
		{[]string{`-data=C:\Some Folder`, "-v=true"}, `"C:\Apps\My Buddy\buddy.exe" "-data=C:\Some Folder" -v=true`},
	}
	for _, tt := range tests {
		if got := (&Entry{exe: exe, args: tt.args}).command(); got != tt.want {
			t.Errorf("command with %q = %s, want %s", tt.args, got, tt.want)
		}
	}
}

func TestStarts(t *testing.T) {
	t.Parallel()
	e := &Entry{exe: `C:\Apps\Buddy\buddy.exe`}
	tests := []struct {
		command string
		want    bool
	}{
		{`"C:\Apps\Buddy\buddy.exe"`, true},
		{`"c:\apps\buddy\BUDDY.EXE"`, true},
		{`"C:\Apps\Buddy\buddy.exe" -voice=true`, true},
		{`"C:\Other\buddy.exe"`, false},
		{`"C:\Apps\Buddy\buddy.exe.bak"`, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := e.starts(tt.command); got != tt.want {
			t.Errorf("starts(%s) = %v, want %v", tt.command, got, tt.want)
		}
	}
}

// Only checks where Buddy may register from; it never touches the registry.
func TestAvailable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		exe  string
		want bool
	}{
		{filepath.Join(os.TempDir(), "go-build123", "b001", "exe", "buddy.exe"), false},
		{`C:\Users\me\go\pkg\go-build\buddy.exe`, false},
		{`C:\Apps\Buddy\buddy.exe`, true},
		{"", false},
	}
	for _, tt := range tests {
		if got := (&Entry{exe: tt.exe}).Available(); got != tt.want {
			t.Errorf("Available for %q = %v, want %v", tt.exe, got, tt.want)
		}
	}
}
