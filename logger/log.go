// Package logger provides the logging implementation for the dapperdox service.
package logger

import (
	stdlog "log"
	"sync"

	"github.com/sirupsen/logrus"
)

var (
	initialize sync.Once
	_logger    logrus.Ext1FieldLogger
)

// RFC3339NanoFixed is time.RFC3339Nano with nanoseconds padded using zeros to
// ensure the formatted time is always the same number of characters.
const RFC3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

// Logger configures a logger instance and returns instance.
func Logger() logrus.Ext1FieldLogger {
	l := newLogger()

	return l
}

// SetLevel sets the level at which log messages are published/written.
func SetLevel(level string) {
	newLogger()

	// If there's no explicit logging level specified, set the level to INFO
	if level == "" {
		level = "info"
	}

	loglevel, err := logrus.ParseLevel(level)
	if err == nil {
		// set default logger and the custom logger levels
		logrus.SetLevel(loglevel)
		_logger.(*logrus.Entry).Logger.SetLevel(loglevel)
	}
}

func newLogger() logrus.Ext1FieldLogger {
	initialize.Do(func() {
		l := logrus.New()
		// configure the default logger to include timestamps and quote empty fields to make visually
		// seeing an empty Field easier. These configurations will not impact or influence the
		// configuration of the logstash hook below.
		l.Formatter = &logrus.TextFormatter{
			TimestampFormat:  RFC3339NanoFixed,
			FullTimestamp:    true,
			QuoteEmptyFields: true,
		}

		_logger = logrus.NewEntry(l)

		// disable any flags that result in a prefix to the message
		// otherwise there will be duplicate timestamps, etc.
		stdlog.SetFlags(0)
		// use WriterLevel to define at what level library code messages should
		// be included into the logs. Most of the time the messages should be silent
		// unless additional diagnostics are required.
		stdlog.SetOutput(_logger.(*logrus.Entry).WriterLevel(logrus.DebugLevel))
		_logger.Debug("Logger initialized")
	})

	return _logger
}
