package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/rollulus/paho.mqtt.golang/packets"

	mqtt "github.com/rollulus/paho.mqtt.golang"
)

func TestTopicSet(t *testing.T) {
	ts0 := newTopicSet([]string{"a", "b", "c", "d", "e", "e", "e"})
	ts1 := newTopicSet([]string{"x", "b", "c", "y", "z", "x", "z"})
	ts2 := newTopicSet([]string{"a", "e", "e", "a", "q", "q", "q"})

	ts3 := ts0.except(ts1).except(ts2)

	elms := ts3.elements()

	if len(elms) != 1 || elms[0] != "d" {
		t.Fail()
	}
}

type mockPublishConnectFunnel struct {
	publish   func(id, topic string, len int, dt time.Duration)
	connect   func(id string, dt time.Duration)
	mqttError func(id string, err mqttError)
}

func (mf *mockPublishConnectFunnel) Publish(id, topic string, len int, dt time.Duration) {
	mf.publish(id, topic, len, dt)
}
func (mf *mockPublishConnectFunnel) Connect(id string, dt time.Duration) { mf.connect(id, dt) }
func (mf *mockPublishConnectFunnel) MqttError(id string, err mqttError)  { mf.mqttError(id, err) }

type mockToken struct {
	err        error
	returnCode byte // for Connect tokens
}

func (mt *mockToken) Wait() bool                     { return true }
func (mt *mockToken) WaitTimeout(time.Duration) bool { return true }
func (mt *mockToken) Error() error                   { return mt.err }
func (mt *mockToken) ReturnCode() byte               { return mt.returnCode } // for Connect tokens

type mqttClientMock struct {
	subscribeMultiple func(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token
	unsubscribe       func(topics ...string) mqtt.Token
	publish           func(topic string, qos byte, retained bool, payload interface{}) mqtt.Token
	connect           func() mqtt.Token
	disconnect        func(quiesce uint)
}

func (m *mqttClientMock) Connect() mqtt.Token     { return m.connect() }
func (m *mqttClientMock) Disconnect(quiesce uint) { m.disconnect(quiesce) }
func (m *mqttClientMock) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	return m.publish(topic, qos, retained, payload)
}
func (m *mqttClientMock) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	panic("not implemented")
}
func (m *mqttClientMock) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	return m.subscribeMultiple(filters, callback)
}
func (m *mqttClientMock) Unsubscribe(topics ...string) mqtt.Token { return m.unsubscribe(topics...) }

type msgHandlerMock struct {
	newMessageHandler func(filters []string) mqtt.MessageHandler
	endMessageHandler func(filters []string)
}

func (m *msgHandlerMock) NewMessageHandler(filters []string) mqtt.MessageHandler {
	return m.newMessageHandler(filters)
}
func (m *msgHandlerMock) EndMessageHandler(filters []string) {
	m.endMessageHandler(filters)
}

func TestAgentConnect(t *testing.T) {
	discCalled, conCalled := false, false
	c := &mqttClientMock{
		disconnect: func(q uint) {
			discCalled = true
		},
		connect: func() mqtt.Token {
			conCalled = true
			return &mockToken{}
		},
	}
	f := &mockPublishConnectFunnel{
		connect: func(id string, dt time.Duration) {

		},
	}

	// happy
	a := &agent{connectBackoff: backoff.NewConstantBackOff(0),
		mqtt: c, funnel: f, log: &noopLogger{}, clientID: "id", newMqttClient: func(o *mqtt.ClientOptions) mqttClient { return c }}
	if err := a.connect(context.TODO()); err != nil {
		t.Error(err)
	}

	if !discCalled || !conCalled {
		t.Error()
	}

	// unhappy, connect fails first recoverable and then irrecoverable
	connCalled, mqttErrCalled := 0, false
	c.connect = func() mqtt.Token {
		connCalled++
		if connCalled != 1 {
			return &mockToken{err: errors.New("x"), returnCode: packets.ErrRefusedBadUsernameOrPassword} // irrecoverable
		}
		return &mockToken{err: errors.New("x"), returnCode: packets.ErrRefusedServerUnavailable} // recoverable
	}
	f.mqttError = func(id string, err mqttError) {
		mqttErrCalled = true
		if err != connectError || id != "id" {
			t.Error()
		}
	}

	err := a.connect(context.TODO())
	if err == nil {
		t.Error()
	}
	if connCalled != 2 || !mqttErrCalled {
		t.Error()
	}
}

func TestAgentPublish(t *testing.T) {
	p := &Publication{"topic", []byte{1, 2, 3, 4}}
	fpublishCalled := false
	mpublishCalled := false
	c := &mqttClientMock{
		publish: func(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
			mpublishCalled = true
			if topic != p.Topic {
				t.Error()
			}
			if plbs := payload.([]byte); !bytes.Equal(plbs, p.Payload) {
				t.Error()
			}
			if retained {
				t.Error()
			}
			return &mockToken{}
		},
	}
	f := &mockPublishConnectFunnel{
		publish: func(id, topic string, ln int, dt time.Duration) {
			fpublishCalled = true
			if id != "id" || topic != p.Topic || ln != len(p.Payload) {
				t.Error()
			}
		},
	}
	a := &agent{mqtt: c, funnel: f, log: &noopLogger{}, clientID: "id"}

	// happy, no errors
	if err := a.publish(p); err != nil {
		t.Error(err)
	}

	if !fpublishCalled || !mpublishCalled {
		t.Error()
	}

	// sad, we'll error
	c.publish = func(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
		return &mockToken{err: io.EOF}
	}
	mqttErrorCalled := false
	f.mqttError = func(id string, err mqttError) {
		mqttErrorCalled = true
		if id != "id" || err != publishError {
			t.Error()
		}
	}

	// expect an io.EOF
	if err := a.publish(p); err != io.EOF || !mqttErrorCalled {
		t.Error()
	}
}

func TestAgentSubscribe(t *testing.T) {
	var nmhCalled, emhCalled bool
	mh := &msgHandlerMock{
		newMessageHandler: func(filters []string) mqtt.MessageHandler {
			nmhCalled = true
			if len(filters) != 1 || filters[0] != "d" {
				t.Error()
			}
			return func(mqtt.Client, mqtt.Message) {}
		},
		endMessageHandler: func(filters []string) {
			emhCalled = true
			if len(filters) != 1 || filters[0] != "a" {
				t.Error()
			}
		},
	}
	var submCalled, unsubCalled bool
	c := &mqttClientMock{
		subscribeMultiple: func(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
			submCalled = true
			if _, ok := filters["d"]; !ok {
				t.Error()
			}
			return &mockToken{}
		},
		unsubscribe: func(topics ...string) mqtt.Token {
			unsubCalled = true
			if len(topics) != 1 || topics[0] != "a" {
				t.Error()
			}
			return &mockToken{}
		},
	}
	a := &agent{mqtt: c, mh: mh, log: &noopLogger{}}

	thisStep := newTopicSet([]string{"b", "c", "d"})
	currentStep := newTopicSet([]string{"a", "b", "c"})

	err := a.subscribe(thisStep, currentStep)
	if err != nil {
		t.Error(err)
	}

	if !(nmhCalled && emhCalled && submCalled && unsubCalled) {
		t.Error()
	}
}

func TestAgentPerformScenario(t *testing.T) {
	mh := &msgHandlerMock{
		newMessageHandler: func(filters []string) mqtt.MessageHandler { return func(mqtt.Client, mqtt.Message) {} },
		endMessageHandler: func(filters []string) {},
	}
	nConnect := 0
	c := &mqttClientMock{
		disconnect: func(q uint) {},
		connect: func() mqtt.Token {
			nConnect++
			return &mockToken{}
		},
		subscribeMultiple: func(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
			if !(reflect.DeepEqual(filters, map[string]byte{"d": 0}) ||
				reflect.DeepEqual(filters, map[string]byte{"a": 0, "b": 0, "c": 0})) {
				t.Error()
			}
			return &mockToken{}
		},
		unsubscribe: func(topics ...string) mqtt.Token { // the list order is randomized due to go's map keys iteration
			us := map[string]bool{}
			for _, t := range topics {
				us[t] = true
			}
			if !reflect.DeepEqual(us, map[string]bool{"a": true, "b": true, "c": true}) {
				t.Error()
			}
			return &mockToken{}
		},
		publish: func(topic string, qos byte, retained bool, payload interface{}) mqtt.Token { return &mockToken{} },
	}
	f := &mockPublishConnectFunnel{
		connect:   func(id string, dt time.Duration) {},
		publish:   func(id, topic string, ln int, dt time.Duration) {},
		mqttError: func(id string, err mqttError) {},
	}
	a := &agent{funnel: f, connectBackoff: backoff.NewConstantBackOff(0), mqtt: c, mh: mh, log: &noopLogger{}, clientID: "id", newMqttClient: func(o *mqtt.ClientOptions) mqttClient { return c }}
	subscribe := make(chan Subscription)
	publish := make(chan Publication)
	disconnect := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	var perScnErr error
	go func() {
		perScnErr = a.performScenario(context.TODO(), subscribe, publish, disconnect)
		wg.Done()
	}()
	publish <- Publication{Topic: "t", Payload: []byte{1, 2, 3, 4}}
	disconnect <- struct{}{}
	subscribe <- Subscription([]string{"a", "b", "c"})
	subscribe <- Subscription([]string{"d"})
	close(subscribe)
	wg.Wait()
	if perScnErr != nil {
		t.Error()
	}
	if nConnect != 2 {
		t.Error()
	}
}

func TestMqttOpts(t *testing.T) {
	cfg := &tls.Config{}
	cfgInsec := &tls.Config{InsecureSkipVerify: true}
	os := mqttOpts("b", "u", "p", "c", cfg, false)
	if os.Username != "u" ||
		os.Password != "p" ||
		os.ClientID != "c" ||
		os.Servers[0].String() != "ssl://b" ||
		os.TLSConfig.InsecureSkipVerify {
		t.Error()
	}
	os = mqttOpts("bb", "u", "p", "c", cfgInsec, true)
	if os.Servers[0].String() != "tcp://bb" || !os.TLSConfig.InsecureSkipVerify {
		t.Error()
	}
}
