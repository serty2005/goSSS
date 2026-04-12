package handlers

import (
	"context"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type fakeMegafonVATSIncomingService struct {
	handleErr error
}

func (f *fakeMegafonVATSIncomingService) HandleWebhook(_ context.Context, _ []byte, _ url.Values) error {
	return f.handleErr
}

func (f *fakeMegafonVATSIncomingService) Start(_ context.Context) {}

func (f *fakeMegafonVATSIncomingService) ReplayEvent(_ context.Context, _ string) error {
	return nil
}

func TestMegafonVATSWebhookHandler_ContentTypeValidation(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/megafon-vats/webhook", strings.NewReader("cmd=event"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался код %d, получен %d", http.StatusBadRequest, rec.Code)
	}
}

func TestMegafonVATSWebhookHandler_Unauthorized(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{handleErr: services.ErrMegafonVATSWebhookUnauthorized})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/megafon-vats/webhook", strings.NewReader("cmd=event"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался код %d, получен %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestMegafonVATSWebhookHandler_BadRequest(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{handleErr: services.ErrMegafonVATSWebhookBadRequest})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/megafon-vats/webhook", strings.NewReader("cmd=event"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался код %d, получен %d", http.StatusBadRequest, rec.Code)
	}
}

func TestMegafonVATSWebhookHandler_Accepted(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/integrations/megafon-vats/webhook",
		strings.NewReader("cmd=event&type=INCOMING&callid=abc-1&crm_token=test"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("ожидался код %d, получен %d", http.StatusAccepted, rec.Code)
	}
}

func TestMegafonVATSWebhookHandler_InternalError(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{handleErr: errors.New("ошибка")})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/megafon-vats/webhook", strings.NewReader("cmd=event"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ожидался код %d, получен %d", http.StatusInternalServerError, rec.Code)
	}
}

func TestMegafonVATSWebhookHandler_LogsMaskedPayloadAndFullURL(t *testing.T) {
	h := NewMegafonVATSWebhookHandler(&fakeMegafonVATSIncomingService{})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/integrations/megafon-vats/webhook",
		strings.NewReader("cmd=event&type=INCOMING&callid=abc-1&crm_token=supersecret"),
	)
	req.Host = "sd.example.ru"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	log := newCaptureWebhookLogger()
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.LoggerContextKey, log))

	rec := httptest.NewRecorder()
	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("ожидался код %d, получен %d", http.StatusAccepted, rec.Code)
	}

	requestEntry, ok := log.find("Мегафон ВАТС webhook запрос")
	if !ok {
		t.Fatal("ожидали лог входящего webhook-запроса")
	}
	requestFields := captureWebhookArgsToMap(requestEntry.args)
	if gotURL, _ := requestFields["url"].(string); gotURL != "https://sd.example.ru/api/integrations/megafon-vats/webhook" {
		t.Fatalf("ожидали полный URL webhook, получили %q", gotURL)
	}
	body, _ := requestFields["body"].(string)
	if strings.Contains(body, "supersecret") {
		t.Fatalf("ожидали маскирование crm_token в логе, получили %q", body)
	}
	if !strings.Contains(body, "crm_token=su") || !strings.Contains(body, "*") {
		t.Fatalf("ожидали замаскированный crm_token, получили %q", body)
	}

	responseEntry, ok := log.find("Мегафон ВАТС webhook ответ")
	if !ok {
		t.Fatal("ожидали лог webhook-ответа")
	}
	responseFields := captureWebhookArgsToMap(responseEntry.args)
	if gotStatus, ok := responseFields["status_code"].(int); !ok || gotStatus != http.StatusAccepted {
		t.Fatalf("ожидали status_code=202 в логе webhook-ответа, получили %#v", responseFields["status_code"])
	}
}

type captureWebhookLogEntry struct {
	msg  string
	args []any
}

type captureWebhookLogger struct {
	mu      sync.Mutex
	entries []captureWebhookLogEntry
}

func newCaptureWebhookLogger() *captureWebhookLogger {
	return &captureWebhookLogger{
		entries: make([]captureWebhookLogEntry, 0, 8),
	}
}

func (l *captureWebhookLogger) Debug(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureWebhookLogger) Info(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureWebhookLogger) Warn(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureWebhookLogger) Error(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureWebhookLogger) Fatal(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureWebhookLogger) With(args ...any) logger.LoggerInterface {
	return l
}

func (l *captureWebhookLogger) find(msg string) (captureWebhookLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.entries {
		if entry.msg == msg {
			return entry, true
		}
	}
	return captureWebhookLogEntry{}, false
}

func (l *captureWebhookLogger) append(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, captureWebhookLogEntry{
		msg:  msg,
		args: append([]any(nil), args...),
	})
}

func captureWebhookArgsToMap(args []any) map[string]any {
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
