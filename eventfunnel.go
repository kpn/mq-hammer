package main

import (
	"time"

	"github.com/sirupsen/logrus"
)

// EventFunnel is used to gather events on mqtt message events from multiple agents
type EventFunnel interface {
	Subscribe(id, topic string, validating bool)
	Publish(id, topic string, len int, dt time.Duration)
	Message(id, topic string, len int)
	Duplicate(id, topic string)
	Unexpected(id, topic string)
	BadPayload(id, topic string)
	Missing(id, topic, key string)
	Unsubcribe(id, topic string)
	Connect(id string, dt time.Duration)
	AddAgent(id string)
	RemoveAgent(id string)
	MqttError(id string, err mqttError)
}

// eventType corresponds to the functions of the EventFunnel interface
type mqttError int

const (
	subscribeError mqttError = iota
	unsubscribeError
	connectError
	publishError
)

// channeledFunnel implements EventFunnel, all functions calls are translated into a corresponding eventsMsg
// which is sent to a channel where it gets processed.
type channeledFunnel struct {
	eventChan      chan *event
	metricHandlers []MetricsHandler
}

func newChanneledFunnel(metricHandlers ...MetricsHandler) *channeledFunnel {
	return &channeledFunnel{eventChan: make(chan *event, 1024), metricHandlers: metricHandlers}
}

// eventType corresponds to the functions of the EventFunnel interface
type eventType int

const (
	subscribe eventType = iota
	message
	duplicate
	unexpected
	badpayload
	missing
	unsubscribe
	connect
	addAgent
	removeAgent
	publish
	mqttErr
)

// event is a timestamped equivalent of a EventFunnel interface function
type event struct {
	timestamp  time.Time
	typ        eventType
	clientID   string
	topic      string
	validating bool          // only message for event == subscribe
	len        int           // only valid for event == message | publish
	dt         time.Duration // only valid for event == connect | publish
	key        string        // only valid for event == missing
	err        mqttError     // only valid for event == mqttErr
}

func (h *channeledFunnel) Subscribe(id, topic string, validating bool) {
	h.eventChan <- &event{timestamp: time.Now(), typ: subscribe, clientID: id, topic: topic, validating: validating}
}
func (h *channeledFunnel) Publish(id, topic string, len int, dt time.Duration) {
	h.eventChan <- &event{timestamp: time.Now(), typ: publish, clientID: id, topic: topic, len: len, dt: dt}
}
func (h *channeledFunnel) Message(id, topic string, len int) {
	h.eventChan <- &event{timestamp: time.Now(), typ: message, clientID: id, topic: topic, len: len}
}
func (h *channeledFunnel) Duplicate(id, topic string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: duplicate, clientID: id, topic: topic}
}
func (h *channeledFunnel) Unexpected(id, topic string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: unexpected, clientID: id, topic: topic}
}
func (h *channeledFunnel) BadPayload(id, topic string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: badpayload, clientID: id, topic: topic}
}
func (h *channeledFunnel) Missing(id, topic, key string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: missing, clientID: id, topic: topic, key: key}
}
func (h *channeledFunnel) Unsubcribe(id, topic string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: unsubscribe, clientID: id, topic: topic}
}
func (h *channeledFunnel) Connect(id string, dt time.Duration) {
	h.eventChan <- &event{timestamp: time.Now(), typ: connect, clientID: id, dt: dt}
}
func (h *channeledFunnel) AddAgent(id string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: addAgent, clientID: id}
}
func (h *channeledFunnel) RemoveAgent(id string) {
	h.eventChan <- &event{timestamp: time.Now(), typ: removeAgent, clientID: id}
}
func (h *channeledFunnel) MqttError(id string, err mqttError) {
	h.eventChan <- &event{timestamp: time.Now(), typ: mqttErr, clientID: id, err: err}
}

// subscriptionData is used to keep track of events (number of and timing) happening during a subscription
type subscriptionData struct {
	actualMessageCount int
	validating         bool
	subscribedAt       time.Time
	firstMessageAt     time.Time
	lastMessageAt      time.Time
}

// Process blocks, while processing incoming events via the channels. Events are aggregated and dispatched every second to all metric handlers
func (h *channeledFunnel) Process() {
	m := Metrics{}
	subData := map[string]map[string]*subscriptionData{}
	ticks := time.NewTicker(time.Second).C
	for {
		select {
		// every tick, log some statistics
		case <-ticks:
			for _, mh := range h.metricHandlers {
				if err := mh.HandleMetrics(&m); err != nil {
					logrus.Warnf("metrics handler error: %s", err)
				}
			}

			m.tCompletes = nil
			m.tConnects = nil
			m.tFirstMsg = nil
			m.tPublish = nil

		// process events coming from the eventFunnel's chan
		case event := <-h.eventChan:
			stats := subData[event.clientID][event.topic]
			switch event.typ {
			case connect:
				m.tConnects = append(m.tConnects, event.dt)
				m.Connects++

			case subscribe:
				m.Subscribes++
				subData[event.clientID][event.topic] = &subscriptionData{subscribedAt: event.timestamp, validating: event.validating}

			case unsubscribe:
				m.Unsubscribes++
				if !stats.validating {
					break
				}
				// are we validating and got any messages? collect arrival times
				if !stats.lastMessageAt.IsZero() {
					m.tFirstMsg = append(m.tFirstMsg, stats.firstMessageAt.Sub(stats.subscribedAt))
					if stats.actualMessageCount > 1 {
						m.tCompletes = append(m.tCompletes, stats.lastMessageAt.Sub(stats.firstMessageAt))
					}
				}

			case publish:
				m.Publishes++
				m.TxBytes += event.len
				m.tPublish = append(m.tPublish, event.dt)

			case message:
				m.RxMessages++
				m.RxBytes += event.len
				stats.lastMessageAt = event.timestamp
				stats.actualMessageCount++
				if stats.firstMessageAt.IsZero() {
					stats.firstMessageAt = event.timestamp
				}

			case duplicate:
				m.Duplicate++

			case unexpected:
				m.Unexpected++

			case badpayload:
				m.BadPayload++

			case missing:
				m.Missing++

			case addAgent:
				m.Agents++
				subData[event.clientID] = map[string]*subscriptionData{}

			case removeAgent:
				m.Agents--

			case mqttErr:
				switch event.err {
				case connectError:
					m.MqttError.Connect++
				case publishError:
					m.MqttError.Publish++
				case subscribeError:
					m.MqttError.Subscribe++
				case unsubscribeError:
					m.MqttError.Unsubscribe++
				}
			}
		}
	}
}
