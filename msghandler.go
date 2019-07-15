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
	"encoding/hex"
	"strings"
	"sync"

	mqtt "github.com/rollulus/paho.mqtt.golang"
)

// MessageHandlerer provides mqtt.MessageHandlers
type MessageHandlerer interface {
	NewMessageHandler(filters []string) mqtt.MessageHandler
	EndMessageHandler(filters []string)
}

type messageHandler struct {
	clientID string
	events   EventFunnel
	logger   perAgentLogger
}

func newMessageHandler(clientID string, eventFunnel EventFunnel, logger perAgentLogger) *messageHandler {
	return &messageHandler{clientID: clientID, events: eventFunnel, logger: logger}
}

func findMatchingFilter(filters []string, topic string) (string, bool) {
	for _, filter := range filters {
		if filter == topic {
			return filter, true
		}
		if strings.HasSuffix(filter, "/#") && strings.HasPrefix(topic, filter[:len(filter)-2]) {
			return filter, true
		}
	}

	return "", false
}

// NewMessageHandler returns a message handler that does not validate messages and hands them over to the event funnel
func (m *messageHandler) NewMessageHandler(filters []string) mqtt.MessageHandler {
	act := map[string][]byte{}
	for _, filter := range filters {
		m.events.Subscribe(m.clientID, filter, false)
	}
	return func(c mqtt.Client, msg mqtt.Message) {
		key := msg.Topic()
		filter, found := findMatchingFilter(filters, key)
		if !found {
			m.logger.Warnf("got message %s, no matching filters in %v", key, filters)
			filter = filters[0] // TODO this is a nasty hack to let the bookkeeping stay more or less accurate
		}

		if old, ok := act[key]; ok && bytes.Equal(old, msg.Payload()) {
			m.logger.Infof("got duplicate %s %s", filter, key)
			m.events.Duplicate(m.clientID, filter)
			return
		}
		act[key] = msg.Payload()
		if found {
			m.logger.Infof("got message %s %s", filter, key)
			m.events.Message(m.clientID, filter, len(msg.Payload()))
		}
	}
}

func (m *messageHandler) EndMessageHandler(filters []string) {
	for _, filter := range filters {
		m.events.Unsubcribe(m.clientID, filter)
	}
}

type validatingHandler struct {
	sync.RWMutex
	clientID  string
	reference ReferenceSet
	events    EventFunnel
	logger    perAgentLogger
	act       map[string]map[string]bool // filter -> key -> valid
}

func newValidatingHandler(clientID string, reference ReferenceSet, eventFunnel EventFunnel, logger perAgentLogger) *validatingHandler {
	return &validatingHandler{clientID: clientID, events: eventFunnel, reference: reference, logger: logger, act: map[string]map[string]bool{}}
}

// NewMessageHandler returns a message handler that validates received messages and hands them over to the event funnel
func (m *validatingHandler) NewMessageHandler(filters []string) mqtt.MessageHandler {
	expected := make(map[string]map[string][]byte)
	actual := make(map[string]map[string]bool)

	for _, filter := range filters {
		exp := m.reference.GetMessages(filter)
		act := map[string]bool{}
		expected[filter] = exp
		actual[filter] = act
		m.Lock()
		m.act[filter] = act
		m.Unlock()
		m.events.Subscribe(m.clientID, filter, true)
		m.logger.Infof("expecting %d messages for %s", len(exp), filter)
	}

	return func(c mqtt.Client, msg mqtt.Message) {
		key := msg.Topic()
		filter, found := findMatchingFilter(filters, key)
		if !found {
			m.logger.Warnf("could not match received topic %s to filter set %v", key, filters)
			filter = filters[0] // TODO this is nasty
		}
		m.events.Message(m.clientID, filter, len(msg.Payload()))
		if _, ok := actual[filter][key]; ok {
			m.logger.Infof("got duplicate %s %s", filter, key)
			m.events.Duplicate(m.clientID, filter)
			return
		}
		if _, ok := expected[filter][key]; !ok {
			m.logger.Warnf("got unexpected %s %s", filter, key)
			m.events.Unexpected(m.clientID, filter)
			return
		}
		if !bytes.Equal(msg.Payload(), expected[filter][key]) {
			m.logger.Warnf("got message %s for %s with unexpected payload:\n%s", key, filter, hex.Dump(msg.Payload()))
			m.events.BadPayload(m.clientID, filter)
			return
		}
		m.logger.Infof("got message %s %s", filter, key)
		actual[filter][key] = true
	}
}

func (m *validatingHandler) EndMessageHandler(filters []string) {
	for _, filter := range filters {
		exp := m.reference.GetMessages(filter)
		m.RLock()
		for key := range exp {
			if _, ok := m.act[filter][key]; !ok {
				m.logger.Warnf("missing message %s for filter %s", key, filter)
				m.events.Missing(m.clientID, filter, key)
			}
		}
		m.RUnlock()
		m.events.Unsubcribe(m.clientID, filter)
	}
}
