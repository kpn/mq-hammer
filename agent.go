package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"os"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/eclipse/paho.mqtt.golang/packets"
	mqtt "github.com/rollulus/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// mqttClient is a subset of an mqtt.CLient
type mqttClient interface {
	Connect() mqtt.Token
	Disconnect(quiesce uint)
	Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token
	Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token
	SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token
	Unsubscribe(topics ...string) mqtt.Token
}

// PublishConnectFunnel is a subset of the EventFunnel interface
type PublishConnectFunnel interface {
	Publish(id, topic string, len int, dt time.Duration)
	Connect(id string, dt time.Duration)
	MqttError(id string, err mqttError)
}

type tokenWithReturnCode interface {
	ReturnCode() byte
}

type agent struct {
	mqtt           mqttClient
	mqttOpts       *mqtt.ClientOptions
	log            perAgentLogger
	clientID       string
	mh             MessageHandlerer
	funnel         PublishConnectFunnel
	connectBackoff backoff.BackOff
	newMqttClient  func(o *mqtt.ClientOptions) mqttClient // nil defaults to mqtt.NewClient but is plugabble for testing
}

func newAgent(mqttOpts *mqtt.ClientOptions, clientID string, log perAgentLogger, mh MessageHandlerer, funnel PublishConnectFunnel) *agent {
	return &agent{
		mqttOpts:       mqttOpts,
		clientID:       clientID,
		log:            log,
		mh:             mh,
		funnel:         funnel,
		connectBackoff: backoff.NewExponentialBackOff(),
	}
}

// connect tries to connect the mqtt client using our backoff
func (a *agent) connect(ctx context.Context) error {
	if a.mqtt != nil {
		a.log.Infof("disconnect old client")
		a.mqtt.Disconnect(0)
	}
	a.log.Infof("new client")
	if a.newMqttClient != nil {
		a.mqtt = a.newMqttClient(a.mqttOpts)
	} else {
		a.mqtt = mqtt.NewClient(a.mqttOpts)
	}
	a.log.Infof("connect")
	a.connectBackoff.Reset()
	bckoff := backoff.WithContext(a.connectBackoff, ctx)
	return backoff.Retry(backoff.Operation(func() error {
		a.log.Infof("connect to mqtt")
		t0 := time.Now()
		if t := a.mqtt.Connect(); t.Wait() && t.Error() != nil {
			code := t.(tokenWithReturnCode).ReturnCode()
			err := fmt.Errorf("mqtt connect: %s", t.Error())
			a.log.Warnf("cannot connect: %s", err)
			a.funnel.MqttError(a.clientID, connectError)
			if code == packets.ErrRefusedBadUsernameOrPassword || code == packets.ErrRefusedNotAuthorised {
				return backoff.Permanent(err) // permanent error; retrying won't help us
			}
			return err // non-permanent error
		}
		a.funnel.Connect(a.clientID, time.Since(t0))
		a.log.Infof("connected")
		return nil
	}), bckoff)
}

func (a *agent) publish(pub *Publication) error {
	a.log.Infof("publish `%s`, %d bytes", pub.Topic, len(pub.Payload))
	t0 := time.Now()
	if t := a.mqtt.Publish(pub.Topic, 0, false, pub.Payload); t.Wait() && t.Error() != nil {
		a.log.Warnf("mqtt publish to `%s`: %s", pub.Topic, t.Error())
		a.funnel.MqttError(a.clientID, publishError)
		return t.Error()
	}
	a.funnel.Publish(a.clientID, pub.Topic, len(pub.Payload), time.Since(t0))
	return nil
}

// subscribe subscribes to all
func (a *agent) subscribe(stepSet topicSet, curTopics topicSet) error {
	newTopics := stepSet.except(curTopics).elements()   // topics we aren't yet subscribed to while we should
	ditchTopics := curTopics.except(stepSet).elements() // topics we were subscribed to but shouldn't be anymore

	if len(newTopics) > 0 {
		a.log.Infof("subscribe %v", newTopics)
		newsubs := make(map[string]byte)
		for _, f := range newTopics {
			newsubs[f] = 0
		}
		t := a.mqtt.SubscribeMultiple(newsubs, a.mh.NewMessageHandler(newTopics))
		go func(token mqtt.Token) {
			if token.Wait() && token.Error() != nil {
				a.log.Warnf("mqtt subscribe to `%v`: %s", newTopics, token.Error())
				a.funnel.MqttError(a.clientID, subscribeError)
			}
		}(t)
	}

	if len(ditchTopics) > 0 {
		t := a.mqtt.Unsubscribe(ditchTopics...)
		go func(token mqtt.Token) {
			if token.Wait() && token.Error() != nil {
				a.log.Warnf("mqtt unsubscribe from to `%v`: %s", ditchTopics, token.Error())
				a.funnel.MqttError(a.clientID, unsubscribeError)
			}
		}(t)
		a.mh.EndMessageHandler(ditchTopics)
	}
	return nil
}

// performScenario subscribes the topics in subscriptions and publishes to the topics in publications
func (a *agent) performScenario(ctx context.Context, subscribe <-chan Subscription, publish <-chan Publication, disconnect <-chan struct{}) error {
connectLoop:
	for {
		if err := a.connect(ctx); err != nil {
			return err
		}

		curTopics := topicSet{}

		for {
			select {
			case <-ctx.Done():
				a.log.Infof("agent finished: ctx.Done")
				break connectLoop

			// publish
			case pub, ok := <-publish:
				if !ok {
					a.log.Infof("agent finished: pubs chan closed")
					break connectLoop
				}
				if err := a.publish(&pub); err != nil {
					continue connectLoop
				}

			// update subscription set
			case sub, ok := <-subscribe:
				if !ok {
					a.log.Infof("agent finished: subs chan closed")
					break connectLoop
				}
				thisStep := newTopicSet(sub)
				if err := a.subscribe(thisStep, curTopics); err != nil {
					continue connectLoop
				}

				curTopics = thisStep

			// disconnect
			case _, ok := <-disconnect:
				if !ok {
					a.log.Infof("agent finished: discs chan closed")
					break connectLoop
				}
				a.log.Infof("voluntary disconnect")
				a.mh.EndMessageHandler(curTopics.elements())
				a.mqtt.Disconnect(0)
				a.mqtt = nil // the lousy mqtt client fails on multiple disconnects 🙄
				continue connectLoop
			}
		}
	}

	if a.mqtt != nil {
		a.mqtt.Disconnect(0)
	}

	return nil
}

// mqttOpts converts the arguments into matching mqtt.ClientOptions
func mqttOpts(broker, user, pass, clientID string, insecure, disableTLS bool) *mqtt.ClientOptions {
	o := mqtt.NewClientOptions()
	o.SetClientID(clientID)
	o.SetUsername(user)
	o.SetPassword(pass)
	o.SetAutoReconnect(false)
	o.SetProtocolVersion(4) // set version to MQTT 3.1.1 and hence disable fallback to 3.1

	if disableTLS {
		o.AddBroker("tcp://" + broker)
	} else {
		o.SetTLSConfig(&tls.Config{InsecureSkipVerify: insecure})
		o.AddBroker("ssl://" + broker)
	}

	return o
}

// newLoggerForAgent returns a logger. templ is a go-templated filename. "" returns a discarding logger, "-" logs to stderr and anything is interpreted as file name
func newLoggerForAgent(clientID, templ string) (perAgentLogger, string, error) {
	if templ == "" {
		return &noopLogger{}, "", nil
	}
	if templ == "-" {
		return logrus.New(), "", nil
	}
	logger := logrus.New()
	t, err := template.New("x").Parse(templ)
	if err != nil {
		return nil, "", err
	}
	var res bytes.Buffer
	if err = t.Execute(&res, map[string]string{"ClientID": clientID}); err != nil {
		return nil, "", err
	}
	fn := res.String()
	f, err := os.OpenFile(fn, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, "", err
	}
	logger.Out = f
	return logger, fn, nil
}

// topicSet is used to diff between desired and actual subscriptions
type topicSet map[string]bool

func newTopicSet(ts []string) topicSet {
	s := topicSet{}
	for _, t := range ts {
		s[t] = true
	}
	return s
}

func (ats topicSet) except(bts topicSet) topicSet {
	var cts []string
	for t := range ats {
		if bts[t] {
			continue
		}
		cts = append(cts, t)
	}
	return newTopicSet(cts)
}

func (ats topicSet) elements() []string {
	elts := make([]string, 0, len(ats))
	for el := range ats {
		elts = append(elts, el)
	}
	return elts
}
