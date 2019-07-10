package main

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
)

// ReferenceSet provides for a wildcard subscription all expected messages
type ReferenceSet interface {
	GetMessages(topic string) map[string][]byte
}

// CachingReferenceSet caches each answer to a GetMessages. It assumes that the total set of topics passed to GetMessages() is limited.
type CachingReferenceSet struct {
	ref map[string][]byte

	refcache map[string]map[string][]byte
	sync.RWMutex
}

// GetMessages provides all messages that match topic, assuming a wildcard subscription
func (r *CachingReferenceSet) GetMessages(topic string) map[string][]byte {
	topic = strings.TrimRight(topic, "#")
	r.RLock()
	if _, ok := r.refcache[topic]; ok {
		defer r.RUnlock()
		return r.refcache[topic]
	}
	r.RUnlock()
	submap := make(map[string][]byte)
	for k, v := range r.ref {
		if strings.HasPrefix(k, topic) {
			submap[k] = v
		}
	}
	r.Lock()
	r.refcache[topic] = submap
	r.Unlock()
	return submap
}

// newReferenceSetFromFile loads a reference set
func newReferenceSetFromReader(f io.Reader) (*CachingReferenceSet, error) {
	var ref map[string][]byte
	if err := json.NewDecoder(f).Decode(&ref); err != nil {
		return nil, err
	}
	return &CachingReferenceSet{ref: ref, refcache: map[string]map[string][]byte{}}, nil
}

// newReferenceSetFromFile loads a reference set
func newReferenceSetFromFile(file string) (*CachingReferenceSet, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return newReferenceSetFromReader(f)
}
