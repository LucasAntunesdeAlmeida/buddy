package rl

import (
	"math/rand/v2"
	"time"
)

// Distill teaches the network what a trained table knows: over epochs
// passes, the network learns to give the table's scores at each of the
// given moments. The table's scores are steady averages of many outcomes,
// while a network learning on its own sees one noisy outcome at a time, so
// starting from the table's knowledge gives the network a far better first
// guess. It keeps learning by itself afterwards, and carries lessons over to
// situations the table never saw.
//
// moments should be real moments, as Buddy meets them: each table situation
// covers a range (idle "0 to 30 seconds"), but real moments cluster at
// particular spots in it (idle is usually exactly 0), and the network has to
// be right where moments actually happen. With no moments, it uses one
// random moment per situation the table knows, which is only good for tests.
func (n *Net) Distill(t *Agent, moments []Observation, epochs int, rng *rand.Rand) {
	type sample struct {
		x      [NumInputs]float64
		target [NumActions]float64
		weight [NumActions]float64
	}
	if len(moments) == 0 {
		for s := range t.Q {
			moments = append(moments, Sample(s, rng))
		}
	}
	samples := make([]sample, 0, len(moments))
	for _, o := range moments {
		sm := sample{x: Inputs(o)}
		row := t.Q[Encode(o)]
		for a := range NumActions {
			// Trust the table's score as far as it has evidence: a few
			// tries make it solid, none at all means "no better than
			// doing nothing", which is what the table itself would assume.
			sm.weight[a] = 0.1
			if row != nil {
				sm.target[a] = row.Value[a]
				sm.weight[a] = min(1, 0.1+float64(row.Visits[a])/3)
			}
		}
		samples = append(samples, sm)
	}
	if len(samples) == 0 {
		return
	}
	n.ensureGrads()
	for range epochs {
		rng.Shuffle(len(samples), func(i, j int) { samples[i], samples[j] = samples[j], samples[i] })
		for start := 0; start < len(samples); start += batchSize {
			n.clearGrads()
			batch := samples[start:min(start+batchSize, len(samples))]
			for _, sm := range batch {
				acts := n.Online.forward(sm.x[:])
				q := values(acts[len(acts)-1])
				var dq [NumActions]float64
				for a := range NumActions {
					dq[a] = sm.weight[a] * (q[a] - sm.target[a]) / float64(len(batch))
				}
				n.backwardAll(acts, rawDelta(dq))
			}
			n.adam()
		}
	}
	n.Target = n.Online.clone()
}

// Sample returns a random moment that falls in state s.
func Sample(s State, rng *rand.Rand) Observation {
	f := s.Features()
	between := func(lo, hi time.Duration) time.Duration {
		return lo + time.Duration(rng.Float64()*float64(hi-lo))
	}
	pick := func(edges []float64, b int) float64 {
		return edges[b] + rng.Float64()*(edges[b+1]-edges[b])
	}
	idle := []time.Duration{0, 30 * time.Second, 2 * time.Minute, 5 * time.Minute, 30 * time.Minute}
	inApp := []time.Duration{0, 10 * time.Minute, time.Hour, 4 * time.Hour}
	since := []time.Duration{0, 2 * time.Minute, 15 * time.Minute, 2 * time.Hour}
	var o Observation
	o.Category = f.Category
	o.Idle = between(idle[f.Idle], idle[f.Idle+1])
	o.Activity = pick([]float64{0, 0.1, 0.5, 1}, f.Activity)
	o.InApp = between(inApp[f.InApp], inApp[f.InApp+1])
	o.Hour = f.Hour*6 + rng.IntN(6)
	o.Annoyance = pick([]float64{0, 0.5, 1.5, 3}, f.Annoyance)
	o.SinceAction = between(since[f.Since], since[f.Since+1])
	o.Back = -1
	return o
}
