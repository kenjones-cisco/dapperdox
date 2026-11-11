package logger

import (
	"bytes"
	"errors"
	stdlog "log"
	"log/slog"
	"strings"
	"testing"
)

func TestSetLevelUpdatesExistingLoggers(t *testing.T) {
	before := level.Level()

	t.Cleanup(func() { level.Set(before) })

	child := Logger().With(slog.String("pkg", "test"))

	for _, value := range []struct {
		name string
		want slog.Level
	}{
		{"", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"trace", LevelTrace},
		{"debug", slog.LevelDebug},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
	} {
		t.Run(value.name, func(t *testing.T) {
			if err := SetLevel(value.name); err != nil {
				t.Fatal(err)
			}

			if !child.Enabled(t.Context(), value.want) || child.Enabled(t.Context(), value.want-1) {
				t.Fatalf("existing logger does not honor level %s", value.want)
			}
		})
	}

	current := level.Level()

	if err := SetLevel("invalid"); !errors.Is(err, errInvalidLevel) {
		t.Fatalf("invalid level error = %v", err)
	}

	if level.Level() != current {
		t.Fatal("invalid configuration changed the logging threshold")
	}
}

func TestStructuredOutputAndStandardLogBridge(t *testing.T) {
	before := level.Level()

	t.Cleanup(func() { level.Set(before) })
	level.Set(slog.LevelInfo)

	var output bytes.Buffer

	log := newLogger(&output).With(slog.String("pkg", "test"))
	bridge := stdlog.New(slog.NewLogLogger(log.Handler(), slog.LevelDebug).Writer(), "", 0)
	bridge.Print("library debug message")

	if output.Len() != 0 {
		t.Fatal("standard-library debug logs escaped the info threshold")
	}

	level.Set(LevelTrace)
	bridge.Print("library debug message")
	log.Log(t.Context(), LevelTrace, "trace message", slog.Int("routes", 19))
	log.ErrorContext(t.Context(), "fetch failed", slog.Any("error", errInvalidLevel))

	for _, want := range []string{"level=DEBUG", "pkg=test", "library debug message", "level=TRACE", "routes=19", `error="invalid log level"`} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("log output missing %q: %s", want, output.String())
		}
	}
}
