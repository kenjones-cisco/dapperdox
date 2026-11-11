package render

import (
	"log/slog"

	"github.com/kenjones-cisco/dapperdox/logger"
)

func log() *slog.Logger {
	return logger.Logger().With(slog.Any("pkg", "render"))
}
