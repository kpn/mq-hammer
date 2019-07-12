package main

import (
	"context"
	"encoding/json"
	"io"
	"math/rand"
	"os"
	"sort"
	"time"
)

// Publication is a mqtt key/value
type Publication struct {
	Topic   string
	Payload []byte
}

// Step contains the topics to subscribe to, the topics and payloads to publish, and the amount of time to wait after having done so
type Step struct {
	Subscribe  *[]string // null and empty have different semantics: null means no subscriptions update; empty means: subscribe to none
	Publish    []Publication
	Disconnect bool

	T    float64       // in file, timestamps are used
	Wait time.Duration // at runtime, we have durations to wait for each step, the delta between the timestamps
}

// Subscription is the set of topics to subscribe to
type Subscription []string

// InfiniteRandomScenario starts at a random position and loops through it ad infinitum
type InfiniteRandomScenario struct {
	steps    []Step
	ctx      context.Context
	randFunc func(int) int // randomness can be tamed during testing, defaults to rand.Intn
}

// NewInfiniteRandomScenario creates an InfiniteRandomScenario
func NewInfiniteRandomScenario(ctx context.Context, s []Step) *InfiniteRandomScenario {
	return &InfiniteRandomScenario{steps: s, ctx: ctx, randFunc: rand.Intn}
}

// Chans returns two channels. One emits topics to subscribe to, the other topics and payloads to publish
func (s *InfiniteRandomScenario) Chans() (<-chan Subscription, <-chan Publication, <-chan struct{}) {
	subsChan := make(chan Subscription)
	pubsChan := make(chan Publication)
	discChan := make(chan struct{})

	go func() {
		defer close(subsChan)
		defer close(pubsChan)
		defer close(discChan)

		i0 := s.randFunc(len(s.steps))
		// initial random wait to avoid multiple scenarios with the same i0 running in sync
		d := time.Duration(float64(s.steps[i0].Wait) * rand.Float64())
		select {
		case <-time.After(d):
		case <-s.ctx.Done():
			return
		}

		for i := i0; ; i++ {
			step := s.steps[i%len(s.steps)]
			tBegin := time.Now()

			// any subscriptions?
			if step.Subscribe != nil {
				select {
				case subsChan <- *step.Subscribe:
				case <-s.ctx.Done():
					return
				}
			}
			// any publications?
			for _, pub := range step.Publish {
				select {
				case pubsChan <- pub:
				case <-s.ctx.Done():
					return
				}
			}
			// any disconnect?
			if step.Disconnect {
				select {
				case discChan <- struct{}{}:
				case <-s.ctx.Done():
					return
				}
			}

			// wait the amount of time this scenario step should take
			tSLeep := step.Wait - time.Since(tBegin)
			if tSLeep > 0 {
				select {
				case <-time.After(tSLeep):
				case <-s.ctx.Done():
					return
				}
			}
		}
	}()

	return subsChan, pubsChan, discChan
}

// newScenarioFromFile loads a json file that contains instructions to which topics to subscribe at what moment
func newScenarioFromFile(file string) ([]Step, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return newScenarioFromReader(f)
}

type byTimestamp []Step

func (s byTimestamp) Len() int           { return len(s) }
func (s byTimestamp) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byTimestamp) Less(i, j int) bool { return s[i].T < s[j].T }

// newScenarioFromFile loads from a reader json that contains instructions to which topics to subscribe at what moment
func newScenarioFromReader(r io.Reader) ([]Step, error) {
	var steps []Step
	if err := json.NewDecoder(r).Decode(&steps); err != nil {
		return nil, err
	}
	sort.Sort(byTimestamp(steps))
	for i, q := range steps {
		if i+1 < len(steps) {
			steps[i].Wait = time.Duration((steps[i+1].T-q.T)*1000) * time.Millisecond
		} else {
			steps[i].Wait = 5 * time.Second // wait at the end
		}
	}
	return steps, nil
}
