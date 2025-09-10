package logger

import (
	"log/slog"
	"os"
)

// LoggerInterface определяет интерфейс для логгера, чтобы можно было использовать разные реализации (zap, slog и т.д.)
type LoggerInterface interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)
	With(args ...any) LoggerInterface
}

// SlogAdapter адаптер для slog.Logger
type SlogAdapter struct {
	logger *slog.Logger
}

func NewSlogAdapter(logger *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: logger}
}

func (s *SlogAdapter) Debug(msg string, args ...any) {
	s.logger.Debug(msg, args...)
}

func (s *SlogAdapter) Info(msg string, args ...any) {
	s.logger.Info(msg, args...)
}

func (s *SlogAdapter) Warn(msg string, args ...any) {
	s.logger.Warn(msg, args...)
}

func (s *SlogAdapter) Error(msg string, args ...any) {
	s.logger.Error(msg, args...)
}

func (s *SlogAdapter) Fatal(msg string, args ...any) {
	s.logger.Error(msg, args...)
	os.Exit(1)
}

func (s *SlogAdapter) With(args ...any) LoggerInterface {
	return &SlogAdapter{logger: s.logger.With(args...)}
}
