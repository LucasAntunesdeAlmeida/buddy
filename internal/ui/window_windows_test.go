package ui

import (
	"testing"
	"time"

	"buddy/internal/rl"
)

func TestDragKind(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		end  point
		path float64
		held time.Duration
		want rl.Reaction
	}{
		{"moved clearly out of the way", point{300, 0}, 320, time.Second, rl.Dragged},
		{"tiny wiggle", point{20, 10}, 60, time.Second, rl.Played},
		{"swung around and back", point{60, 0}, 600, 2 * time.Second, rl.Played},
		{"carried around for a while", point{300, 0}, 400, 5 * time.Second, rl.Played},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			w := &window{scale: 1, pathLen: tt.path, grabbedAt: time.Now().Add(-tt.held)}
			if got := w.dragKind(tt.end); got != tt.want {
				t.Errorf("dragKind = %v, want %v", got, tt.want)
			}
		})
	}
}
