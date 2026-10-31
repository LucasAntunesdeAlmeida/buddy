package ui

import (
	"image"
	"time"

	"github.com/LucasAntunesdeAlmeida/buddy/internal/settings"
)

// Options are what the window needs besides the brain's channels.
type Options struct {
	Settings  *settings.Settings
	Autostart Autostart
	// Report writes the "what has Buddy learned?" report and returns its path.
	Report func() (string, error)
	// OnEnd runs when Windows signs out or shuts down. It must save what
	// Buddy learned before returning, because Windows ends the app right after.
	OnEnd func()
}

// Autostart turns starting with Windows on and off.
type Autostart interface {
	// Available is false when Buddy runs from a temporary build that
	// won't exist next time, as with go run.
	Available() bool
	Enabled() bool
	Set(on bool) error
}

func (o Options) soundOn() bool { return o.Settings == nil || o.Settings.SoundOn() }

func (o Options) paused(now time.Time) bool { return o.Settings != nil && o.Settings.Paused(now) }

// IconImage draws Buddy for an icon size×size pixels: body, face and sprout,
// cropped tight.
func IconImage(size int) (*image.RGBA, error) {
	const box = 100.0 // scene units around Buddy, sprout included
	s, err := NewScene(float64(size) / box)
	if err != nil {
		return nil, err
	}
	// A moment when Buddy isn't blinking.
	img := s.Render(time.UnixMilli(1_000_000))
	k := s.scale
	x0, y0 := int((bodyX-box/2)*k), int((ground+8-box)*k)
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			out.SetRGBA(x, y, img.RGBAAt(x0+x, y0+y))
		}
	}
	return out, nil
}
