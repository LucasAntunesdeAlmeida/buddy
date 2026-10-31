package ui

import (
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"buddy/internal/brain"
	"buddy/internal/rl"
)

var t0 = time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)

// A pose is something Buddy can be doing, set up to start at t0.
type pose struct {
	name   string
	start  func(s *Scene)
	frame  func(s *Scene, t time.Duration) // optional, before each frame
	length time.Duration                   // how long to record
	still  time.Duration                   // when to take the still picture
}

func command(c brain.Command) func(*Scene) {
	return func(s *Scene) { s.Apply(c, t0) }
}

const sample = "You've been coding a while. Stretch?"

var poses = []pose{
	{name: "idle", start: func(*Scene) {}, length: 2 * time.Second, still: 300 * time.Millisecond},
	{name: "fidget", start: command(brain.Command{Action: rl.Fidget}), still: 400 * time.Millisecond},
	{name: "wave", start: command(brain.Command{Action: rl.Wave}), still: 600 * time.Millisecond},
	{name: "bounce", start: command(brain.Command{Action: rl.Bounce}), still: 300 * time.Millisecond},
	{name: "chirp", start: command(brain.Command{Action: rl.Chirp}), still: 450 * time.Millisecond},
	{name: "speak", start: command(brain.Command{Action: rl.Speak, Phrase: sample}), still: 2500 * time.Millisecond},
	{name: "approach", start: command(brain.Command{Action: rl.Approach}), still: 200 * time.Millisecond},
	{name: "clicked", start: func(s *Scene) { s.Poke(t0) }, length: happyFor, still: 150 * time.Millisecond},
	{
		name:  "dragged",
		start: func(s *Scene) { s.Grab(t0) },
		frame: func(s *Scene, t time.Duration) {
			// Carried right, then left, then held still.
			if t < 1600*time.Millisecond {
				s.Drag(600 * math.Sin(t.Seconds()*2*math.Pi/1.6))
			}
		},
		length: 2400 * time.Millisecond, still: 500 * time.Millisecond,
	},
	{
		name: "dropped",
		start: func(s *Scene) {
			s.Grab(t0.Add(-time.Second))
			s.Release(t0)
		},
		length: dropFor, still: 60 * time.Millisecond,
	},
	{name: "yawn", start: func(s *Scene) { s.Sleep(t0) }, length: yawnFor + 400*time.Millisecond, still: 700 * time.Millisecond},
	{
		name:   "asleep",
		start:  func(s *Scene) { s.Sleep(t0.Add(-time.Minute)) },
		length: 2400 * time.Millisecond, still: time.Second,
	},
	{
		name: "waking",
		start: func(s *Scene) {
			s.Sleep(t0.Add(-time.Minute))
			s.Apply(brain.Command{}, t0)
		},
		length: wakeFor, still: 700 * time.Millisecond,
	},
}

func (p pose) recordLength() time.Duration {
	if p.length > 0 {
		return p.length
	}
	s, _ := NewScene(1)
	p.start(s)
	return s.ends.Sub(t0)
}

// play sets up a fresh scene for the pose and renders it at time at,
// stepping through the frames before it so motion that builds up over time
// (like the drag swing) is right.
func (p pose) play(t *testing.T, scale float64, at time.Duration) *image.RGBA {
	t.Helper()
	s, err := NewScene(scale)
	if err != nil {
		t.Fatal(err)
	}
	p.start(s)
	const step = 50 * time.Millisecond
	for d := time.Duration(0); d < at; d += step {
		if p.frame != nil {
			p.frame(s, d)
		}
		s.Render(t0.Add(d))
	}
	if p.frame != nil {
		p.frame(s, at)
	}
	return s.Render(t0.Add(at))
}

// Set BUDDY_SNAPSHOTS to a folder to save a contact sheet of every pose on a
// light and a dark background, to judge the design.
func TestRenderEveryPose(t *testing.T) {
	out := os.Getenv("BUDDY_SNAPSHOTS")
	for _, scale := range []float64{1, 1.5} {
		w, h := int(math.Ceil(Width*scale)), int(math.Ceil(Height*scale))
		sheet := image.NewRGBA(image.Rect(0, 0, w*len(poses), h*2))
		draw.Draw(sheet, image.Rect(0, 0, w*len(poses), h), image.NewUniform(color.RGBA{0xf3, 0xf3, 0xf3, 0xff}), image.Point{}, draw.Src)
		draw.Draw(sheet, image.Rect(0, h, w*len(poses), 2*h), image.NewUniform(color.RGBA{0x1e, 0x1e, 0x1e, 0xff}), image.Point{}, draw.Src)

		for i, p := range poses {
			img := p.play(t, scale, p.still)
			if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
				t.Fatalf("%s at %v: image is %v, want %dx%d", p.name, scale, b, w, h)
			}
			opaque := 0
			for i := 3; i < len(img.Pix); i += 4 {
				if img.Pix[i] == 0xff {
					opaque++
				}
			}
			if opaque < 1000 {
				t.Errorf("%s at %v: only %d opaque pixels, Buddy is missing", p.name, scale, opaque)
			}
			if img.Pix[3] != 0 {
				t.Errorf("%s at %v: corner is not transparent", p.name, scale)
			}
			for row := range 2 {
				r := image.Rect(i*w, row*h, (i+1)*w, (row+1)*h)
				draw.Draw(sheet, r, img, image.Point{}, draw.Over)
				d := font.Drawer{Dst: sheet, Src: image.NewUniform(color.RGBA{0x80, 0x80, 0x80, 0xff}), Face: basicfont.Face7x13, Dot: fixed.P(r.Min.X+6, r.Min.Y+14)}
				d.DrawString(p.name)
			}
		}
		if out != "" {
			save(t, filepath.Join(out, fmt.Sprintf("poses@%v.png", scale)), func(f *os.File) error { return png.Encode(f, sheet) })
		}
	}
}

// With BUDDY_SNAPSHOTS set, also saves every animation as a GIF and as a
// film strip of frames side by side.
func TestRecordAnimations(t *testing.T) {
	out := os.Getenv("BUDDY_SNAPSHOTS")
	if out == "" {
		t.Skip("set BUDDY_SNAPSHOTS to record animations")
	}
	const fps = 20
	bg := image.NewUniform(color.RGBA{0xf3, 0xf3, 0xf3, 0xff})
	for _, p := range poses {
		s, err := NewScene(1)
		if err != nil {
			t.Fatal(err)
		}
		p.start(s)
		length := p.recordLength()
		anim := &gif.GIF{}
		strip := image.NewRGBA(image.Rect(0, 0, Width*12, Height))
		draw.Draw(strip, strip.Bounds(), bg, image.Point{}, draw.Src)
		n := int(length.Seconds() * fps)
		var flats []*image.RGBA
		for i := range n {
			at := time.Duration(i) * time.Second / fps
			if p.frame != nil {
				p.frame(s, at)
			}
			img := s.Render(t0.Add(at))
			flat := image.NewRGBA(img.Bounds())
			draw.Draw(flat, flat.Bounds(), bg, image.Point{}, draw.Src)
			draw.Draw(flat, flat.Bounds(), img, image.Point{}, draw.Over)
			frame := image.NewPaletted(img.Bounds(), palette.Plan9)
			draw.Draw(frame, frame.Bounds(), flat, image.Point{}, draw.Src)
			anim.Image = append(anim.Image, frame)
			anim.Delay = append(anim.Delay, 100/fps)
			flats = append(flats, flat)
		}
		for k := range 12 {
			draw.Draw(strip, image.Rect(k*Width, 0, (k+1)*Width, Height), flats[k*n/12], image.Point{}, draw.Src)
		}
		save(t, filepath.Join(out, p.name+".gif"), func(f *os.File) error { return gif.EncodeAll(f, anim) })
		save(t, filepath.Join(out, p.name+"-strip.png"), func(f *os.File) error { return png.Encode(f, strip) })
	}
}

func save(t *testing.T, path string, write func(f *os.File) error) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := write(f); err != nil {
		t.Fatal(err)
	}
}

func TestAnimationsEnd(t *testing.T) {
	s, err := NewScene(1)
	if err != nil {
		t.Fatal(err)
	}
	for a := rl.Fidget; a < rl.NumActions; a++ {
		s.Apply(brain.Command{Action: a, Phrase: "hi"}, t0)
		if !s.Animating(t0) {
			t.Errorf("%v is not animating right after it starts", a)
		}
		if s.Animating(t0.Add(10 * time.Second)) {
			t.Errorf("%v still animating after 10s", a)
		}
	}
}

func TestTransitions(t *testing.T) {
	s, err := NewScene(1)
	if err != nil {
		t.Fatal(err)
	}
	s.Sleep(t0)
	if !s.Animating(t0.Add(time.Second)) {
		t.Error("not yawning right after falling asleep")
	}
	if s.Animating(t0.Add(yawnFor + time.Millisecond)) {
		t.Error("still yawning after yawnFor")
	}
	if got := s.sleepiness(t0.Add(yawnFor)); got != 1 {
		t.Errorf("sleepiness after the yawn = %v, want 1", got)
	}

	wake := t0.Add(time.Minute)
	s.Poke(wake)
	if s.Asleep() || !s.waking(wake.Add(time.Second)) {
		t.Error("a poke should wake Buddy with a stretch")
	}
	if got := s.sleepiness(wake.Add(time.Second)); got != 0 {
		t.Errorf("sleepiness a second after waking = %v, want 0", got)
	}

	s.Grab(wake)
	if !s.Animating(wake.Add(time.Hour)) {
		t.Error("a held Buddy should keep animating")
	}
	s.Release(wake.Add(time.Hour))
	if s.Animating(wake.Add(time.Hour + dropFor + happyFor)) {
		t.Error("still animating long after being dropped")
	}
}

func TestChirpIsWAV(t *testing.T) {
	b := chirpWAV()
	if string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || len(b) < 1000 {
		t.Errorf("chirp is not a WAV file: % x", b[:12])
	}
}

func BenchmarkRender(b *testing.B) {
	s, err := NewScene(1)
	if err != nil {
		b.Fatal(err)
	}
	now := time.Now()
	for b.Loop() {
		s.Render(now)
	}
}

func TestIconImage(t *testing.T) {
	for _, size := range []int{16, 32, 48} {
		img, err := IconImage(size)
		if err != nil {
			t.Fatal(err)
		}
		opaque := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] == 0xff {
				opaque++
			}
		}
		if opaque < size*size/4 {
			t.Errorf("%dpx icon is only %d/%d opaque, Buddy should fill it", size, opaque, size*size)
		}
	}
}
