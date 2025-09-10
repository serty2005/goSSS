package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

// SimpleConsoleHandler - кастомный обработчик для упрощенного консольного вывода
type SimpleConsoleHandler struct {
	level     slog.Level
	component string
	requestID string
}

func (h *SimpleConsoleHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *SimpleConsoleHandler) Handle(ctx context.Context, r slog.Record) error {
	// Получаем время
	timeStr := r.Time.Format("2006-01-02T15:04:05.000-07:00")

	// Получаем уровень
	levelStr := r.Level.String()

	// Используем сохраненный компонент или ищем в атрибутах
	component := h.component
	requestID := h.requestID
	if component == "" || requestID == "" {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "component" && component == "" {
				component = a.Value.String()
			}
			if a.Key == "request_id" && requestID == "" {
				requestID = a.Value.String()
			}
			return true
		})
	}

	// Получаем сообщение
	msg := r.Message

	// Форматируем вывод
	if component != "" && requestID != "" {
		fmt.Printf("%s %s %s[%s]: %q\n", timeStr, levelStr, component, requestID, msg)
	} else if component != "" {
		fmt.Printf("%s %s %s: %q\n", timeStr, levelStr, component, msg)
	} else if requestID != "" {
		fmt.Printf("%s %s [%s]: %q\n", timeStr, levelStr, requestID, msg)
	} else {
		fmt.Printf("%s %s %q\n", timeStr, levelStr, msg)
	}

	return nil
}

func (h *SimpleConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Создаем новый обработчик с атрибутами
	newHandler := &SimpleConsoleHandler{
		level:     h.level,
		component: h.component,
		requestID: h.requestID,
	}

	// Сохраняем атрибуты для использования в Handle
	for _, attr := range attrs {
		if attr.Key == "component" {
			newHandler.component = attr.Value.String()
		}
		if attr.Key == "request_id" {
			newHandler.requestID = attr.Value.String()
		}
	}

	return newHandler
}

func (h *SimpleConsoleHandler) WithGroup(name string) slog.Handler {
	// Для простоты не поддерживаем WithGroup в кастомном обработчике
	return h
}

// NewSlogLogger инициализирует логгер slog.
// Он может писать логи как в консоль (читаемый формат), так и в файл (JSON для парсинга).
func NewSlogLogger(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	level := getSlogLevel(logLevel)

	// Кастомный обработчик для консоли с упрощенным форматом
	consoleHandler := &SimpleConsoleHandler{
		level: level,
	}

	var handlers []slog.Handler
	handlers = append(handlers, consoleHandler)

	// Обработчик для файла (JSON)
	if !disableFileLogging {
		// Создаем директорию для логов, если ее нет
		if err := os.MkdirAll(logDir, 0755); err != nil {
			// В случае ошибки просто не будем писать в файл
			slog.Error("Не удалось создать директорию для логов, файловое логирование отключено.", "dir", logDir, "error", err)
		} else {
			logPath := filepath.Join(logDir, loggerName+".log")
			fileWriter := &lumberjack.Logger{
				Filename:   logPath,
				MaxSize:    10, // megabytes
				MaxBackups: 3,
				MaxAge:     28, // days
				Compress:   true,
			}

			fileHandler := slog.NewJSONHandler(fileWriter, &slog.HandlerOptions{
				Level:     level,
				AddSource: false,
			})
			handlers = append(handlers, fileHandler)
		}
	}

	// Объединяем обработчики
	var handler slog.Handler
	if len(handlers) == 1 {
		handler = handlers[0]
	} else {
		handler = &TeeHandler{handlers: handlers}
	}

	// Создаем логгер с полем component
	slogLogger := slog.New(handler).With("component", loggerName)

	return NewSlogAdapter(slogLogger)
}

// TeeHandler объединяет несколько обработчиков
type TeeHandler struct {
	handlers []slog.Handler
}

func (t *TeeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *TeeHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *TeeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithAttrs(attrs)
	}
	return &TeeHandler{handlers: newHandlers}
}

func (t *TeeHandler) WithGroup(name string) slog.Handler {
	newHandlers := make([]slog.Handler, len(t.handlers))
	for i, h := range t.handlers {
		newHandlers[i] = h.WithGroup(name)
	}
	return &TeeHandler{handlers: newHandlers}
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

// Устаревшая функция для совместимости, но теперь использует slog
func New(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	return NewSlogLogger(logDir, loggerName, logLevel, disableFileLogging)
}
