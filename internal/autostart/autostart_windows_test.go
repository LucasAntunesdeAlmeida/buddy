package autostart

import (
	"os"
	"path/filepath"
	"testing"
)

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
