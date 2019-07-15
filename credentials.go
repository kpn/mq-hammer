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
	"encoding/csv"
	"errors"
	"os"

	uuid "github.com/satori/go.uuid"
)

// MqttCredentials provides mqtt credentials
type MqttCredentials interface {
	Get() (username string, password string, clientID string, err error) // a zero value for clientID means: generate one
}

type fixedCreds struct {
	username string
	password string
	clientID string
}

func (f *fixedCreds) Get() (string, string, string, error) {
	return f.username, f.password, f.clientID + uuid.NewV4().String(), nil
}

type fileCreds struct {
	creds [][]string
}

// csv: username,password,clientid
func newMqttCredentialsFromFile(filename string) (*fileCreds, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	vs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	return &fileCreds{vs}, nil
}

func (f *fileCreds) Get() (string, string, string, error) {
	if f.Size() == 0 {
		return "", "", "", errors.New("fileCreds ran out of credentials")
	}
	h := f.creds[0]
	if len(h) != 3 {
		return "", "", "", errors.New("expect 3 fields in credentials file")
	}
	f.creds = f.creds[1:]
	return h[0], h[1], h[2], nil
}

func (f *fileCreds) Size() int {
	return len(f.creds)
}
