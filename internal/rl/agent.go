package rl

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// Learning parameters.
const (
	// The learning rate for a state and action is 1/visits, so early
	// experiences count fully and noisy rewards average out, but it never
	// drops below alphaMin, so Buddy keeps adapting when your habits change.
	alphaMin   = 0.05
	gamma      = 0.9  // how much Buddy cares about what happens next
	epsilonMin = 0.02 // exploration never stops completely
	epsilonMax = 0.2
	epsilonLag = 20000.0 // steps for exploration to fall by a factor of e
)

// Row holds the learned value of each action in one state and how often
// each was tried.
type Row struct {
	Value  [NumActions]float64 `json:"v"`
	Visits [NumActions]uint32  `json:"n"`
}

// Agent is a Q-learning agent with a table of action values per state.
// Unseen states have all values at zero, so the cost of attention actions
// makes Buddy quiet by default until it learns otherwise.
type Agent struct {
	Q     map[State]*Row `json:"q"`
	Steps int            `json:"steps"`

	rng *rand.Rand
}

// NewAgent returns an agent that knows nothing yet.
func NewAgent(seed uint64) *Agent {
	return &Agent{Q: map[State]*Row{}, rng: rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))}
}

func (a *Agent) row(s State) *Row {
	r, ok := a.Q[s]
	if !ok {
		r = new(Row)
		a.Q[s] = r
	}
	return r
}

// Values returns the action values for a state.
func (a *Agent) Values(s State) [NumActions]float64 {
	if r, ok := a.Q[s]; ok {
		return r.Value
	}
	return [NumActions]float64{}
}

// Epsilon is the current chance of trying a random action.
func (a *Agent) Epsilon() float64 {
	return epsilonMin + (epsilonMax-epsilonMin)*math.Exp(-float64(a.Steps)/epsilonLag)
}

// calmActions are the only ones Buddy experiments with while the user is focused.
var calmActions = []Action{Quiet, Fidget}

// Choose picks an action with epsilon-greedy exploration. explored reports
// whether the action was a random experiment.
func (a *Agent) Choose(s State, focused bool) (act Action, explored bool) {
	if a.rng.Float64() < a.Epsilon() {
		if focused {
			return calmActions[a.rng.IntN(len(calmActions))], true
		}
		return Action(a.rng.IntN(int(NumActions))), true
	}
	return a.Greedy(s), false
}

// Greedy returns the best known action. Ties go to the lower action, so
// Quiet wins when nothing is known.
func (a *Agent) Greedy(s State) Action {
	v := a.Values(s)
	best := Quiet
	for act := Quiet + 1; act < NumActions; act++ {
		if v[act] > v[best] {
			best = act
		}
	}
	return best
}

// Update applies one Q-learning step for the transition s -a-> next with reward r.
func (a *Agent) Update(s State, act Action, r float64, next State) {
	nv := a.Values(next)
	best := nv[0]
	for _, v := range nv[1:] {
		best = max(best, v)
	}
	row := a.row(s)
	row.Visits[act]++
	alpha := max(alphaMin, 1/float64(row.Visits[act]))
	row.Value[act] += alpha * (r + gamma*best - row.Value[act])
	a.Steps++
}

// Load reads an agent from path, or returns a fresh one if the file does not exist.
func Load(path string, seed uint64) (*Agent, error) {
	a := NewAgent(seed)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read q-table: %w", err)
	}
	if err := json.Unmarshal(b, a); err != nil {
		return nil, fmt.Errorf("parse q-table %s: %w", path, err)
	}
	if a.Q == nil {
		a.Q = map[State]*Row{}
	}
	for s, row := range a.Q {
		if int(s) >= NumStates || row == nil {
			return nil, fmt.Errorf("q-table %s: state %d out of range, it was made by a different version of Buddy", path, s)
		}
	}
	return a, nil
}

// Save writes the agent to path atomically, so a crash never leaves half a file.
func (a *Agent) Save(path string) error {
	b, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("encode q-table: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qtable-*.json")
	if err != nil {
		return fmt.Errorf("save q-table: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("save q-table: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save q-table: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save q-table: %w", err)
	}
	return nil
}
