package logger

import (
	"log"
	"log/slog"
	"os"
	"strings"
)

// ConfigureStdLogger переводит стандартный log-пакет на тот же JSON-формат,
// что и основной slog-логгер, и возвращает bootstrap-логгер.
func ConfigureStdLogger(component, logLevel string) LoggerInterface {
	base := slog.New(newJSONHandler(os.Stdout, getSlogLevel(logLevel))).With("component", component)
	log.SetFlags(0)
	log.SetOutput(&stdLogWriter{logger: base})
	slog.SetDefault(base)
	return NewSlogAdapter(base)
}

type stdLogWriter struct {
	logger *slog.Logger
}

func (w *stdLogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message == "" {
		return len(p), nil
	}
	w.logger.Info(message)
	return len(p), nil
}
