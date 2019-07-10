package main

type perAgentLogger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
}

type noopLogger struct{}

func (l *noopLogger) Infof(format string, args ...interface{}) {

}

func (l *noopLogger) Warnf(format string, args ...interface{}) {

}

func (l *noopLogger) Errorf(format string, args ...interface{}) {

}
