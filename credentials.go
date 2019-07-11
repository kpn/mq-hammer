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
