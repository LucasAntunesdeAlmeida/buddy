package rl

import (
	"math"
	"sync"
)

// Learner is a reinforcement learning brain. Buddy has two: the Q-table
// (Agent), which learns each situation on its own, and the neural network
// (Net), which carries what it learns over to similar situations.
type Learner interface {
	// Act picks an action, exploring now and then. explored reports
	// whether the action was a random experiment.
	Act(o Observation) (a Action, explored bool)
	// Best returns the action the learner currently thinks is best.
	Best(o Observation) Action
	// Scores returns the learned value of every action.
	Scores(o Observation) [NumActions]float64
	// Learn updates from one step: in o, doing a earned r and led to next.
	Learn(o Observation, a Action, r float64, next Observation)
	// Epsilon is the current chance of a random experiment.
	Epsilon() float64
	// SimScore is the reward per simulated day measured after pre-training,
	// used to pick the better brain. Zero if never measured.
	SimScore() float64
	SetSimScore(float64)
	// Kind names the brain: "table" or "network".
	Kind() string
	Save(path string) error
}

// Agent is a Learner.
var _ Learner = (*Agent)(nil)

// Act implements Learner.
func (a *Agent) Act(o Observation) (Action, bool) { return a.Choose(Encode(o), o.Focused()) }

// Best implements Learner.
func (a *Agent) Best(o Observation) Action { return a.Greedy(Encode(o)) }

// Scores implements Learner.
func (a *Agent) Scores(o Observation) [NumActions]float64 { return a.Values(Encode(o)) }

// Learn implements Learner.
func (a *Agent) Learn(o Observation, act Action, r float64, next Observation) {
	a.Update(Encode(o), act, r, Encode(next))
}

// SimScore implements Learner.
func (a *Agent) SimScore() float64 { return a.Sim }

// SetSimScore implements Learner.
func (a *Agent) SetSimScore(v float64) { a.Sim = v }

// Kind implements Learner.
func (a *Agent) Kind() string { return "table" }

// epsilon is the exploration schedule shared by both brains.
func epsilon(steps int) float64 {
	return epsilonMin + (epsilonMax-epsilonMin)*math.Exp(-float64(steps)/epsilonLag)
}

// Locked makes a Learner safe to share, so a report can read it while the
// brain is learning.
type Locked struct {
	mu sync.Mutex
	l  Learner
}

// NewLocked wraps l.
func NewLocked(l Learner) *Locked { return &Locked{l: l} }

var _ Learner = (*Locked)(nil)

// Act implements Learner.
func (k *Locked) Act(o Observation) (Action, bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.Act(o)
}

// Best implements Learner.
func (k *Locked) Best(o Observation) Action {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.Best(o)
}

// Scores implements Learner.
func (k *Locked) Scores(o Observation) [NumActions]float64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.Scores(o)
}

// Learn implements Learner.
func (k *Locked) Learn(o Observation, a Action, r float64, next Observation) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.l.Learn(o, a, r, next)
}

// Epsilon implements Learner.
func (k *Locked) Epsilon() float64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.Epsilon()
}

// SimScore implements Learner.
func (k *Locked) SimScore() float64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.SimScore()
}

// SetSimScore implements Learner.
func (k *Locked) SetSimScore(v float64) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.l.SetSimScore(v)
}

// Kind implements Learner.
func (k *Locked) Kind() string { return k.l.Kind() }

// Save implements Learner.
func (k *Locked) Save(path string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.l.Save(path)
}

// Peek runs f with the wrapped learner while holding the lock, for reading
// internals, like the network's weights, that Learner doesn't expose.
func (k *Locked) Peek(f func(Learner)) {
	k.mu.Lock()
	defer k.mu.Unlock()
	f(k.l)
}
