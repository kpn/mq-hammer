package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	mqtt "github.com/rollulus/paho.mqtt.golang"

	"github.com/rollulus/mq-hammer/pkg/hammer"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	_ "net/http/pprof"

	"net/http"
)

var brokerAddr string
var scenarioFile string
var referenceFile string
var nAgents int
var sleep time.Duration
var clientIDPrefix string
var agentLogFormat string
var credentialsFile string
var insecure bool
var disableMqttTLS bool
var prometheusSrv string
var verbose bool
var verboser bool

func init() {
	fs := rootCmd.PersistentFlags()
	fs.StringVar(&brokerAddr, "broker", "", "Broker address")
	fs.StringVar(&scenarioFile, "scenario", "", "File containing the scenario as JSON")
	fs.StringVar(&referenceFile, "ref", "", "File with the expected reference data as JSON")
	fs.IntVarP(&nAgents, "num-agents", "n", 1, "Number of agents to spin up")
	fs.DurationVar(&sleep, "sleep", 250*time.Millisecond, "Duration to wait between spinning up each agent")
	fs.StringVar(&clientIDPrefix, "client-id", "mq-hammer:"+hammer.GetVersion().GitTag, "Client ID prefix; a UUID is appended to it anyway")
	fs.StringVar(&agentLogFormat, "agent-logs", "", "Output per-agent logs to this go-templated filename, e.g. 'agent-{{ .ClientID }}.log', or - to log to stderr")
	fs.StringVar(&credentialsFile, "credentials", "", "File with TODO")
	fs.BoolVarP(&insecure, "insecure", "k", false, "Don't validate TLS hostnames / cert chains")
	fs.BoolVar(&disableMqttTLS, "disable-mqtt-tls", false, "Disable TLS for MQTT, use plain tcp sockets to the MQTT broker")
	fs.StringVar(&prometheusSrv, "prometheus", ":8080", "Export Prometheus metrics at this address")
	fs.BoolVarP(&verbose, "verbose", "v", false, "Verbose: output paho mqtt's internal logging (crit, err and warn) to stderr")
	fs.BoolVarP(&verboser, "verboser", "w", false, "Verboser: output paho mqtt's internal logging (crit, err, warn and debug) to stderr")

	rootCmd.AddCommand(versionCmd)

	if err := viper.BindPFlags(fs); err != nil {
		panic(err)
	}
	viper.AutomaticEnv()
}

var rootCmd = &cobra.Command{
	Use:   "mqhammer",
	Short: "MQ Hammer is an MQTT load testing tool",
	Long: `MQ Hammer is an MQTT load testing tool

    MQ Hammer will create --num-agents goroutines, each subscribing, unsubscribing
    and publish to topics at given timestamps according to the instructions in the
    --scenario file.

    It is possible to provide a reference data set that can be used for validation
    of static (and retained) data. With it, MQ Hammer knows when after a
    subscription the complete contents came in, how long it took to complete, if
    messages are missing, etc.

    By default, agents do not log to stderr to keep things clean a bit. For
    diagnostics, it is possible through --agent-logs to output logs to a file, one
    for each agent. Giving --agent-logs=- as argument will make the agents log to the
    default logger.

    All arguments can be specified through identically named environment variables
    as well.`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if brokerAddr == "" {
			return errors.New("broker cannot be empty")
		}

		// plug in default ports if unspecified
		if !strings.Contains(brokerAddr, ":") {
			if disableMqttTLS {
				brokerAddr += ":1883"
			} else {
				brokerAddr += ":8883"
			}
		}

		if scenarioFile == "" {
			return errors.New("scenario cannot be empty")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ver := hammer.GetVersion()
		logrus.WithFields(logrus.Fields{"GitCommit": ver.GitCommit, "GitTag": ver.GitTag, "SemVer": ver.SemVer}).Infof("this is MQ Hammer")

		// verbose?
		if verbose || verboser {
			l := logrus.StandardLogger()
			mqtt.CRITICAL = l
			mqtt.ERROR = l
			mqtt.WARN = l
		}
		if verboser {
			mqtt.DEBUG = logrus.StandardLogger()
		}

		// load scenario
		logrus.WithFields(logrus.Fields{"scenario": scenarioFile}).Infof("load scenario")
		scenario, err := newScenarioFromFile(scenarioFile)
		if err != nil {
			return err
		}

		// load optional reference data set
		var refSet ReferenceSet
		if referenceFile != "" {
			logrus.WithFields(logrus.Fields{"referenceFile": referenceFile}).Infof("load reference data")
			refSet, err = newReferenceSetFromFile(referenceFile)
			if err != nil {
				return err
			}
		}

		// mqtt creds from file
		var creds MqttCredentials
		creds = &fixedCreds{clientID: "asdfkuaoewriuweafksdjf"}
		if credentialsFile != "" { // creds from file
			logrus.WithFields(logrus.Fields{"credentialsFile": credentialsFile}).Infof("load credentials from file")
			fcreds, err := newMqttCredentialsFromFile(credentialsFile)
			if err != nil {
				return err
			}
			if nAgents > fcreds.Size() { // this test is only approximate, tokens might be used for mqtt metrics and distributed mode as well
				return fmt.Errorf("cannot create %d agents with only %d tokens provided", nAgents, fcreds.Size())
			}
			creds = fcreds
			logrus.WithFields(logrus.Fields{"credentialsFile": credentialsFile, "nCredentials": fcreds.Size()}).Infof("loaded credentials from file")
		}

		metricsHandlers := []MetricsHandler{&consoleMetrics{}}

		// expose prometheus metrics?
		if prometheusSrv != "" {
			logrus.WithFields(logrus.Fields{"address": prometheusSrv}).Info("export Prometheus metrics")
			pm, err := newPrometheusMetrics(prometheusSrv)
			if err != nil {
				return err
			}
			metricsHandlers = append(metricsHandlers, pm)
		}

		// start event funnel processing
		eventFunnel := newChanneledFunnel(metricsHandlers...)
		go eventFunnel.Process()

		// create agent controller
		ctl := newAgentController(brokerAddr, nAgents, agentLogFormat, sleep, creds, eventFunnel, refSet, scenario)
		if insecure {
			ctl.tlsInsecure = true
		}
		if disableMqttTLS {
			ctl.disableMqttTLS = true
		}

		// run!
		logrus.Infof("go")
		ctl.Control()

		return nil
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of MQ Hammer",
	Run: func(cmd *cobra.Command, args []string) {
		ver := hammer.GetVersion()
		fmt.Printf("MQ Hammer %s (%s) %s\n", ver.SemVer, ver.GitTag, ver.GitCommit)
	}}

func main() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
