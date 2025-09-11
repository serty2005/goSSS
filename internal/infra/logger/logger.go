// Файл: internal/infra/logger/logger.go
package logger

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// NewSlogLogger инициализирует логгер slog.
// Он пишет логи в консоль (читаемый кастомный формат) и в файл (JSON для парсинга).
func NewSlogLogger(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	level := getSlogLevel(logLevel)

	// Наш новый кастомный обработчик для красивого вывода в консоль
	consoleHandler := NewPrettyConsoleHandler(os.Stdout, &PrettyHandlerOptions{
		Level: level,
	})

	var handler slog.Handler = consoleHandler

	// Если файловое логирование включено, создаем MultiHandler
	if !disableFileLogging {
		if err := os.MkdirAll(logDir, 0755); err != nil {
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

			// Объединяем обработчики
			handler = NewMultiHandler(consoleHandler, fileHandler)
		}
	}

	// Создаем логгер с начальным полем component
	slogLogger := slog.New(handler).With("component", loggerName)

	return NewSlogAdapter(slogLogger)
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

// --- MultiHandler для вывода в несколько мест ---

type multiHandler struct {
	handlers []slog.Handler
}

// NewMultiHandler создает обработчик, который дублирует записи во все переданные обработчики.
func NewMultiHandler(handlers ...slog.Handler) slog.Handler {
	return &multiHandler{handlers: handlers}
}

func (t *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	// Достаточно, чтобы хотя бы один обработчик был включен
	for _, h := range t.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (t *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range t.handlers {
		if h.Enabled(ctx, r.Level) {
			// Ошибки от одного обработчика не должны прерывать другие
			if err := h.Handle(ctx, r.Clone()); err != nil {
				// В реальном приложении здесь можно логировать ошибку самого логгера
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

// Устаревшая функция для совместимости, но теперь использует slog
func New(logDir, loggerName, logLevel string, disableFileLogging bool) LoggerInterface {
	return NewSlogLogger(logDir, loggerName, logLevel, disableFileLogging)
}

// --- PrettyConsoleHandler для красивого вывода в консоль ---

// PrettyHandlerOptions определяет опции для PrettyConsoleHandler.
type PrettyHandlerOptions struct {
	Level slog.Leveler
}

// PrettyConsoleHandler - это slog.Handler, который выводит логи в красивом, человеко-читаемом формате.
type PrettyConsoleHandler struct {
	opts   PrettyHandlerOptions
	writer io.Writer
	mu     *sync.Mutex
	attrs  []slog.Attr // Атрибуты, добавленные через WithAttrs
	group  string      // Текущая группа
}

func NewPrettyConsoleHandler(w io.Writer, opts *PrettyHandlerOptions) *PrettyConsoleHandler {
	if opts == nil {
		opts = &PrettyHandlerOptions{}
	}
	if opts.Level == nil {
		opts.Level = slog.LevelInfo
	}
	return &PrettyConsoleHandler{
		opts:   *opts,
		writer: w,
		mu:     new(sync.Mutex),
	}
}

func (h *PrettyConsoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *PrettyConsoleHandler) Handle(_ context.Context, r slog.Record) error {
	buf := new(bytes.Buffer)

	// 1. Формируем префикс: [ВРЕМЯ] [УРОВЕНЬ]
	buf.WriteString(fmt.Sprintf("[%s] [%s]", r.Time.Format("2006-01-02 15:04:05.000"), r.Level.String()))

	// 2. Собираем все атрибуты (из With и из самой записи) в одну мапу.
	// Атрибуты из записи имеют приоритет.
	allAttrs := make(map[string]slog.Value)
	for _, attr := range h.attrs {
		allAttrs[attr.Key] = attr.Value
	}
	r.Attrs(func(a slog.Attr) bool {
		allAttrs[a.Key] = a.Value
		return true
	})

	// 3. Извлекаем и форматируем специальные поля (component, request_id)
	if component, ok := allAttrs["component"]; ok {
		buf.WriteString(fmt.Sprintf(" [%s]", component.String()))
		delete(allAttrs, "component") // Удаляем, чтобы не выводить дважды
	}
	if requestID, ok := allAttrs["request_id"]; ok {
		buf.WriteString(fmt.Sprintf(" [%s]", requestID.String()))
		delete(allAttrs, "request_id")
	}

	// 4. Добавляем основное сообщение
	buf.WriteString(" " + r.Message)
	buf.WriteByte('\n')

	// 5. Выводим остальные атрибуты, каждый с новой строки с отступом
	if len(allAttrs) > 0 {
		for key, value := range allAttrs {
			buf.WriteString(fmt.Sprintf("    %s: %v\n", key, value.Any()))
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.writer.Write(buf.Bytes())
	return err
}

func (h *PrettyConsoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Создаем новый обработчик, чтобы сохранить неизменность (immutability)
	newHandler := &PrettyConsoleHandler{
		opts:   h.opts,
		writer: h.writer,
		mu:     h.mu,
		group:  h.group,
	}

	// Копируем существующие атрибуты и добавляем новые.
	// Новые атрибуты перезаписывают старые с теми же ключами.
	attrMap := make(map[string]slog.Value)
	for _, attr := range h.attrs {
		attrMap[attr.Key] = attr.Value
	}
	for _, attr := range attrs {
		attrMap[attr.Key] = attr.Value
	}

	newAttrs := make([]slog.Attr, 0, len(attrMap))
	for key, value := range attrMap {
		newAttrs = append(newAttrs, slog.Attr{Key: key, Value: value})
	}

	newHandler.attrs = newAttrs
	return newHandler
}

func (h *PrettyConsoleHandler) WithGroup(name string) slog.Handler {
	// Для простоты не будем усложнять группы, но оставим возможность
	if name == "" {
		return h
	}
	newHandler := *h
	if newHandler.group != "" {
		newHandler.group += "."
	}
	newHandler.group += name
	return &newHandler
}
