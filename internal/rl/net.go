package rl

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// NumInputs is the size of the network's input: 14 smooth numbers plus the
// table's 20 buckets.
const NumInputs = 14 + idleBuckets + activityBuckets + inAppBuckets + hourBuckets + annoyBuckets + sinceBuckets

// Inputs turns an observation into numbers between 0 and 1, in two views.
// The smooth numbers keep detail, so the network can tell 9 minutes from 11
// and learn that they are much alike. The table's buckets, one input each,
// let it learn sharp rules as easily as the table does, like "after an hour
// in the same app things change". With both, it can do anything the table
// can and also carry lessons over between similar moments.
func Inputs(o Observation) [NumInputs]float64 {
	var f [NumInputs]float64
	c := o.Category
	if int(c) >= 6 {
		c = 0
	}
	f[c] = 1 // one-hot category, 6 inputs
	f[6] = logScale(o.Idle.Minutes(), 10)
	f[7] = min(max(o.Activity, 0), 1)
	f[8] = logScale(o.InApp.Minutes(), 240)
	hour := 2 * math.Pi * float64(o.Hour) / 24 // a circle, so 23:00 is next to 00:00
	f[9], f[10] = 0.5+0.5*math.Sin(hour), 0.5+0.5*math.Cos(hour)
	f[11] = min(max(o.Annoyance/3, 0), 1)
	f[12] = logScale(o.SinceAction.Minutes(), 120)
	if o.Focused() {
		f[13] = 1
	}
	b := o.Features()
	i := 14
	for _, v := range []struct{ bucket, n int }{
		{b.Idle, idleBuckets}, {b.Activity, activityBuckets}, {b.InApp, inAppBuckets},
		{b.Hour, hourBuckets}, {b.Annoyance, annoyBuckets}, {b.Since, sinceBuckets},
	} {
		f[i+v.bucket] = 1
		i += v.n
	}
	return f
}

// logScale maps 0..top onto 0..1 with more detail at the low end, where
// differences matter more (1 minute vs 5, not 101 vs 105).
func logScale(v, top float64) float64 {
	return min(math.Log1p(max(v, 0))/math.Log1p(top), 1)
}

// Network shape and training settings.
const (
	DefaultHidden = 128   // neurons in each hidden layer
	replaySize    = 10000 // experiences kept in each memory
	batchSize     = 32
	minReplay     = 200  // don't train on fewer experiences than this
	syncTarget    = 1000 // updates between target network refreshes
	learningRate  = 3e-4
	adamBeta1     = 0.9
	adamBeta2     = 0.999
	huberDelta    = 1.0
)

// Layer is a fully connected layer with Adam optimizer state.
type Layer struct {
	In, Out int
	W, B    []float64 // weights (Out×In, row-major) and biases
	MW, VW  []float64 // Adam moments for W
	MB, VB  []float64 // Adam moments for B
}

func newLayer(in, out int, rng *rand.Rand, gain float64) Layer {
	l := Layer{In: in, Out: out,
		W: make([]float64, in*out), B: make([]float64, out),
		MW: make([]float64, in*out), VW: make([]float64, in*out),
		MB: make([]float64, out), VB: make([]float64, out),
	}
	std := gain * math.Sqrt(2/float64(in)) // He initialization for ReLU
	for i := range l.W {
		l.W[i] = rng.NormFloat64() * std
	}
	return l
}

// MLP is a small multi-layer perceptron with ReLU between layers.
type MLP struct{ Layers []Layer }

// forward runs x through the network, returning every layer's output
// (after ReLU, except the last) for backpropagation.
func (m *MLP) forward(x []float64) [][]float64 {
	acts := [][]float64{x}
	for li := range m.Layers {
		l := &m.Layers[li]
		out := make([]float64, l.Out)
		for o := range l.Out {
			sum := l.B[o]
			row := l.W[o*l.In : (o+1)*l.In]
			for i, v := range x {
				sum += row[i] * v
			}
			if li < len(m.Layers)-1 {
				sum = max(sum, 0)
			}
			out[o] = sum
		}
		acts = append(acts, out)
		x = out
	}
	return acts
}

func (m *MLP) predict(x []float64) []float64 {
	acts := m.forward(x)
	return acts[len(acts)-1]
}

func (m *MLP) clone() MLP {
	c := MLP{Layers: make([]Layer, len(m.Layers))}
	for i, l := range m.Layers {
		c.Layers[i] = Layer{In: l.In, Out: l.Out, W: append([]float64(nil), l.W...), B: append([]float64(nil), l.B...)}
	}
	return c
}

// Experience is one remembered step.
type Experience struct {
	X, Next [NumInputs]float32
	Action  Action
	Reward  float32
}

// Net is a Double DQN: a neural network that predicts the value of each
// action, trained on remembered experiences against a slowly updated copy of
// itself, which keeps learning stable.
type Net struct {
	Online MLP
	Target MLP
	// Steps where Buddy stayed quiet far outnumber its attempts, so they are
	// remembered apart and every batch takes half from each. Otherwise
	// attempts are too rare in training, their values stay noisy, and picking
	// the best of several noisy values makes Buddy act far too often.
	QuietMem Memory
	ActMem   Memory
	Steps    int
	Updates  int
	AdamT    int
	Sim      float64

	rng        *rand.Rand
	trainEvery int
	batches    int
	margin     float64 // see SetMargin
	grads      []Layer // reused gradient buffers
}

// NewNet returns an untrained network with DefaultHidden neurons per layer.
func NewNet(seed uint64) *Net { return NewNetSized(seed, DefaultHidden) }

// NewNetSized returns an untrained network with hidden neurons in each of
// its two hidden layers. Bigger can tell finer differences apart, but
// learns slower and costs more to run.
func NewNetSized(seed uint64, hidden int) *Net {
	n := &Net{rng: rand.New(rand.NewPCG(seed, seed^0x2545f4914f6cdd1d)), trainEvery: 1, batches: 1, margin: actMargin}
	n.Online = MLP{Layers: []Layer{
		newLayer(NumInputs, hidden, n.rng, 1),
		newLayer(hidden, hidden, n.rng, 1),
		newLayer(hidden, int(NumActions), n.rng, 0), // zero outputs: start from the biases below
	}}
	// Start out preferring quiet, like the table, by charging attention
	// actions their cost up front.
	out := &n.Online.Layers[2]
	for a := range NumActions {
		if a.Attention() {
			out.B[a] = -costAttention
		}
	}
	n.Target = n.Online.clone()
	return n
}

var _ Learner = (*Net)(nil)

// The network is a dueling network: its first output is how good the
// moment is if Buddy stays quiet, and every other output is how much better
// or worse than that one action would be. Buddy's choices hang on those
// small differences, so the network learns them directly instead of as the
// gap between two large numbers, where small errors would flip decisions.

// values turns the network's raw outputs into the value of each action.
func values(out []float64) [NumActions]float64 {
	var q [NumActions]float64
	q[Quiet] = out[Quiet]
	for a := Quiet + 1; a < NumActions; a++ {
		q[a] = out[Quiet] + out[a]
	}
	return q
}

// rawDelta turns a loss slope on each action's value into slopes on the raw
// outputs: the quiet output feeds every action, and each other output only
// its own.
func rawDelta(dq [NumActions]float64) []float64 {
	d := make([]float64, NumActions)
	for a := range NumActions {
		d[Quiet] += dq[a]
		if a != Quiet {
			d[a] += dq[a]
		}
	}
	return d
}

// Scores implements Learner.
func (n *Net) Scores(o Observation) [NumActions]float64 {
	f := Inputs(o)
	return values(n.Online.predict(f[:]))
}

// actMargin is how much better than staying quiet an attempt must look
// before the network makes it. The network's values are estimates with
// some error, and taking the best of several estimates favors whichever
// happens to be overrated; without a margin that makes Buddy pushy.
const actMargin = 0.1

// Best implements Learner. Ties go to the calmer action.
func (n *Net) Best(o Observation) Action {
	q := n.Scores(o)
	best := Quiet
	for a := Quiet + 1; a < NumActions; a++ {
		bar := q[best]
		if a.Attention() && !best.Attention() {
			bar += n.margin
		}
		if q[a] > bar {
			best = a
		}
	}
	return best
}

// Act implements Learner.
func (n *Net) Act(o Observation) (Action, bool) {
	if n.rng.Float64() < n.Epsilon() {
		if o.Focused() {
			return calmActions[n.rng.IntN(len(calmActions))], true
		}
		return Action(n.rng.IntN(int(NumActions))), true
	}
	return n.Best(o), false
}

// Epsilon implements Learner.
func (n *Net) Epsilon() float64 { return epsilon(n.Steps) }

// SimScore implements Learner.
func (n *Net) SimScore() float64 { return n.Sim }

// SetSimScore implements Learner.
func (n *Net) SetSimScore(v float64) { n.Sim = v }

// Kind implements Learner.
func (n *Net) Kind() string { return "network" }

// Learn implements Learner with plain deep Q-learning: it remembers the step
// and trains on a random batch of remembered steps. Buddy itself learns
// through Hybrid instead, which is steadier; this is kept for experiments.
func (n *Net) Learn(o Observation, a Action, r float64, next Observation) {
	x, nx := Inputs(o), Inputs(next)
	e := Experience{Action: a, Reward: float32(r)}
	for i := range NumInputs {
		e.X[i], e.Next[i] = float32(x[i]), float32(nx[i])
	}
	if a == Quiet {
		n.QuietMem.add(e)
	} else {
		n.ActMem.add(e)
	}
	n.Steps++
	if len(n.QuietMem.Items)+len(n.ActMem.Items) < minReplay || n.Steps%n.trainEvery != 0 {
		return
	}
	for range n.batches {
		n.train()
	}
}

// train does one Adam step on a random minibatch with Huber loss.
func (n *Net) train() {
	n.ensureGrads()
	n.clearGrads()

	var x, nx [NumInputs]float64
	for i := range batchSize {
		mem := &n.QuietMem
		if len(mem.Items) == 0 || (i%2 == 1 && len(n.ActMem.Items) > 0) {
			mem = &n.ActMem
		}
		e := &mem.Items[n.rng.IntN(len(mem.Items))]
		for i := range NumInputs {
			x[i], nx[i] = float64(e.X[i]), float64(e.Next[i])
		}
		// Double DQN: the online network picks the next action, the target
		// network says what it's worth.
		nextQ := values(n.Online.predict(nx[:]))
		best := 0
		for a := range nextQ {
			if nextQ[a] > nextQ[best] {
				best = a
			}
		}
		target := float64(e.Reward) + gamma*values(n.Target.predict(nx[:]))[best]

		acts := n.Online.forward(x[:])
		q := values(acts[len(acts)-1])[e.Action]
		diff := q - target
		grad := min(max(diff, -huberDelta), huberDelta) / batchSize // Huber loss slope
		var dq [NumActions]float64
		dq[e.Action] = grad
		n.backwardAll(acts, rawDelta(dq))
	}
	n.adam()
	n.Updates++
	if n.Updates%syncTarget == 0 {
		n.Target = n.Online.clone()
	}
}

// backward adds the gradient of one sample's loss, which only touches the
// output for the action taken.
func (n *Net) backward(acts [][]float64, action int, grad float64) {
	delta := make([]float64, n.Online.Layers[len(n.Online.Layers)-1].Out)
	delta[action] = grad
	n.backwardAll(acts, delta)
}

// backwardAll adds the gradient for a loss slope on every output at once.
func (n *Net) backwardAll(acts [][]float64, delta []float64) {
	layers := n.Online.Layers
	for li := len(layers) - 1; li >= 0; li-- {
		l, g := &layers[li], &n.grads[li]
		in := acts[li]
		var prev []float64
		if li > 0 {
			prev = make([]float64, l.In)
		}
		for o, d := range delta {
			if d == 0 {
				continue
			}
			g.B[o] += d
			row := l.W[o*l.In : (o+1)*l.In]
			grow := g.W[o*l.In : (o+1)*l.In]
			for i, v := range in {
				grow[i] += d * v
				if prev != nil {
					prev[i] += d * row[i]
				}
			}
		}
		if li > 0 {
			for i := range prev {
				if in[i] <= 0 { // ReLU passes no gradient where it was off
					prev[i] = 0
				}
			}
		}
		delta = prev
	}
}

func (n *Net) adam() {
	n.AdamT++
	c1 := 1 - math.Pow(adamBeta1, float64(n.AdamT))
	c2 := 1 - math.Pow(adamBeta2, float64(n.AdamT))
	step := func(p, g, m, v []float64) {
		for i := range p {
			m[i] = adamBeta1*m[i] + (1-adamBeta1)*g[i]
			v[i] = adamBeta2*v[i] + (1-adamBeta2)*g[i]*g[i]
			p[i] -= learningRate * (m[i] / c1) / (math.Sqrt(v[i]/c2) + 1e-8)
		}
	}
	for li := range n.Online.Layers {
		l, g := &n.Online.Layers[li], &n.grads[li]
		step(l.W, g.W, l.MW, l.VW)
		step(l.B, g.B, l.MB, l.VB)
	}
}

// Save writes the network and its memories atomically.
func (n *Net) Save(path string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".net-*.gob")
	if err != nil {
		return fmt.Errorf("save network: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := gob.NewEncoder(tmp).Encode(n); err != nil {
		tmp.Close()
		return fmt.Errorf("save network: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save network: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save network: %w", err)
	}
	return nil
}

// LoadNet reads a network from path, or returns nil and fs.ErrNotExist if
// there is none.
func LoadNet(path string, seed uint64) (*Net, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("read network: %w", err)
	}
	defer f.Close()
	n := NewNet(seed)
	loaded := &Net{}
	if err := gob.NewDecoder(f).Decode(loaded); err != nil {
		return nil, fmt.Errorf("parse network %s: %w", path, err)
	}
	if l := loaded.Online.Layers; len(l) != 3 || l[0].In != NumInputs || l[2].Out != int(NumActions) {
		return nil, fmt.Errorf("network %s: %w", path, ErrOldBrain)
	}
	loaded.rng, loaded.trainEvery, loaded.batches, loaded.margin = n.rng, n.trainEvery, n.batches, n.margin
	return loaded, nil
}

// Memory is a ring of the most recent experiences.
type Memory struct {
	Items []Experience
	Next  int // where the next experience goes once Items is full
}

func (m *Memory) add(e Experience) {
	if len(m.Items) < replaySize {
		m.Items = append(m.Items, e)
		return
	}
	m.Items[m.Next] = e
	m.Next = (m.Next + 1) % replaySize
}

// Activations returns every layer's values for an observation: the inputs,
// both hidden layers (after ReLU) and the action values. For explaining
// decisions, not for training.
func (n *Net) Activations(o Observation) [][]float64 {
	x := Inputs(o)
	acts := n.Online.forward(x[:])
	q := values(acts[len(acts)-1])
	acts[len(acts)-1] = q[:]
	return acts
}

// Margin is how much better than staying quiet an attempt must look before
// the network makes it.
func (n *Net) Margin() float64 { return n.margin }

// SetMargin sets how much better than staying quiet an attempt must look
// before the network makes it.
func (n *Net) SetMargin(m float64) { n.margin = m }

// ErrOldBrain means a saved brain doesn't fit this version of Buddy, for
// example because Buddy has learned new actions since.
var ErrOldBrain = errors.New("made by an older version of Buddy; run buddysim again")

func (n *Net) ensureGrads() {
	if n.grads == nil {
		for _, l := range n.Online.Layers {
			n.grads = append(n.grads, Layer{W: make([]float64, len(l.W)), B: make([]float64, len(l.B))})
		}
	}
}

func (n *Net) clearGrads() {
	for i := range n.grads {
		clear(n.grads[i].W)
		clear(n.grads[i].B)
	}
}
