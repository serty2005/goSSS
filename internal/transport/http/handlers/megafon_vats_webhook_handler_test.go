package handlers

import (
	"context"
	"errors"
	"etalon-server/internal/services"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
