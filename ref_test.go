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
	"bytes"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

func TestReferenceSetCache(t *testing.T) {
	t0, p0 := "/this/is/a/lengthy/key/name", []byte("correct_payload_0")
	t1, p1 := "/this/is/shorter", []byte("payload_1")
	b64 := base64.StdEncoding.EncodeToString
	ref, err := newReferenceSetFromReader(strings.NewReader(fmt.Sprintf(`{
		"%s": "%s",
		"%s": "%s"
	}`, t0, b64(p0), t1, b64(p1))))

	if err != nil {
		t.Error(err)
	}

	// exact
	res := ref.GetMessages(t0)
	if len(res) != 1 || !bytes.Equal(res[t0], p0) {
		t.Fail()
	}

	// cached
	res = ref.GetMessages(t0)
	if len(res) != 1 || !bytes.Equal(res[t0], p0) {
		t.Fail()
	}

	// wildcard
	res = ref.GetMessages("/this/is/#")
	if len(res) != 2 || !bytes.Equal(res[t0], p0) || !bytes.Equal(res[t1], p1) {
		t.Fail()
	}

	// wildcard cached
	res = ref.GetMessages("/this/is/#")
	if len(res) != 2 || !bytes.Equal(res[t0], p0) || !bytes.Equal(res[t1], p1) {
		t.Fail()
	}

	// nonsnse
	res = ref.GetMessages("/not/existing/#")
	if len(res) != 0 {
		t.Fail()
	}

	// nonsnse cached
	res = ref.GetMessages("/not/existing/#")
	if len(res) != 0 {
		t.Fail()
	}

}
