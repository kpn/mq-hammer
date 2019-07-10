package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	units "github.com/docker/go-units"
	mqtt "github.com/rollulus/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
)

// Metrics groups a bunch of metrics that result from events produced by the event funnel
type Metrics struct {
	Agents       int      // gauge, number of agent
	Connects     int      // counter, connects
	Subscribes   int      // counter, subscribes
	Unsubscribes int      // counter, unsubscribes
	Publishes    int      // counter, publishes
	RxMessages   int      // counter, messages, including bad ones
	RxBytes      int      // counter, sum of payload bytes
	TxBytes      int      // counter, sum of payload bytes
	Unexpected   int      // counter, received but not in reference set
	Duplicate    int      // counter, received > 1x
	Missing      int      // counter, in reference set but not received
	BadPayload   int      // counter, payload in reference set is different
	MqttError    struct { // counter, for which action did the broker return an error?
		Connect     int
		Publish     int
		Subscribe   int
		Unsubscribe int
	}
	tPublish   []time.Duration // publish durations since last metric
	tConnects  []time.Duration // connect durations since last metric
	tFirstMsg  []time.Duration // first message durations since last metric, only if validation is available
	tCompletes []time.Duration // complete message set durations since last metric, only if validation is available
}

// Durations is a slice of time.Duration
type Durations []time.Duration

func (a Durations) Len() int           { return len(a) }
func (a Durations) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a Durations) Less(i, j int) bool { return a[i] < a[j] }

// Get provides access by index
func (a Durations) Get(i int) time.Duration { return a[i] }

// Clone duplicates
func (a Durations) Clone() Percentilables { return append(Durations(nil), a...) }

// Percentilables are not only sortable but also allow obtaining the data
type Percentilables interface {
	sort.Interface
	Get(int) time.Duration
	Clone() Percentilables
}

func computePercentiles(data Percentilables, ps []float64) []time.Duration {
	n := data.Len()

	if n < 1 {
		return nil
	}

	s := []time.Duration{}
	mydata := data.Clone()
	sort.Sort(mydata)
	for _, r := range ps {
		v := mydata.Get(int(r * float64(n)))
		s = append(s, v)
	}
	return s
}

// Percentiles are used to summarise multiple observations of a metric by the console logger
type Percentiles struct {
	Samples int
	Pcts    []float64
	Values  []float64
}

func roundDuration(d time.Duration) time.Duration {
	if d < time.Millisecond {
		return d
	}
	if d < time.Second {
		return d.Round(time.Microsecond)
	}
	return d.Round(time.Millisecond)
}

func (p *Percentiles) String() string {
	ss := []string{}
	for i, v := range p.Values {
		dur := time.Duration(v * float64(time.Second))
		ss = append(ss, fmt.Sprintf("%.2f%% <= %s", p.Pcts[i], roundDuration(dur)))
	}
	return fmt.Sprintf("%d samples; %s", p.Samples, strings.Join(ss, "; "))
}

func pct(durs []time.Duration, pcts []float64) Percentiles {
	res := computePercentiles(Durations(durs), pcts)
	fres := []float64{}
	for _, r := range res {
		fres = append(fres, r.Seconds())
	}
	return Percentiles{Samples: len(durs), Pcts: pcts, Values: fres}
}

func limit(durs []time.Duration, cap int) []time.Duration {
	if len(durs) > cap {
		return durs[len(durs)-cap:]
	}
	return durs
}

// MetricsHandler handle metrics
type MetricsHandler interface {
	HandleMetrics(m *Metrics) error
}

type consoleMetrics struct {
	prev  Metrics
	tLast time.Time

	totFirstMsg  []time.Duration
	totCompletes []time.Duration
	totConnects  []time.Duration
}

const percentileSamples = 1024

func (c *consoleMetrics) HandleMetrics(m *Metrics) error {
	pcts := []float64{.05, .1, .5, .9, .95, .99}
	c.totFirstMsg = append(c.totFirstMsg, m.tFirstMsg...)
	c.totConnects = append(c.totConnects, m.tConnects...)
	c.totCompletes = append(c.totCompletes, m.tCompletes...)
	c.totFirstMsg = limit(c.totFirstMsg, percentileSamples)
	c.totCompletes = limit(c.totCompletes, percentileSamples)
	c.totConnects = limit(c.totConnects, percentileSamples)
	FirstMsgTime := pct(c.totFirstMsg, pcts)
	CompleteMsgTime := pct(c.totCompletes, pcts)
	ConnTime := pct(c.totConnects, pcts)

	if c.tLast.IsZero() {
		c.tLast = time.Now()
		c.prev = *m
		return nil
	}
	if time.Since(c.tLast) < 1*time.Second {
		return nil
	}
	dt := time.Since(c.tLast).Seconds()
	logrus.Infof("ag=%d subs=%d (%.1f/s) unsubs=%d (%.1f/s) pubs=%d (%.1f/s) %s inmsgs=%d (%.1f/s) %s unexp=%d dup=%d miss=%d badpl=%d err={%s}",
		m.Agents,
		m.Subscribes,
		float64(m.Subscribes-c.prev.Subscribes)/dt,
		m.Unsubscribes,
		float64(m.Unsubscribes-c.prev.Unsubscribes)/dt,
		m.Publishes,
		float64(m.Publishes-c.prev.Publishes)/dt,
		units.HumanSize(float64(m.TxBytes)),
		m.RxMessages,
		float64(m.RxMessages-c.prev.RxMessages)/dt,
		units.HumanSize(float64(m.RxBytes)),
		m.Unexpected,
		m.Duplicate,
		m.Missing,
		m.BadPayload,
		fmt.Sprintf("c=%d,p=%d,s=%d,u=%d", m.MqttError.Connect, m.MqttError.Publish, m.MqttError.Subscribe, m.MqttError.Unsubscribe),
	)
	logrus.Infof("t_connect               : %s", ConnTime.String())
	logrus.Infof("t_firstMsg - t_subscribe: %s", FirstMsgTime.String())
	logrus.Infof("t_lastMsg - t_firstMsg  : %s", CompleteMsgTime.String())
	c.tLast = time.Now()
	c.prev = *m
	return nil
}

type mqttMetrics struct {
	client     mqtt.Client
	topic      string
	interval   time.Duration
	lastOutput time.Time
}

func (c *mqttMetrics) HandleMetrics(m *Metrics) error {
	if time.Since(c.lastOutput) < c.interval {
		return nil
	}
	if !c.client.IsConnected() {
		if t := c.client.Connect(); t.Wait() && t.Error() != nil {
			return fmt.Errorf("metrics publisher: %s", t.Error())
		}
	}

	bs, err := json.Marshal(struct {
		ProducedAt time.Time
		Metrics
	}{ProducedAt: time.Now(), Metrics: *m})
	if err != nil {
		return fmt.Errorf("metrics publisher: %s", err)
	}

	if t := c.client.Publish(c.topic, 0, true, bs); t.Wait() && t.Error() != nil {
		return fmt.Errorf("metrics publisher: %s", t.Error())
	}

	c.lastOutput = time.Now()
	return nil
}
