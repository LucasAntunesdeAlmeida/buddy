package rl

import (
	"errors"
	"math/rand/v2"
)

// Hybrid is Buddy's brain as he runs on your desktop: the neural network
// decides, and the table keeps the diary. Every reaction is tallied in the
// table, which averages outcomes exactly, and every so often the network
// studies the table on the real moments Buddy met with you. That way the
// network learns from you as reliably as the table does, and still carries
// lessons over to moments that are only similar to ones it has seen.
//
// Learning straight into the network, one noisy reaction at a time, was
// tried first; it drifted and got worse with practice.
type Hybrid struct {
	Net       *Net
	Table     *Agent
	TablePath string // where Save keeps the table

	moments []Observation // recent real moments, to study on
	next    int
	steps   int // since the last study
	rng     *rand.Rand
}

// Study settings.
const (
	studyEvery   = 200   // steps between studies, about two hours of use
	studyMoments = 20000 // recent moments kept to study on
	studyPasses  = 2
)

// NewHybrid joins a network and the table it learned from.
func NewHybrid(net *Net, table *Agent, tablePath string, seed uint64) *Hybrid {
	return &Hybrid{Net: net, Table: table, TablePath: tablePath, rng: rand.New(rand.NewPCG(seed, 31))}
}

var _ Learner = (*Hybrid)(nil)

// Act implements Learner: the network decides.
func (h *Hybrid) Act(o Observation) (Action, bool) { return h.Net.Act(o) }

// Best implements Learner.
func (h *Hybrid) Best(o Observation) Action { return h.Net.Best(o) }

// Scores implements Learner.
func (h *Hybrid) Scores(o Observation) [NumActions]float64 { return h.Net.Scores(o) }

// Learn implements Learner: the table tallies the step, and the moment is
// kept for the network's next study.
func (h *Hybrid) Learn(o Observation, a Action, r float64, next Observation) {
	h.Table.Learn(o, a, r, next)
	if len(h.moments) < studyMoments {
		h.moments = append(h.moments, o)
	} else {
		h.moments[h.next] = o
		h.next = (h.next + 1) % studyMoments
	}
	if h.steps++; h.steps >= studyEvery {
		h.Study()
	}
}

// Study has the network learn the table's scores on the recent moments.
func (h *Hybrid) Study() {
	h.steps = 0
	h.Net.Distill(h.Table, h.moments, studyPasses, h.rng)
}

// Epsilon implements Learner.
func (h *Hybrid) Epsilon() float64 { return h.Net.Epsilon() }

// SimScore implements Learner.
func (h *Hybrid) SimScore() float64 { return h.Net.SimScore() }

// SetSimScore implements Learner.
func (h *Hybrid) SetSimScore(v float64) { h.Net.SetSimScore(v) }

// Kind implements Learner.
func (h *Hybrid) Kind() string { return "network" }

// Save implements Learner: the network goes to path, the table to TablePath.
func (h *Hybrid) Save(path string) error {
	return errors.Join(h.Net.Save(path), h.Table.Save(h.TablePath))
}
