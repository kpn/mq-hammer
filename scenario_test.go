package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewScenarioFromReader(t *testing.T) {
	j := `
	[
		{
			"t":15,
			"subscribe":[],
			"publish":[{"topic":"t","payload":"Y29vbAo="}]
		},	
		{
			"t":11,
			"subscribe":["a"]
		},
		{
			"t":17,
			"subscribe":["a"],
			"disconnect":true
		}	
	]
	`

	exp := []Step{
		{T: 11, Subscribe: &[]string{"a"}, Wait: time.Second * 4},
		{T: 15, Subscribe: &[]string{}, Publish: []Publication{{Topic: "t", Payload: []byte("cool\x0a")}}, Wait: time.Second * 2},
		{T: 17, Subscribe: &[]string{"a"}, Disconnect: true, Wait: time.Second * 5},
	}

	steps, err := newScenarioFromReader(strings.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(exp, steps) {
		t.Fail()
	}

	// invalid json
	if _, err := newScenarioFromReader(strings.NewReader("so much fun")); err == nil {
		t.Fail()
	}
}

func TestInfiniteRandomScenarioChans(t *testing.T) {
	steps := []Step{
		{Subscribe: &[]string{"a"}, Wait: time.Nanosecond},
		{Subscribe: &[]string{}, Publish: []Publication{{Topic: "t", Payload: []byte("cool\x0a")}}, Wait: time.Nanosecond},
		{Subscribe: &[]string{"b"}, Disconnect: true, Wait: time.Nanosecond},
	}

	irs := NewInfiniteRandomScenario(context.TODO(), steps)
	irs.randFunc = func(int) int { return 0 }
	sub, pub, disc := irs.Chans()

	// step0
	s := <-sub
	if s[0] != "a" {
		t.Fail()
	}

	// step1
	var p Publication
	select {
	case pp := <-pub:
		p = pp
	case ss := <-sub:
		s = ss
	}
	select {
	case pp := <-pub:
		p = pp
	case ss := <-sub:
		s = ss
	}
	if p.Topic != "t" || string(p.Payload) != "cool\x0a" {
		t.Fail()
	}
	if len(s) != 0 {
		t.Fail()
	}

	// step2
	select {
	case ss := <-sub:
		s = ss
	case <-disc:
	}
	select {
	case ss := <-sub:
		s = ss
	case <-disc:
	}
	if s[0] != "b" {
		t.Fail()
	}
}
