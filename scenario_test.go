// Copyright 2019 Koninklijke KPN N.V.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
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
