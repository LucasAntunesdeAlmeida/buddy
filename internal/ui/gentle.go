package ui

import (
	"image/color"
	"math"
	"time"
)

// The gentle, silent hellos: peeking and emoting.

const peekFor = 2200 * time.Millisecond

// peek sinks Buddy out of sight below his spot, lets him peek left and
// right with just his sprout and eyes showing, then pops him back up.
func peek(l *look, t float64) {
	const depth = 42.0
	switch {
	case t < 0.45: // crouch and sink
		d := smoothstep(t / 0.45)
		l.lift = -depth * d
		l.sy -= 0.08 * math.Sin(math.Pi*d)
	case t < 1.15: // hidden, peeking left and right
		l.lift = -depth
		l.pupil = 3.5 * math.Sin((t-0.45)/0.7*2*math.Pi)
		l.sprout += 0.15 * math.Sin(t*12)
	case t < 1.6: // pop up, a little past his spot
		p := (t - 1.15) / 0.45
		l.lift = -depth*(1-easeOutBack(p)) + 8*math.Sin(math.Pi*p)
		l.sy += 0.08 * math.Sin(math.Pi*p)
		l.eyes, l.mouth = eyesHappy, mouthGrin
	default: // ta-da
		raise := envelope(t-1.6, 0.6, 0.15)
		l.armL, l.armR = 0.35+1.8*raise, 0.35+1.8*raise
		l.eyes, l.mouth = eyesHappy, mouthGrin
	}
}

// emote is the picture in an Emote bubble.
type emote uint8

const (
	emoteHeart emote = iota
	emoteSparkle
	emoteExclaim
	emoteQuestion
	emoteNote
	numEmotes
)

const emoteFor = 2400 * time.Millisecond

var sparkleColor = color.NRGBA{0xf5, 0xb8, 0x2e, 0xff}

// emoteFace matches Buddy's face to the bubble he shows.
func (s *Scene) emoteFace(l *look, t float64) {
	switch s.emote {
	case emoteHeart, emoteSparkle:
		l.eyes, l.mouth = eyesHappy, mouthGrin
	case emoteExclaim:
		l.mouth = mouthO
		if t < 0.4 {
			hop(l, t, 0.4, 6)
		}
	case emoteQuestion:
		l.pupil, l.lean = 2.5, 1.5 // a curious sideways look
	case emoteNote:
		l.mouth = mouthO
		l.lean = 2 * math.Sin(t*6) // swaying to the tune
		l.sprout += 0.25 * math.Sin(t*6)
	}
}

// drawEmote draws the bubble: it grows out of Buddy's head through a trail
// of dots, floats a little, and shrinks away at the end.
func (s *Scene) drawEmote(now time.Time, cx, rx, top float64) {
	t := now.Sub(s.started).Seconds()
	total := emoteFor.Seconds()
	grow := easeOutBack(t / 0.25)
	if t > total-0.3 {
		grow = min(grow, smoothstep((total-t)/0.3))
	}
	if grow < 0.05 {
		return
	}
	bx, by := cx+rx*0.85, top-22+2*math.Sin(t*3)
	dot := func(x, y, r float64) {
		s.outlined(func(o float64, c color.NRGBA) { s.ellipse(x, y, r*grow+o, r*grow+o, 0, c) }, paper)
	}
	dot(cx+rx*0.45, top-4, 2.5)
	dot(cx+rx*0.62, top-10, 4)
	dot(bx, by, 15)

	k := grow
	switch s.emote {
	case emoteHeart:
		s.heart(bx, by+1, 9*k)
	case emoteSparkle:
		s.sparkle(bx, by, 10*k)
	case emoteExclaim:
		s.fill(bx-3, by-10*k, bx+3, by+2*k, ink, func(p pather) { p.roundRect(bx-2.2*k, by-9*k, 4.4*k, 11*k, 2*k) })
		s.ellipse(bx, by+6*k, 2.4*k, 2.4*k, 0, ink)
	case emoteQuestion:
		w := 2.8 * k
		s.curve(vec{bx - 5*k, by - 4*k}, vec{bx - 3*k, by - 12*k}, vec{bx + 2*k, by - 10*k}, w, ink)
		s.curve(vec{bx + 2*k, by - 10*k}, vec{bx + 7*k, by - 7*k}, vec{bx, by + 1*k}, w, ink)
		s.ellipse(bx, by+6.5*k, 2.2*k, 2.2*k, 0, ink)
	case emoteNote:
		s.note(bx-2*k, by+5*k, 0.85*k)
	}
}

// sparkle draws a four-pointed star.
func (s *Scene) sparkle(x, y, r float64) {
	if r < 2 {
		return
	}
	star := func(r float64, c color.NRGBA) {
		s.fill(x-r, y-r, x+r, y+r, c, func(p pather) {
			for i := range 8 {
				a := float64(i)*math.Pi/4 - math.Pi/2
				rr := r
				if i%2 == 1 {
					rr = r * 0.38
				}
				v := vec{x + rr*math.Cos(a), y + rr*math.Sin(a)}
				if i == 0 {
					p.moveTo(v)
				} else {
					p.lineTo(v)
				}
			}
			p.close()
		})
	}
	star(r+outline*1.6, lineColor)
	star(r, sparkleColor)
}
