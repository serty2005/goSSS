package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// NewSlogLogger инициализирует slog-логгер с единым JSON-форматом
// для stdout и файлового логирования.
func NewSlogLogger(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	level := getSlogLevel(logLevel)
	consoleHandler := newJSONHandler(os.Stdout, level)

	var handler slog.Handler = consoleHandler

	if !disableFileLogging {
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			slog.Error("Не удалось создать директорию для логов, файловое логирование отключено", "dir", logDir, "error", err)
		} else {
			logPath := filepath.Join(logDir, loggerName+".log")
			fileWriter := &lumberjack.Logger{
				Filename:   logPath,
				MaxSize:    10,
				MaxBackups: 3,
				MaxAge:     28,
				Compress:   true,
			}
			handler = NewMultiHandler(consoleHandler, newJSONHandler(fileWriter, level))
		}
	}

	slogLogger := slog.New(handler).With("component", loggerName)
	return NewSlogAdapter(slogLogger)
}

func newJSONHandler(writer io.Writer, level slog.Leveler) slog.Handler {
	base := slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level:     level,
		AddSource: false,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			switch attr.Key {
			case slog.TimeKey:
				if attr.Value.Kind() == slog.KindTime {
					return slog.String(attr.Key, attr.Value.Time().Format(time.RFC3339Nano))
				}
			case slog.LevelKey:
				return slog.String(attr.Key, strings.ToLower(attr.Value.String()))
			}

			if err, ok := attr.Value.Any().(error); ok && err != nil {
				return slog.String(attr.Key, err.Error())
			}

			return attr
		},
	})
	return &dedupeJSONHandler{next: base}
}

// getSlogLevel преобразует строковый уровень логирования в slog.Level.
func getSlogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type multiHandler struct {
	handlers []slog.Handler
}

type dedupeJSONHandler struct {
	next  slog.Handler
	attrs []slog.Attr
	group string
}

// NewMultiHandler создает обработчик, который дублирует записи во все переданные обработчики.
func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (t *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *dedupeJSONHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *dedupeJSONHandler) Handle(ctx context.Context, r slog.Record) error {
	merged := make([]slog.Attr, 0, len(h.attrs)+8)
	indexByKey := make(map[string]int, len(h.attrs)+8)

	for _, attr := range h.attrs {
		mergeAttr(&merged, indexByKey, h.qualifyAttr(attr))
	}

	r.Attrs(func(attr slog.Attr) bool {
		mergeAttr(&merged, indexByKey, h.qualifyAttr(attr))
		return true
	})

	record := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	record.AddAttrs(merged...)
	return h.next.Handle(ctx, record)
}

func (h *dedupeJSONHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	combined := append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &dedupeJSONHandler{
		next:  h.next,
		attrs: combined,
		group: h.group,
	}
}

func (h *dedupeJSONHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}

	group := name
	if h.group != "" {
		group = h.group + "." + name
	}

	return &dedupeJSONHandler{
		next:  h.next,
		attrs: append([]slog.Attr{}, h.attrs...),
		group: group,
	}
}

func (h *dedupeJSONHandler) qualifyAttr(attr slog.Attr) slog.Attr {
	if h.group == "" || attr.Key == "" {
		return attr
	}
	attr.Key = h.group + "." + attr.Key
	return attr
}

func mergeAttr(attrs *[]slog.Attr, indexByKey map[string]int, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}

	if attr.Key == "" {
		*attrs = append(*attrs, attr)
		return
	}

	if index, ok := indexByKey[attr.Key]; ok {
		(*attrs)[index] = attr
		return
	}

	indexByKey[attr.Key] = len(*attrs)
	*attrs = append(*attrs, attr)
}

func (t *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				continue
			}
		}
	}
	return nil
}

func (t *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &multiHandler{handlers: newHandlers}
}

func (t *multiHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &multiHandler{handlers: newHandlers}
}

// Устаревшая функция для совместимости, но теперь использует slog.
func New(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	return NewSlogLogger(logDir, loggerName, logLevel, disableFileLogging)
}
