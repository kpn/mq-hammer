package main

import (
	"context"
	"crypto/tls"
	"time"

	"github.com/sirupsen/logrus"
)

type agentController struct {
	active         map[*agent]context.CancelFunc
	brokerAddr     string
	target         int
	creds          MqttCredentials
	eventFunnel    EventFunnel
	refSet         ReferenceSet
	steps          []Step
	interval       time.Duration
	tlsConfig      *tls.Config
	disableMqttTLS bool
	agentLogFormat string

	targetUpdate chan int
}

func newAgentController(brokerAddr string, tlsConfig *tls.Config, target int, agentLogFormat string, interval time.Duration, creds MqttCredentials, eventFunnel EventFunnel, refSet ReferenceSet, steps []Step) *agentController {
	return &agentController{
		active:         map[*agent]context.CancelFunc{},
		brokerAddr:     brokerAddr,
		target:         target,
		creds:          creds,
		eventFunnel:    eventFunnel,
		refSet:         refSet,
		steps:          steps,
		interval:       interval,
		tlsConfig:      tlsConfig,
		agentLogFormat: agentLogFormat,
		targetUpdate:   make(chan int),
	}
}

// spawn creates agents asynchronously. The finished and broken channels are used to communicate what happens with an agent after it has been created
func (s *agentController) spawn(finished, broken chan *agent) (*agent, error) {
	user, pass, clientID, err := s.creds.Get()
	if err != nil {
		return nil, err
	}

	logger, filename, err := newLoggerForAgent(clientID, agentLogFormat)
	if err != nil {
		return nil, err
	}

	mqCliOpts := mqttOpts(s.brokerAddr, user, pass, clientID, s.tlsConfig, s.disableMqttTLS)

	var msgHandler MessageHandlerer
	if s.refSet != nil {
		msgHandler = newValidatingHandler(clientID, s.refSet, s.eventFunnel, logger)
	} else {
		msgHandler = newMessageHandler(clientID, s.eventFunnel, logger)
	}

	logrus.WithFields(logrus.Fields{"clientID": clientID, "log": filename, "broker": s.brokerAddr, "disableTls": s.disableMqttTLS}).Infof("new agent")
	logger.Infof("clientID: %s, broker: %s, disableMqttTls: %t, user: %s, pass: %s", clientID, s.brokerAddr, s.disableMqttTLS, user, pass)

	agent := newAgent(mqCliOpts, clientID, logger, msgHandler, s.eventFunnel)
	s.eventFunnel.AddAgent(clientID)

	ctx, cancel := context.WithCancel(context.Background())
	s.active[agent] = cancel

	go func() {
		scenario := NewInfiniteRandomScenario(ctx, s.steps)

		subs, pubs, discs := scenario.Chans()
		if err := agent.performScenario(ctx, subs, pubs, discs); err != nil {
			logrus.Warnf("agent %s died: %s", clientID, err)
			logger.Errorf("died: %s", err)
			broken <- agent
		} else {
			logrus.Infof("agent %s finished", clientID)
			finished <- agent
		}
		s.eventFunnel.RemoveAgent(clientID)
	}()

	return agent, nil
}

func (s *agentController) SetNumAgents(n int) {
	logrus.Warnf("SetNumAgents not implemented") // TODO
}

func (s *agentController) GetNumAgents() (actual, desired int) {
	logrus.Warnf("GetNumAgents not implemented") // TODO
	return 0, 0
}

// Control tries to spawn as many agents as requested. When agents start to fail, the spawning rate is reduced
func (s *agentController) Control() {
	tgt := s.target
	act := 0

	finished, broken := make(chan *agent), make(chan *agent)

	lastBrokenAgentAt := time.Time{}

	normalSpawn := s.interval                        // spawn agents every normalSpawn
	slowSpawn := 2500*time.Millisecond + normalSpawn // in case of problems, spawn slower
	const gracePeriod = 15 * time.Second             // stay slow during this period

	for {
		sleep := slowSpawn // assume we're slow unless proven otherwise

		if act < tgt {
			if _, err := s.spawn(finished, broken); err == nil {
				// good spawn? spawn normal
				sleep = normalSpawn
				act++
			} else {
				// bad spawn? go slow
				logrus.Warnf("cannot spawn agent: %s", err)
				sleep = slowSpawn
			}
		}

		if time.Since(lastBrokenAgentAt) < gracePeriod {
			// in the grace period? go slow
			sleep = slowSpawn
		}

		// now process incoming messages for at most sleep seconds
		ticker := time.After(sleep)
	empty:
		for {
			select {
			case <-finished:
				act--
			case <-broken:
				act--
				if time.Since(lastBrokenAgentAt) > gracePeriod {
					logrus.Infof("agents are breaking, we'll spawn new agents slower (every %s instead of %s) for the next %s", slowSpawn, normalSpawn, gracePeriod)
				}
				lastBrokenAgentAt = time.Now()
			case <-ticker:
				break empty
			}
		}
	}
}
