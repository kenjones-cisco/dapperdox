// Package logger provides the logging implementation for the dapperdox service.
package logger

import (
	"errors"
	"fmt"
	"io"
	stdlog "log"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	errInvalidLevel = errors.New("invalid log level")
	initialize      sync.Once
	level           slog.LevelVar
	instance        = newLogger(os.Stderr)
)

func newLogger(output io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
		Level: &level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				return slog.String(slog.TimeKey, attr.Value.Time().Format(RFC3339NanoFixed))
			}

			if attr.Key == slog.LevelKey && attr.Value.Any() == LevelTrace {
				return slog.String(slog.LevelKey, "TRACE")
			}

			return attr
		},
	}))
}

// RFC3339NanoFixed includes nanoseconds padded to a fixed width.
const RFC3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

// LevelTrace preserves the detailed diagnostics below slog's debug level.
const LevelTrace slog.Level = slog.LevelDebug - 4

// Logger returns the shared structured logger.
func Logger() *slog.Logger {
	initialize.Do(func() {
		slog.SetDefault(instance)
		stdlog.SetFlags(0)
		stdlog.SetOutput(slog.NewLogLogger(instance.Handler(), slog.LevelDebug).Writer())
	})

	return instance
}

// SetLevel changes the level for existing loggers, including package loggers.
func SetLevel(value string) error {
	var next slog.Level

	switch strings.ToLower(value) {
	case "", "info":
		next = slog.LevelInfo
	case "trace":
		next = LevelTrace
	case "debug":
		next = slog.LevelDebug
	case "warn", "warning":
		next = slog.LevelWarn
	case "error":
		next = slog.LevelError
	default:
		return fmt.Errorf("%w %q: use trace, debug, info, warn, or error", errInvalidLevel, value)
	}

	Logger()
	level.Set(next)

	return nil
}
