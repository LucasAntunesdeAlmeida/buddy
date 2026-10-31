// Package rl is Buddy's brain: a tabular Q-learning agent over a small,
// bucketed view of what the user is doing.
package rl

import (
	"fmt"
	"time"

	"buddy/internal/activity"
)

// Observation is everything the agent looks at before it acts.
type Observation struct {
	activity.Snapshot
	// Hour is the local hour of day, 0 to 23.
	Hour int
	// Annoyance is Buddy's estimate of how fed up the user is.
	Annoyance float64
	// SinceAction is the time since Buddy last tried to get attention.
	SinceAction time.Duration
}

// Focused reports whether the user is busy enough that interrupting is rude.
func (o Observation) Focused() bool {
	return o.Activity >= 0.5 && o.Idle < 30*time.Second
}

// State is an Observation reduced to one table index.
type State uint16

// Bucket counts for each feature. Their product is NumStates.
const (
	idleBuckets     = 4 // <30s, <2m, <5m, away
	activityBuckets = 3 // low, medium, high
	inAppBuckets    = 3 // <10m, <60m, longer
	hourBuckets     = 4 // night, morning, afternoon, evening
	annoyBuckets    = 3 // calm, irritated, fed up
	sinceBuckets    = 3 // <2m, <15m, longer

	NumStates = int(activity.NumCategories) * idleBuckets * activityBuckets *
		inAppBuckets * hourBuckets * annoyBuckets * sinceBuckets
)

// Features is a State decoded into its buckets.
type Features struct {
	Category                                      activity.Category
	Idle, Activity, InApp, Hour, Annoyance, Since int
}

// bucket returns the index of the first limit d is below, or len(limits).
func bucket(d time.Duration, limits ...time.Duration) int {
	for i, l := range limits {
		if d < l {
			return i
		}
	}
	return len(limits)
}

// Features buckets an observation.
func (o Observation) Features() Features {
	f := Features{
		Category: o.Category,
		Idle:     bucket(o.Idle, 30*time.Second, 2*time.Minute, activity.AwayAfter),
		InApp:    bucket(o.InApp, 10*time.Minute, time.Hour),
		Hour:     ((o.Hour%24 + 24) % 24) / 6,
		Since:    bucket(o.SinceAction, 2*time.Minute, 15*time.Minute),
	}
	switch {
	case o.Activity < 0.1:
		f.Activity = 0
	case o.Activity < 0.5:
		f.Activity = 1
	default:
		f.Activity = 2
	}
	switch {
	case o.Annoyance < 0.5:
		f.Annoyance = 0
	case o.Annoyance < 1.5:
		f.Annoyance = 1
	default:
		f.Annoyance = 2
	}
	if f.Category >= activity.NumCategories {
		f.Category = activity.Other
	}
	return f
}

// Encode maps an observation to its table index.
func Encode(o Observation) State {
	f := o.Features()
	s := int(f.Category)
	s = s*idleBuckets + f.Idle
	s = s*activityBuckets + f.Activity
	s = s*inAppBuckets + f.InApp
	s = s*hourBuckets + f.Hour
	s = s*annoyBuckets + f.Annoyance
	s = s*sinceBuckets + f.Since
	return State(s)
}

// Features decodes a table index back into its buckets.
func (s State) Features() Features {
	v := int(s)
	var f Features
	f.Since, v = v%sinceBuckets, v/sinceBuckets
	f.Annoyance, v = v%annoyBuckets, v/annoyBuckets
	f.Hour, v = v%hourBuckets, v/hourBuckets
	f.InApp, v = v%inAppBuckets, v/inAppBuckets
	f.Activity, v = v%activityBuckets, v/activityBuckets
	f.Idle, v = v%idleBuckets, v/idleBuckets
	f.Category = activity.Category(v)
	return f
}

var (
	idleNames  = [idleBuckets]string{"active", "paused", "idle", "away"}
	levelNames = [3]string{"low", "mid", "high"}
	inAppNames = [inAppBuckets]string{"<10m", "<1h", "1h+"}
	hourNames  = [hourBuckets]string{"night", "morning", "afternoon", "evening"}
	annoyNames = [annoyBuckets]string{"calm", "irritated", "fed-up"}
	sinceNames = [sinceBuckets]string{"<2m", "<15m", "15m+"}
)

func (s State) String() string {
	f := s.Features()
	return fmt.Sprintf("%s/%s/input-%s/app-%s/%s/%s/last-%s",
		f.Category, idleNames[f.Idle], levelNames[f.Activity], inAppNames[f.InApp],
		hourNames[f.Hour], annoyNames[f.Annoyance], sinceNames[f.Since])
}
