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

type fakeBitrixIncomingService struct {
	handleErr error
}

func (f *fakeBitrixIncomingService) HandleWebhook(_ context.Context, _ []byte, _ url.Values) error {
	return f.handleErr
}

func (f *fakeBitrixIncomingService) Start(_ context.Context) {}

func TestBitrixWebhookHandler_ContentTypeValidation(t *testing.T) {
	h := NewBitrixWebhookHandler(&fakeBitrixIncomingService{})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/bitrix/webhook", strings.NewReader("event=ONCRMDEALADD"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался код %d, получен %d", http.StatusBadRequest, rec.Code)
	}
}

func TestBitrixWebhookHandler_Unauthorized(t *testing.T) {
	h := NewBitrixWebhookHandler(&fakeBitrixIncomingService{handleErr: services.ErrBitrixWebhookUnauthorized})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/bitrix/webhook", strings.NewReader("event=ONCRMDEALADD"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ожидался код %d, получен %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestBitrixWebhookHandler_Accepted(t *testing.T) {
	h := NewBitrixWebhookHandler(&fakeBitrixIncomingService{})
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/integrations/bitrix/webhook",
		strings.NewReader("event=ONCRMDEALADD&auth%5Bapplication_token%5D=test"),
	)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("ожидался код %d, получен %d", http.StatusAccepted, rec.Code)
	}
}

func TestBitrixWebhookHandler_InternalError(t *testing.T) {
	h := NewBitrixWebhookHandler(&fakeBitrixIncomingService{handleErr: errors.New("ошибка")})
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/bitrix/webhook", strings.NewReader("event=ONCRMDEALADD"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()

	h.HandleWebhook(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ожидался код %d, получен %d", http.StatusInternalServerError, rec.Code)
	}
}
