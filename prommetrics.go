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
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type prometheusMetrics struct {
	agents             prometheus.Gauge
	messages           *prometheus.GaugeVec
	messageBytes       *prometheus.GaugeVec
	mqttActions        *prometheus.GaugeVec
	mqttErrors         *prometheus.GaugeVec
	messageValidations *prometheus.GaugeVec
	tMqttAction        *prometheus.HistogramVec
	tComplete          prometheus.Histogram
	tFirst             prometheus.Histogram
}

func newPrometheusMetrics(addr string) (*prometheusMetrics, error) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	s := &http.Server{Addr: addr, Handler: mux}
	go func() {
		panic(s.ListenAndServe())
	}()

	pm := &prometheusMetrics{}

	pm.agents = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "agents",
		Help:      "number of active agents",
	})

	pm.mqttActions = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "mqtt_operations_total",
		Help:      "number of mqtt operations attempted",
	}, []string{"operation"})

	pm.mqttErrors = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "mqtt_errors_total",
		Help:      "number of mqtt operations failed",
	}, []string{"operation"})

	pm.messageValidations = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "message_validation_problems_total",
		Help:      "number of message validations occurred",
	}, []string{"error"})

	pm.messages = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "mqtt_messages_total",
		Help:      "number of mqtt messages",
	}, []string{"direction"})

	pm.messageBytes = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mqhammer",
		Name:      "mqtt_messages_bytes",
		Help:      "number of mqtt messages bytes",
	}, []string{"direction"})

	pm.tComplete = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "mqhammer",
		Name:      "completion_time_seconds",
		Help:      "message completion histogram",
		Buckets:   prometheus.DefBuckets,
	})

	pm.tFirst = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "mqhammer",
		Name:      "first_msg_time_seconds",
		Help:      "first message histogram",
		Buckets:   prometheus.DefBuckets,
	})

	pm.tMqttAction = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "mqhammer",
		Name:      "mqtt_action_seconds",
		Help:      "mqtt action timing histogram",
		Buckets:   prometheus.DefBuckets,
	}, []string{"operation"})

	prometheus.MustRegister(pm.agents)
	prometheus.MustRegister(pm.mqttActions)
	prometheus.MustRegister(pm.mqttErrors)
	prometheus.MustRegister(pm.messageValidations)
	prometheus.MustRegister(pm.messages)
	prometheus.MustRegister(pm.messageBytes)
	prometheus.MustRegister(pm.tComplete)
	prometheus.MustRegister(pm.tFirst)
	prometheus.MustRegister(pm.tMqttAction)

	return pm, nil
}

func (c *prometheusMetrics) HandleMetrics(m *Metrics) error {
	c.agents.Set(float64(m.Agents))

	c.mqttActions.WithLabelValues("connect").Set(float64(m.Connects))
	c.mqttActions.WithLabelValues("subscribe").Set(float64(m.Subscribes))
	c.mqttActions.WithLabelValues("unsubscribe").Set(float64(m.Unsubscribes))
	c.mqttActions.WithLabelValues("publish").Set(float64(m.Publishes))

	c.mqttErrors.WithLabelValues("connect").Set(float64(m.MqttError.Connect))
	c.mqttErrors.WithLabelValues("subscribe").Set(float64(m.MqttError.Subscribe))
	c.mqttErrors.WithLabelValues("unsubscribe").Set(float64(m.MqttError.Unsubscribe))
	c.mqttErrors.WithLabelValues("publish").Set(float64(m.MqttError.Publish))

	c.messageValidations.WithLabelValues("bad_payload").Set(float64(m.BadPayload))
	c.messageValidations.WithLabelValues("missing").Set(float64(m.Missing))
	c.messageValidations.WithLabelValues("unexpected").Set(float64(m.Unexpected))
	c.messageValidations.WithLabelValues("duplicate").Set(float64(m.Duplicate))

	c.messages.WithLabelValues("in").Set(float64(m.RxMessages))
	c.messages.WithLabelValues("out").Set(float64(m.Publishes))

	c.messageBytes.WithLabelValues("in").Set(float64(m.RxBytes))
	c.messageBytes.WithLabelValues("out").Set(float64(m.TxBytes))

	for _, v := range m.tCompletes {
		c.tComplete.Observe(v.Seconds())
	}

	for _, v := range m.tFirstMsg {
		c.tFirst.Observe(v.Seconds())
	}

	for _, v := range m.tConnects {
		c.tMqttAction.WithLabelValues("connect").Observe(v.Seconds())
	}

	for _, v := range m.tPublish {
		c.tMqttAction.WithLabelValues("publish").Observe(v.Seconds())
	}

	return nil
}
