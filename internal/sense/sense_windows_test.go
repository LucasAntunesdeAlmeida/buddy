package sense

import (
	"testing"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/activity"
)

// Polls the real desktop once; checks the Win32 calls work, not their values.
func TestPoll(t *testing.T) {
	w := NewWatcher()
	w.poll()
	s := w.Snapshot()
	if s.Activity < 0 || s.Activity > 1 {
		t.Errorf("Activity = %v, want between 0 and 1", s.Activity)
	}
	if s.Category >= activity.NumCategories {
		t.Errorf("Category = %d, out of range", s.Category)
	}
	if s.Idle < 0 {
		t.Errorf("Idle = %v, want >= 0", s.Idle)
	}
	t.Logf("busy: %q", busyReason())
	if _, _, ok := CursorPos(); !ok {
		t.Log("no cursor position (headless session?)")
	}
}
