// Package ui is Buddy's body: a small transparent window that floats above
// other windows, draws Buddy, plays its animations and turns mouse input into
// reactions for the brain. Buddy is drawn on the CPU into a small bitmap, so
// it never loads a graphics driver.
package ui

import (
	"image"
	"image/color"
	"math"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"buddy/internal/brain"
	"buddy/internal/rl"
)

// Window size and Buddy's proportions, in pixels at 100% display scaling.
const (
	Width   = 240
	Height  = 200
	bodyX   = Width / 2
	ground  = 184.0 // where Buddy stands
	bodyRX  = 37.0  // half the body's width
	bodyH   = 64.0  // the body's height
	outline = 2.2   // width of the dark outline around everything
)

// Speech timing.
const (
	popIn    = 180 * time.Millisecond // the bubble grows in
	typeRate = 40                     // characters per second
	readTime = 3500 * time.Millisecond
)

var (
	lineColor  = color.NRGBA{0x24, 0x49, 0x4a, 0xff}
	bodyColor  = color.NRGBA{0x7a, 0xd8, 0xc1, 0xff}
	bodyShade  = color.NRGBA{0x55, 0xb7, 0xa1, 0xff}
	sleepBody  = color.NRGBA{0x6c, 0xc4, 0xb0, 0xff}
	sleepShade = color.NRGBA{0x4c, 0xa5, 0x91, 0xff}
	shine      = color.NRGBA{0xff, 0xff, 0xff, 0x70}
	ink        = color.NRGBA{0x23, 0x32, 0x36, 0xff}
	cheekColor = color.NRGBA{0xff, 0x8f, 0xa3, 0x90}
	tongue     = color.NRGBA{0xff, 0x7b, 0x8e, 0xff}
	stemColor  = color.NRGBA{0x4f, 0x9f, 0x6a, 0xff}
	leafColor  = color.NRGBA{0x95, 0xdc, 0x7f, 0xff}
	heartColor = color.NRGBA{0xff, 0x6b, 0x88, 0xff}
	shadow     = color.NRGBA{0x00, 0x00, 0x00, 0x30}
	paper      = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

// Scene is what Buddy is doing and how it looks. It knows nothing about windows.
type Scene struct {
	scale float64
	font  *opentype.Font
	face  font.Face
	img   *image.RGBA
	rast  vector.Rasterizer

	asleep  bool
	anim    rl.Action
	started time.Time
	ends    time.Time
	phrase  []rune
	lines   []string
	clicked time.Time
	facing  float64   // -1 left, 1 right
	sleptAt time.Time // when Buddy last fell asleep, for the yawn
	wokeAt  time.Time // when Buddy last woke up, for the stretch

	// Being carried by the mouse.
	grabbed  bool
	grabAt   time.Time
	dropped  time.Time
	dragV    float64 // sideways speed, scene units per second
	swing    float64 // smoothed sideways swing
	lastDraw time.Time
	emote    emote // which bubble the current Emote shows
}

// Effect is what the window must do for a command besides drawing.
type Effect struct {
	Sound bool // play the chirp
	Walk  bool // walk toward the cursor
}

// NewScene returns a scene drawn at the given display scale (1 = 100%).
func NewScene(scale float64) (*Scene, error) {
	f, err := opentype.Parse(gomedium.TTF)
	if err != nil {
		return nil, err
	}
	s := &Scene{font: f, anim: rl.Quiet, facing: 1}
	return s, s.SetScale(scale)
}

// SetScale changes the display scale, for example when Buddy moves to
// another monitor.
func (s *Scene) SetScale(scale float64) error {
	face, err := opentype.NewFace(s.font, &opentype.FaceOptions{Size: 13 * scale, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return err
	}
	s.scale, s.face = scale, face
	w, h := s.Size()
	s.img = image.NewRGBA(image.Rect(0, 0, w, h))
	s.lines = s.wrap(string(s.phrase), bubbleMaxW)
	return nil
}

// Size is the bitmap size in physical pixels.
func (s *Scene) Size() (w, h int) {
	return int(math.Ceil(Width * s.scale)), int(math.Ceil(Height * s.scale))
}

func duration(a rl.Action, phrase []rune) time.Duration {
	switch a {
	case rl.Fidget:
		return 1600 * time.Millisecond
	case rl.Wave:
		return 2 * time.Second
	case rl.Bounce:
		return 1200 * time.Millisecond
	case rl.Chirp:
		return 1600 * time.Millisecond
	case rl.Approach:
		return 3 * time.Second
	case rl.Peek:
		return peekFor
	case rl.Emote:
		return emoteFor
	case rl.Speak:
		return popIn + time.Duration(len(phrase))*time.Second/typeRate + readTime
	}
	return 0
}

// Apply starts what the brain asked for.
func (s *Scene) Apply(c brain.Command, now time.Time) Effect {
	s.stop()
	s.setAsleep(c.Asleep, now)
	if c.Asleep {
		return Effect{}
	}
	s.anim, s.started = c.Action, now
	if c.Action == rl.Speak {
		s.phrase = []rune(c.Phrase)
		s.lines = s.wrap(c.Phrase, bubbleMaxW)
	}
	s.ends = now.Add(duration(c.Action, s.phrase))
	switch c.Action {
	case rl.Chirp:
		return Effect{Sound: true}
	case rl.Approach:
		return Effect{Walk: true}
	case rl.Emote:
		s.emote = emote(now.UnixMilli() % int64(numEmotes))
	}
	return Effect{}
}

func (s *Scene) stop() { s.anim, s.phrase, s.lines = rl.Quiet, nil, nil }

// Face turns Buddy toward a direction: negative is left, positive right.
func (s *Scene) Face(dir float64) {
	if dir < 0 {
		s.facing = -1
	} else {
		s.facing = 1
	}
}

// Poke shows that Buddy was clicked, and wakes it up.
func (s *Scene) Poke(now time.Time) {
	s.clicked = now
	s.setAsleep(false, now)
}

// Sleep puts Buddy to sleep at once, without waiting for the brain.
func (s *Scene) Sleep(now time.Time) { s.setAsleep(true, now) }

// setAsleep changes state with a yawn on the way down and a stretch on the
// way up.
func (s *Scene) setAsleep(asleep bool, now time.Time) {
	if asleep == s.asleep {
		return
	}
	s.asleep = asleep
	if asleep {
		s.sleptAt = now
		s.stop()
	} else {
		s.wokeAt = now
	}
}

// Asleep reports whether Buddy is sleeping.
func (s *Scene) Asleep() bool { return s.asleep }

// Grab starts a drag: Buddy dangles from the cursor until Release.
func (s *Scene) Grab(now time.Time) {
	s.grabbed, s.grabAt, s.dragV = true, now, 0
}

// Drag tells the scene how fast Buddy is carried sideways, in scene units
// per second, so it can swing behind the motion.
func (s *Scene) Drag(vx float64) { s.dragV = vx }

// Release drops Buddy where it is.
func (s *Scene) Release(now time.Time) {
	s.grabbed, s.dropped = false, now
}

// Walking reports whether the walk animation is still playing.
func (s *Scene) Walking(now time.Time) bool {
	return s.anim == rl.Approach && s.Animating(now)
}

// How long the reactions and transitions last.
const (
	happyFor = 1100 * time.Millisecond
	yawnFor  = 1800 * time.Millisecond
	wakeFor  = 1400 * time.Millisecond
	dropFor  = 700 * time.Millisecond
)

// Animating reports whether Buddy is moving fast enough to need every frame.
// An animation ends on its own once it has played.
func (s *Scene) Animating(now time.Time) bool {
	if s.anim != rl.Quiet && !now.Before(s.ends) {
		s.stop()
	}
	return s.anim != rl.Quiet || s.grabbed || s.happy(now) || s.yawning(now) || s.waking(now) || s.landing(now)
}

func (s *Scene) yawning(now time.Time) bool {
	return s.asleep && !s.sleptAt.IsZero() && now.Sub(s.sleptAt) < yawnFor
}

func (s *Scene) waking(now time.Time) bool {
	return !s.asleep && !s.wokeAt.IsZero() && now.Sub(s.wokeAt) < wakeFor
}

func (s *Scene) landing(now time.Time) bool {
	return !s.dropped.IsZero() && now.Sub(s.dropped) < dropFor
}

// sleepiness fades Buddy's color between awake (0) and asleep (1).
func (s *Scene) sleepiness(now time.Time) float64 {
	switch {
	case s.asleep && s.sleptAt.IsZero():
		return 1
	case s.asleep:
		return min(1, now.Sub(s.sleptAt).Seconds()/yawnFor.Seconds())
	case s.waking(now):
		return max(0, 1-now.Sub(s.wokeAt).Seconds()/0.6)
	}
	return 0
}

func (s *Scene) happy(now time.Time) bool {
	return !s.clicked.IsZero() && now.Sub(s.clicked) < happyFor
}

type eyes uint8

const (
	eyesOpen eyes = iota
	eyesBlink
	eyesHappy   // ^ ^
	eyesSqueeze // > <
	eyesSleep
)

type mouth uint8

const (
	mouthSmile mouth = iota
	mouthGrin        // open, with tongue
	mouthTalk        // opens and closes
	mouthO           // surprised or singing
	mouthYawn        // a big tall yawn, opening by talk
	mouthSleep       // tiny
)

// look is one frame of Buddy's body language.
type look struct {
	lift   float64 // height above the ground
	sx, sy float64 // squash and stretch, 1 = normal
	lean   float64 // the face slides this way, and the body half as far
	pupil  float64 // where the eyes look, sideways
	eyes   eyes
	mouth  mouth
	talk   float64 // how open a talking mouth is, 0 to 1
	armL   float64 // arm angles from hanging down, outward is positive
	armR   float64
	footL  float64 // foot lift
	footR  float64
	sprout float64 // sway of the sprout on its head
	sleepy float64 // body color from awake (0) to asleep (1)
}

func (s *Scene) pose(now time.Time) look {
	secs := float64(now.UnixMilli()%1_000_000) / 1000
	t := now.Sub(s.started).Seconds()
	breath := math.Sin(secs * 2.2)
	l := look{
		sx: 1 + 0.012*breath, sy: 1 - 0.022*breath,
		armL: 0.35, armR: 0.35,
		sprout: 0.12 * math.Sin(secs*1.7),
		sleepy: s.sleepiness(now),
	}
	if now.UnixMilli()%3700 < 160 {
		l.eyes = eyesBlink
	}
	if s.grabbed {
		s.dangle(&l, now, secs)
		return l
	}
	if s.asleep {
		if s.yawning(now) {
			yawn(&l, now.Sub(s.sleptAt).Seconds())
			return l
		}
		slow := math.Sin(secs * 1.3)
		l.sx, l.sy = 1+0.02*slow, 1-0.035*slow
		l.eyes, l.mouth = eyesSleep, mouthSleep
		l.sprout = 0.35 + 0.05*slow // droops
		l.armL, l.armR = 0.2, 0.2
		return l
	}

	switch s.anim {
	case rl.Fidget:
		// Look left, look right, then hop back to the middle.
		turn := keyframes(t, [2]float64{0, 0}, [2]float64{0.2, -1}, [2]float64{0.55, -1},
			[2]float64{0.8, 1}, [2]float64{1.15, 1}, [2]float64{1.3, 0})
		l.pupil = 4.5 * turn
		l.lean = 3.5 * turn
		l.sprout -= 0.25 * turn // trails behind the turn
		// Lift the arm on the side it's peeking toward.
		l.armL += 0.7 * max(-turn, 0)
		l.armR += 0.7 * max(turn, 0)
		if t >= 1.25 {
			hop(&l, t-1.25, 0.35, 6)
		}
	case rl.Wave:
		up := envelope(t, 2, 0.25)
		l.armR = 0.35 + up*(2.2+0.45*math.Sin(t*10))
		l.lean = 1.5 * up
		l.eyes, l.mouth = eyesHappy, mouthGrin
	case rl.Bounce:
		hop(&l, t, 0.6, 24)
		if l.lift > 4 {
			l.mouth = mouthGrin
		}
	case rl.Chirp:
		if t < 0.6 {
			hop(&l, t, 0.6, 12)
		}
		if t < 0.9 {
			l.mouth = mouthO
		}
	case rl.Speak:
		typing := time.Duration(t*float64(time.Second)) < popIn+time.Duration(len(s.phrase))*time.Second/typeRate
		if typing {
			l.mouth = mouthTalk
			l.talk = 0.5 + 0.5*math.Sin(t*22)
			l.sy += 0.012 * math.Sin(t*22)
		}
	case rl.Approach:
		l.lean = 3 * s.facing
		l.pupil = 3 * s.facing
		step := math.Mod(t, 0.32) / 0.32
		l.lift = 5 * math.Sin(step*math.Pi)
		if int(t/0.32)%2 == 0 {
			l.footL = 3 * math.Sin(step*math.Pi)
		} else {
			l.footR = 3 * math.Sin(step*math.Pi)
		}
		swing := 0.3 * math.Sin(t/0.32*math.Pi)
		l.armL, l.armR = 0.35+swing, 0.35-swing
		l.sprout -= 0.2 * s.facing
	case rl.Peek:
		peek(&l, t)
	case rl.Emote:
		s.emoteFace(&l, t)
	}

	if s.waking(now) {
		wake(&l, now.Sub(s.wokeAt).Seconds())
	}
	if s.landing(now) {
		dt := now.Sub(s.dropped).Seconds()
		spring := math.Exp(-6*dt) * math.Cos(16*dt)
		l.sy -= 0.18 * spring
		l.sx += 0.14 * spring
		l.sprout += 0.5 * spring
		if dt < 0.3 {
			l.eyes, l.mouth = eyesSqueeze, mouthSleep
		}
	}
	if s.happy(now) {
		ht := now.Sub(s.clicked).Seconds()
		spring := math.Exp(-5*ht) * math.Cos(18*ht)
		l.sy -= 0.16 * spring
		l.sx += 0.12 * spring
		l.eyes, l.mouth = eyesHappy, mouthGrin
		raise := 1.2 * math.Exp(-3*ht)
		l.armL, l.armR = max(l.armL, 0.35+raise), max(l.armR, 0.35+raise)
		l.sprout += 0.4 * spring
	}
	return l
}

// dangle is Buddy held up by the mouse: stretched, arms up, feet kicking,
// swinging behind the direction it's carried in.
func (s *Scene) dangle(l *look, now time.Time, secs float64) {
	in := easeOutBack(now.Sub(s.grabAt).Seconds() / 0.25)
	l.lift = 6 * in
	l.sy, l.sx = 1+0.1*in, 1-0.07*in
	l.lean = s.swing
	l.armL, l.armR = 0.35+2.3*in, 0.35+2.3*in
	kick := 2 * math.Sin(secs*9)
	l.footL, l.footR = -3*in-kick, -3*in+kick
	l.eyes, l.mouth = eyesOpen, mouthO
	l.pupil = min(max(s.dragV*0.006, -3.5), 3.5) // looks where it's going
	l.sprout = 0.07*s.swing + 0.1*math.Sin(secs*5)
}

// yawn plays while falling asleep: a big stretch and yawn, then the sprout
// slowly droops.
func yawn(l *look, t float64) {
	stretch := envelope(t, 1.5, 0.4)
	l.sy += 0.1 * stretch
	l.sx -= 0.06 * stretch
	l.armL, l.armR = 0.35+2.1*stretch, 0.35+2.1*stretch
	l.mouth, l.talk = mouthYawn, envelope(t, 1.4, 0.35)
	l.eyes = eyesSqueeze
	if t > 1.4 {
		l.eyes, l.mouth = eyesSleep, mouthSleep
	}
	l.sprout = 0.35 * smoothstep(t/yawnFor.Seconds())
}

// wake plays after waking up: eyes blink open, a stretch with arms up, and
// the drooping sprout springs back up.
func wake(l *look, t float64) {
	switch {
	case t < 0.35:
		l.eyes = eyesSleep
	case t < 0.5:
		l.eyes = eyesBlink
	}
	if t > 0.35 && t < 0.9 {
		l.mouth = mouthO
	}
	up := envelope(t-0.3, 1, 0.3)
	l.sy += 0.12 * up
	l.sx -= 0.07 * up
	l.armL, l.armR = max(l.armL, 0.35+2.3*up), max(l.armR, 0.35+2.3*up)
	l.sprout += 0.35 * (1 - easeOutBack((t-0.2)/0.5))
}

// keyframes eases between (time, value) keys and holds the last value.
func keyframes(t float64, keys ...[2]float64) float64 {
	for i := 1; i < len(keys); i++ {
		if a, b := keys[i-1], keys[i]; t < b[0] {
			return a[1] + (b[1]-a[1])*smoothstep((t-a[0])/(b[0]-a[0]))
		}
	}
	return keys[len(keys)-1][1]
}

func smoothstep(v float64) float64 {
	v = min(max(v, 0), 1)
	return v * v * (3 - 2*v)
}

// hop animates one jump every period seconds: a wind-up squash, a stretch
// on the way up and down, and a squash on landing.
func hop(l *look, t, period, height float64) {
	const windUp, landing = 0.18, 0.18
	u := math.Mod(t, period) / period
	squash := 0.0
	switch {
	case u < windUp:
		squash = math.Sin(math.Pi * u / windUp)
	case u > 1-landing:
		squash = math.Sin(math.Pi * (u - (1 - landing)) / landing)
	default:
		a := (u - windUp) / (1 - windUp - landing)
		l.lift = height * 4 * a * (1 - a)
		stretch := 0.1 * math.Abs(1-2*a) // fastest, so longest, near the ground
		l.sy += stretch
		l.sx -= stretch * 0.6
		l.sprout += 0.35 * (2*a - 1) // trails behind the motion
		raise := 0.9 * l.lift / height
		l.armL, l.armR = 0.35+raise, 0.35+raise
	}
	l.sy -= 0.14 * squash
	l.sx += 0.1 * squash
}

// envelope rises from 0 to 1 over ramp seconds, holds, and falls back to 0
// at the end of length seconds, easing both ways.
func envelope(t, length, ramp float64) float64 {
	v := min(t/ramp, (length-t)/ramp, 1)
	v = max(v, 0)
	return 0.5 - 0.5*math.Cos(math.Pi*v)
}

// easeOutBack overshoots a little before settling at 1, for pop-ins.
func easeOutBack(v float64) float64 {
	v = min(max(v, 0), 1)
	const c = 1.7
	v--
	return 1 + (c+1)*v*v*v + c*v*v
}

// carry moves the swing toward the drag speed, so a carried Buddy lags
// behind the cursor and settles when it stops. The speed fades on its own,
// since the window only reports it while the mouse moves.
func (s *Scene) carry(now time.Time) {
	dt := 0.0
	if !s.lastDraw.IsZero() {
		dt = min(max(now.Sub(s.lastDraw).Seconds(), 0), 0.1)
	}
	s.lastDraw = now
	target := min(max(-s.dragV*0.01, -7), 7)
	s.swing += (target - s.swing) * min(1, dt*10)
	s.dragV *= math.Exp(-dt * 5)
}

// mix blends two colors, t from 0 (a) to 1 (b).
func mix(a, b color.NRGBA, t float64) color.NRGBA {
	l := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.NRGBA{l(a.R, b.R), l(a.G, b.G), l(a.B, b.B), l(a.A, b.A)}
}

// Render draws the scene. The result is premultiplied RGBA and stays valid
// until the next call.
func (s *Scene) Render(now time.Time) *image.RGBA {
	clear(s.img.Pix)
	s.Animating(now) // let finished animations end before drawing them
	s.carry(now)
	l := s.pose(now)

	cx := bodyX + l.lean*0.5
	rx, h := bodyRX*l.sx, bodyH*l.sy
	base := ground - l.lift
	top := base - h

	// Ground shadow, smaller and fainter the higher Buddy jumps.
	up := min(max(l.lift, 0)/30, 1)
	sh := shadow
	sh.A = uint8(float64(shadow.A) * (1 - 0.5*up))
	if l.lift < 0 { // peeking: the shadow fades as Buddy sinks out of sight
		sh.A = uint8(float64(sh.A) * max(0, 1+l.lift/25))
	}
	s.ellipse(cx, ground+2, 30*l.sx*(1-0.4*up), 4.5, 0, sh)

	s.drawBuddy(l, cx, rx, h, base)
	s.drawEffects(now, cx, rx, top)
	if len(s.lines) > 0 {
		s.drawBubble(now, cx, top)
	}
	return s.img
}

func (s *Scene) drawBuddy(l look, cx, rx, h, base float64) {
	fill, shade := mix(bodyColor, sleepBody, l.sleepy), mix(bodyShade, sleepShade, l.sleepy)
	top := base - h

	// Feet and arms sit behind the body, so only their outer ends show.
	for _, f := range []struct{ x, lift float64 }{{cx - 15, l.footL}, {cx + 15, l.footR}} {
		s.outlined(func(o float64, c color.NRGBA) { s.ellipse(f.x, base-f.lift, 8.5+o, 5.5+o, 0, c) }, shade)
	}
	shoulderY := base - h*0.42
	for _, a := range []struct{ side, angle float64 }{{-1, l.armL}, {1, l.armR}} {
		// A raised arm swings out from the edge of the body and reaches further,
		// so it reads as a hand in the air rather than an ear.
		raised := min(max((a.angle-0.35)/2.2, 0), 1)
		reach := 8 + 5*raised
		ax := cx + a.side*(rx*(0.88+0.1*raised)+math.Sin(a.angle)*reach)
		ay := shoulderY + math.Cos(a.angle)*reach
		s.outlined(func(o float64, c color.NRGBA) { s.ellipse(ax, ay, 5.5+o, 9+1.5*raised+o, -a.side*a.angle, c) }, fill)
	}

	// Sprout, rooted under the top of the head.
	root := vec{cx + l.lean*0.5, top + 3}
	tip := vec{root.x + math.Sin(l.sprout)*15, root.y - math.Cos(l.sprout)*15}
	bend := vec{(root.x+tip.x)/2 - math.Sin(l.sprout)*3, (root.y + tip.y) / 2}
	// Two leaves grow sideways from the tip, tilted up into a V.
	type leaf struct {
		at  vec
		rot float64
	}
	var leaves [2]leaf
	for i, side := range []float64{-1, 1} {
		rot := l.sprout - side*0.5
		if side < 0 {
			rot += math.Pi
		}
		leaves[i] = leaf{vec{tip.x + 5.5*math.Cos(rot), tip.y + 5.5*math.Sin(rot)}, rot}
	}
	s.curve(root, bend, tip, 2.6+2*outline, lineColor)
	for _, lf := range leaves {
		s.ellipse(lf.at.x, lf.at.y, 6.5+outline, 3.4+outline, lf.rot, lineColor)
	}
	s.curve(root, bend, tip, 2.6, stemColor)
	for _, lf := range leaves {
		s.ellipse(lf.at.x, lf.at.y, 6.5, 3.4, lf.rot, leafColor)
	}

	// Body: outline, a shaded base, the lit body on top, and a shine.
	s.fill(cx-rx-outline-1, top-outline-1, cx+rx+outline+1, base+outline+1, lineColor, func(p pather) {
		p.body(cx, base+outline, rx+outline, h+2*outline)
	})
	s.fill(cx-rx-1, top-1, cx+rx+1, base+1, shade, func(p pather) { p.body(cx, base, rx, h) })
	s.fill(cx-rx-1, top-1, cx+rx+1, base+1, fill, func(p pather) { p.body(cx, base-3.5, rx-1.8, h-3.5) })
	s.ellipse(cx-rx*0.48, top+h*0.3, 5.5, 10, 0.55, shine)
	s.ellipse(cx-rx*0.66, top+h*0.55, 2, 2.6, 0.4, shine)

	s.drawFace(l, cx+l.lean*0.5, base-h*0.5)
}

func (s *Scene) drawFace(l look, fx, ey float64) {
	s.ellipse(fx-21, ey+6.5, 5.5, 3.4, 0, cheekColor)
	s.ellipse(fx+21, ey+6.5, 5.5, 3.4, 0, cheekColor)

	for _, side := range []float64{-1, 1} {
		x := fx + side*12.5 + l.pupil
		switch l.eyes {
		case eyesOpen:
			s.ellipse(x, ey, 4.4, 5.8, 0, ink)
			s.ellipse(x-1.3, ey-2.3, 1.8, 1.8, 0, paper)
			s.ellipse(x+1.5, ey+2.2, 0.9, 0.9, 0, paper)
		case eyesBlink:
			s.curve(vec{x - 4.5, ey}, vec{x, ey + 1.5}, vec{x + 4.5, ey}, 2.4, ink)
		case eyesHappy:
			s.curve(vec{x - 5, ey + 1.5}, vec{x, ey - 5}, vec{x + 5, ey + 1.5}, 2.6, ink)
		case eyesSqueeze: // > < : the point faces the nose
			tip, ends := vec{x - side*3, ey}, x+side*4
			s.curve(vec{ends, ey - 3.5}, vec{(ends + tip.x) / 2, ey - 1.75}, tip, 2.4, ink)
			s.curve(tip, vec{(ends + tip.x) / 2, ey + 1.75}, vec{ends, ey + 3.5}, 2.4, ink)
		case eyesSleep:
			s.curve(vec{x - 5, ey - 0.5}, vec{x, ey + 4}, vec{x + 5, ey - 0.5}, 2.4, ink)
		}
	}

	mx, my := fx+l.pupil*0.4, ey+10
	switch l.mouth {
	case mouthSmile:
		s.curve(vec{mx - 4, my - 0.5}, vec{mx, my + 3.5}, vec{mx + 4, my - 0.5}, 2.2, ink)
	case mouthGrin:
		s.fill(mx-6, my-1, mx+6, my+8, ink, func(p pather) {
			p.moveTo(vec{mx - 5.5, my})
			p.lineTo(vec{mx + 5.5, my})
			p.cubeTo(vec{mx + 5.5, my + 5}, vec{mx + 2.5, my + 7.5}, vec{mx, my + 7.5})
			p.cubeTo(vec{mx - 2.5, my + 7.5}, vec{mx - 5.5, my + 5}, vec{mx - 5.5, my})
			p.close()
		})
		s.ellipse(mx, my+5.2, 3, 1.8, 0, tongue)
	case mouthTalk:
		s.ellipse(mx, my+2.5, 3.2, 1.2+2.6*l.talk, 0, ink)
	case mouthO:
		s.ellipse(mx, my+3, 2.8, 3.4, 0, ink)
	case mouthYawn:
		s.ellipse(mx, my+3, 3.5+1.2*l.talk, 1.5+5*l.talk, 0, ink)
		if l.talk > 0.5 {
			s.ellipse(mx, my+3+3.5*l.talk, 2.4, 1.4, 0, tongue)
		}
	case mouthSleep:
		s.ellipse(mx, my+2, 1.8, 1.4, 0, ink)
	}
}

// drawEffects draws music notes, the heart after a click, and sleeping Zs.
// They fade by shrinking, so overlapping strokes never show through.
func (s *Scene) drawEffects(now time.Time, cx, rx, top float64) {
	if s.anim == rl.Emote {
		s.drawEmote(now, cx, rx, top)
	}
	if s.anim == rl.Chirp {
		t := now.Sub(s.started).Seconds()
		for i, delay := range []float64{0, 0.3} {
			p := (t - delay) / 1.2
			if p <= 0 || p >= 1 {
				continue
			}
			size := easeOutBack(p*5) * (1 - p*p)
			s.note(cx+rx+4+float64(i)*12+5*math.Sin(p*6), top-2-p*45, size)
		}
	}
	if s.happy(now) {
		p := now.Sub(s.clicked).Seconds() / happyFor.Seconds()
		size := easeOutBack(p*4) * min(1, (1-p)*2.5)
		s.heart(cx+rx*0.7, top-4-p*24, 9*size)
	}
	if s.asleep && !s.yawning(now) {
		secs := float64(now.UnixMilli()%1_000_000) / 1000
		for i := range 3 {
			p := math.Mod(secs/2.4+float64(i)/3, 1)
			size := (5 + 5*p) * math.Sin(math.Pi*p)
			s.zee(cx+rx*0.75+p*16, top+6-p*34, size)
		}
	}
}

func (s *Scene) note(x, y, size float64) {
	if size < 0.35 { // smaller notes are just specks
		return
	}
	k := size
	head, stemTop := vec{x, y}, vec{x + 3.4*k, y - 15*k}
	flagCtl, flagEnd := vec{x + 9*k, y - 11*k}, vec{x + 8*k, y - 5*k}
	for _, o := range []float64{1.6, 0} {
		c := ink
		if o > 0 {
			c = paper
		}
		s.ellipse(head.x, head.y, 4.2*k+o, 3.2*k+o, -0.4, c)
		s.curve(vec{x + 3.4*k, y - 1}, vec{x + 3.4*k, y - 8*k}, stemTop, 1.8*k+2*o, c)
		s.curve(stemTop, flagCtl, flagEnd, 1.8*k+2*o, c)
	}
}

func (s *Scene) heart(x, y, size float64) {
	if size < 2.5 { // smaller hearts are just specks
		return
	}
	shape := func(r float64, c color.NRGBA) {
		s.fill(x-r, y-r, x+r, y+r, c, func(p pather) {
			p.moveTo(vec{x, y + 0.4*r})
			p.cubeTo(vec{x - 0.6*r, y}, vec{x - 0.45*r, y - 0.45*r}, vec{x, y - 0.15*r})
			p.cubeTo(vec{x + 0.45*r, y - 0.45*r}, vec{x + 0.6*r, y}, vec{x, y + 0.4*r})
			p.close()
		})
	}
	shape(size*1.3+outline*2, lineColor)
	shape(size*1.3, heartColor)
}

func (s *Scene) zee(x, y, size float64) {
	if size < 4.5 { // smaller Zs are just a speck
		return
	}
	pts := []vec{{x, y}, {x + size, y}, {x, y + size}, {x + size, y + size}}
	for _, w := range []float64{2.2 + 2*outline, 2.2} {
		c := paper
		if w > 2.2 {
			c = lineColor
		}
		for i := range len(pts) - 1 {
			a, b := pts[i], pts[i+1]
			s.curve(a, vec{(a.x + b.x) / 2, (a.y + b.y) / 2}, b, w, c)
		}
	}
}

// Speech bubble layout.
const (
	bubbleMaxW = 184.0
	bubblePadX = 11.0
	bubblePadY = 8.0
	lineH      = 17.0
	bubbleR    = 10.0
)

func (s *Scene) drawBubble(now time.Time, cx, top float64) {
	t := now.Sub(s.started)
	grow := easeOutBack(t.Seconds() / popIn.Seconds())
	if grow <= 0.05 {
		return
	}

	w := 0.0
	for _, l := range s.lines {
		w = max(w, s.measure(l))
	}
	w += 2 * bubblePadX
	h := float64(len(s.lines))*lineH + 2*bubblePadY
	x := min(max(cx-w/2, 4), Width-4-w)
	y := max(top-30-h, 4) // clear of the sprout

	// Grow from the tail, which points at Buddy's head.
	tipAt := vec{cx - 25, y + h + 14}
	anchor := tipAt
	g := func(v vec) vec { return vec{anchor.x + (v.x-anchor.x)*grow, anchor.y + (v.y-anchor.y)*grow} }
	box := func(dx, dy, out float64, c color.NRGBA) {
		a, b := g(vec{x - out + dx, y - out + dy}), g(vec{x + w + out + dx, y + h + out + dy})
		s.fill(a.x-1, a.y-1, b.x+1, b.y+1, c, func(p pather) { p.roundRect(a.x, a.y, b.x-a.x, b.y-a.y, (bubbleR+out)*grow) })
	}
	tail := func(out float64, c color.NRGBA) {
		// The tail leans past the sprout toward the top of Buddy's head.
		a, b, tip := g(vec{cx - 32 - out*1.2, y + h - 2}), g(vec{cx - 16 + out*1.2, y + h - 2}), g(vec{tipAt.x, tipAt.y + out*1.4})
		s.fill(min(a.x, tip.x)-1, a.y-1, max(b.x, tip.x)+1, tip.y+1, c, func(p pather) {
			p.moveTo(a)
			p.lineTo(b)
			p.lineTo(tip)
			p.close()
		})
	}
	box(0, 2.5, 0, shadow)
	box(0, 0, outline, lineColor)
	tail(outline, lineColor)
	box(0, 0, 0, paper)
	tail(0, paper)

	if grow < 0.98 {
		return
	}
	// Type the text out, laid out in full from the start so lines don't jump.
	shown := int((t - popIn).Seconds() * typeRate)
	for i, l := range s.lines {
		if shown <= 0 {
			break
		}
		r := []rune(l)
		n := min(len(r), shown)
		s.text(string(r[:n]), x+bubblePadX, y+bubblePadY+float64(i)*lineH+12.5)
		shown -= len(r) + 1 // the space wrap removed
	}
}

// outlined draws a shape twice: grown by the outline width in the outline
// color, then at its size in fill.
func (s *Scene) outlined(shape func(grow float64, c color.NRGBA), fill color.NRGBA) {
	shape(outline, lineColor)
	shape(0, fill)
}

type vec struct{ x, y float64 }

// pather adds paths to the rasterizer in scene units.
type pather struct {
	r      *vector.Rasterizer
	k      float64 // scale
	ox, oy float64 // physical origin of the rasterizer's box
}

func (p pather) pt(v vec) (float32, float32) {
	return float32(v.x*p.k - p.ox), float32(v.y*p.k - p.oy)
}

func (p pather) moveTo(a vec) { p.r.MoveTo(p.pt(a)) }
func (p pather) lineTo(a vec) { p.r.LineTo(p.pt(a)) }
func (p pather) close()       { p.r.ClosePath() }
func (p pather) cubeTo(a, b, c vec) {
	ax, ay := p.pt(a)
	bx, by := p.pt(b)
	cx, cy := p.pt(c)
	p.r.CubeTo(ax, ay, bx, by, cx, cy)
}

// kappa places cubic Bézier handles to approximate a quarter circle.
const kappa = 0.5522847498

func (p pather) ellipse(cx, cy, rx, ry, rot float64) {
	cos, sin := math.Cos(rot), math.Sin(rot)
	at := func(x, y float64) vec { return vec{cx + x*cos - y*sin, cy + x*sin + y*cos} }
	kx, ky := kappa*rx, kappa*ry
	p.moveTo(at(rx, 0))
	p.cubeTo(at(rx, ky), at(kx, ry), at(0, ry))
	p.cubeTo(at(-kx, ry), at(-rx, ky), at(-rx, 0))
	p.cubeTo(at(-rx, -ky), at(-kx, -ry), at(0, -ry))
	p.cubeTo(at(kx, -ry), at(rx, -ky), at(rx, 0))
	p.close()
}

// body is Buddy's gumdrop shape: round on top, flatter where it sits.
func (p pather) body(cx, base, rx, h float64) {
	top := base - h
	p.moveTo(vec{cx, base})
	p.cubeTo(vec{cx + rx*0.78, base}, vec{cx + rx, base - h*0.1}, vec{cx + rx, base - h*0.42})
	p.cubeTo(vec{cx + rx, base - h*0.8}, vec{cx + rx*0.58, top}, vec{cx, top})
	p.cubeTo(vec{cx - rx*0.58, top}, vec{cx - rx, base - h*0.8}, vec{cx - rx, base - h*0.42})
	p.cubeTo(vec{cx - rx, base - h*0.1}, vec{cx - rx*0.78, base}, vec{cx, base})
	p.close()
}

func (p pather) roundRect(x, y, w, h, r float64) {
	r = min(r, w/2, h/2)
	k := kappa * r
	p.moveTo(vec{x + r, y})
	p.lineTo(vec{x + w - r, y})
	p.cubeTo(vec{x + w - r + k, y}, vec{x + w, y + r - k}, vec{x + w, y + r})
	p.lineTo(vec{x + w, y + h - r})
	p.cubeTo(vec{x + w, y + h - r + k}, vec{x + w - r + k, y + h}, vec{x + w - r, y + h})
	p.lineTo(vec{x + r, y + h})
	p.cubeTo(vec{x + r - k, y + h}, vec{x, y + h - r + k}, vec{x, y + h - r})
	p.lineTo(vec{x, y + r})
	p.cubeTo(vec{x, y + r - k}, vec{x + r - k, y}, vec{x + r, y})
	p.close()
}

// fill rasterizes a shape inside the box (x0, y0)-(x1, y1), in scene units.
// Working in the shape's own small box keeps drawing cheap.
func (s *Scene) fill(x0, y0, x1, y1 float64, c color.NRGBA, build func(pather)) {
	k := s.scale
	b := image.Rect(int(math.Floor(x0*k)), int(math.Floor(y0*k)), int(math.Ceil(x1*k)), int(math.Ceil(y1*k))).Intersect(s.img.Bounds())
	if b.Empty() {
		return
	}
	s.rast.Reset(b.Dx(), b.Dy())
	build(pather{r: &s.rast, k: k, ox: float64(b.Min.X), oy: float64(b.Min.Y)})
	s.rast.Draw(s.img, b, image.NewUniform(c), image.Point{})
}

func (s *Scene) ellipse(cx, cy, rx, ry, rot float64, c color.NRGBA) {
	r := max(rx, ry)
	s.fill(cx-r-1, cy-r-1, cx+r+1, cy+r+1, c, func(p pather) { p.ellipse(cx, cy, rx, ry, rot) })
}

// curve strokes a quadratic Bézier from a to b bent toward ctl, with round ends.
func (s *Scene) curve(a, ctl, b vec, width float64, c color.NRGBA) {
	const n = 12
	var pts [n + 1]vec
	var norms [n + 1]vec
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for i := range pts {
		t := float64(i) / n
		u := 1 - t
		pts[i] = vec{u*u*a.x + 2*u*t*ctl.x + t*t*b.x, u*u*a.y + 2*u*t*ctl.y + t*t*b.y}
		d := vec{2*u*(ctl.x-a.x) + 2*t*(b.x-ctl.x), 2*u*(ctl.y-a.y) + 2*t*(b.y-ctl.y)}
		l := math.Hypot(d.x, d.y)
		if l == 0 {
			d, l = vec{b.x - a.x, b.y - a.y}, math.Hypot(b.x-a.x, b.y-a.y)
		}
		if l > 0 {
			norms[i] = vec{-d.y / l * width / 2, d.x / l * width / 2}
		}
		x0, y0 = min(x0, pts[i].x), min(y0, pts[i].y)
		x1, y1 = max(x1, pts[i].x), max(y1, pts[i].y)
	}
	pad := width/2 + 1
	s.fill(x0-pad, y0-pad, x1+pad, y1+pad, c, func(p pather) {
		p.moveTo(vec{pts[0].x + norms[0].x, pts[0].y + norms[0].y})
		for i := 1; i <= n; i++ {
			p.lineTo(vec{pts[i].x + norms[i].x, pts[i].y + norms[i].y})
		}
		for i := n; i >= 0; i-- {
			p.lineTo(vec{pts[i].x - norms[i].x, pts[i].y - norms[i].y})
		}
		p.close()
	})
	if c.A == 0xff { // caps would darken a translucent stroke where they overlap
		s.ellipse(a.x, a.y, width/2, width/2, 0, c)
		s.ellipse(b.x, b.y, width/2, width/2, 0, c)
	}
}

func (s *Scene) text(str string, x, y float64) {
	d := font.Drawer{
		Dst:  s.img,
		Src:  image.NewUniform(ink),
		Face: s.face,
		Dot:  fixed.P(int(math.Round(x*s.scale)), int(math.Round(y*s.scale))),
	}
	d.DrawString(str)
}

// measure returns the width of str in scene units.
func (s *Scene) measure(str string) float64 {
	return float64(font.MeasureString(s.face, str).Ceil()) / s.scale
}

// wrap splits str into lines no wider than width scene units.
func (s *Scene) wrap(str string, width float64) []string {
	var lines []string
	line := ""
	for word := range strings.FieldsSeq(str) {
		next := word
		if line != "" {
			next = line + " " + word
		}
		if s.measure(next) > width && line != "" {
			lines = append(lines, line)
			next = word
		}
		line = next
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}
