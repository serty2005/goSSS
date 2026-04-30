package middleware

import (
	"context"
	loggerinfra "etalon-server/internal/infra/logger"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
}

func TestStatusRecorderFlushDelegatesToUnderlyingWriter(t *testing.T) {
	base := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: base}

	rec.Flush()

	if !base.flushed {
		t.Fatal("ожидалось, что Flush будет делегирован в базовый writer")
	}
}

func TestTimeoutUnlessSkipsConfiguredRequests(t *testing.T) {
	deadlineSeen := make(chan bool, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasDeadline := r.Context().Deadline()
		deadlineSeen <- hasDeadline
		w.WriteHeader(http.StatusOK)
	})

	withSkip := TimeoutUnless(10*time.Millisecond, func(r *http.Request) bool {
		return r.URL.Path == "/api/events"
	})(handler)

	reqSkip := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	recSkip := httptest.NewRecorder()
	withSkip.ServeHTTP(recSkip, reqSkip)
	if recSkip.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d для исключённого пути, получен %d", http.StatusOK, recSkip.Code)
	}
	if hasDeadline := <-deadlineSeen; hasDeadline {
		t.Fatal("для исключённого пути не ожидался deadline в контексте")
	}

	reqRegular := httptest.NewRequest(http.MethodGet, "/api/companies", nil)
	recRegular := httptest.NewRecorder()
	withSkip.ServeHTTP(recRegular, reqRegular)
	if recRegular.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d для обычного пути, получен %d", http.StatusOK, recRegular.Code)
	}
	if hasDeadline := <-deadlineSeen; !hasDeadline {
		t.Fatal("для обычного пути ожидался deadline в контексте")
	}
}

type capturedLogEntry struct {
	level string
	msg   string
	args  []any
}

type capturedLogger struct {
	entries *[]capturedLogEntry
	prefix  []any
}

func newCapturedLogger() *capturedLogger {
	entries := make([]capturedLogEntry, 0, 8)
	return &capturedLogger{entries: &entries}
}

func (l *capturedLogger) Debug(msg string, args ...any) {
	l.write("debug", msg, args...)
}

func (l *capturedLogger) Info(msg string, args ...any) {
	l.write("info", msg, args...)
}

func (l *capturedLogger) Warn(msg string, args ...any) {
	l.write("warn", msg, args...)
}

func (l *capturedLogger) Error(msg string, args ...any) {
	l.write("error", msg, args...)
}

func (l *capturedLogger) Fatal(msg string, args ...any) {
	l.write("fatal", msg, args...)
}

func (l *capturedLogger) With(args ...any) loggerinfra.LoggerInterface {
	combined := append(append([]any{}, l.prefix...), args...)
	return &capturedLogger{entries: l.entries, prefix: combined}
}

func (l *capturedLogger) write(level, msg string, args ...any) {
	combined := append(append([]any{}, l.prefix...), args...)
	*l.entries = append(*l.entries, capturedLogEntry{
		level: level,
		msg:   msg,
		args:  combined,
	})
}

func TestRequestLoggingMiddlewareWritesSingleAccessLogAndRedactsQuery(t *testing.T) {
	baseLogger := newCapturedLogger()
	router := chi.NewRouter()
	router.Use(chiMiddleware.RequestID)
	router.Use(LoggerInjector(baseLogger))
	router.Use(RequestLoggingMiddleware())
	router.Get("/api/test", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test?token=secret&limit=10", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusCreated, rec.Code)
	}
	if len(*baseLogger.entries) != 1 {
		t.Fatalf("ожидался один access-log, получено %d", len(*baseLogger.entries))
	}

	entry := (*baseLogger.entries)[0]
	if entry.level != "info" {
		t.Fatalf("ожидался уровень info, получен %s", entry.level)
	}
	if entry.msg != "HTTP запрос завершён" {
		t.Fatalf("ожидалось сообщение access-log, получено %q", entry.msg)
	}

	args := argsToMap(entry.args)
	if event, _ := args["event"].(string); event != "http_access" {
		t.Fatalf("ожидался event=http_access, получено %#v", args["event"])
	}
	if query, _ := args["query"].(string); query != "limit=10&token=%5BREDACTED%5D" {
		t.Fatalf("ожидалась маскировка query-параметров, получено %q", query)
	}
	if route, _ := args["route"].(string); route != "/api/test" {
		t.Fatalf("ожидался route=/api/test, получено %q", route)
	}
	if responseBytes, _ := args["response_bytes"].(int); responseBytes != 2 {
		t.Fatalf("ожидался размер ответа 2 байта, получено %v", args["response_bytes"])
	}
}

func TestRecovererReturnsInternalServerErrorAndLogsPanic(t *testing.T) {
	baseLogger := newCapturedLogger()
	router := chi.NewRouter()
	router.Use(chiMiddleware.RequestID)
	router.Use(LoggerInjector(baseLogger))
	router.Use(RequestLoggingMiddleware())
	router.Use(Recoverer())
	router.Get("/panic", func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic?token=secret", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusInternalServerError, rec.Code)
	}
	if len(*baseLogger.entries) < 2 {
		t.Fatalf("ожидалось минимум две записи лога, получено %d", len(*baseLogger.entries))
	}

	panicEntry := (*baseLogger.entries)[0]
	if panicEntry.level != "error" {
		t.Fatalf("ожидался error-лог паники, получен %s", panicEntry.level)
	}

	args := argsToMap(panicEntry.args)
	if event, _ := args["event"].(string); event != "http_panic" {
		t.Fatalf("ожидался event=http_panic, получено %#v", args["event"])
	}
	if panicValue, _ := args["panic"].(string); panicValue != "boom" {
		t.Fatalf("ожидалась причина паники boom, получено %q", panicValue)
	}
	if query, _ := args["query"].(string); query != "token=%5BREDACTED%5D" {
		t.Fatalf("ожидалась маскировка токена, получено %q", query)
	}
	stacktrace, _ := args["stacktrace"].(string)
	if !strings.Contains(stacktrace, "TestRecovererReturnsInternalServerErrorAndLogsPanic") {
		t.Fatalf("ожидался stacktrace с именем теста, получено %q", stacktrace)
	}
}

func TestGetLoggerFallsBackWhenContextDoesNotContainLogger(t *testing.T) {
	logger := GetLogger(context.Background())
	if logger == nil {
		t.Fatal("ожидался fallback-логгер")
	}
}

func argsToMap(args []any) map[string]any {
	result := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		result[key] = args[i+1]
	}
	return result
}
