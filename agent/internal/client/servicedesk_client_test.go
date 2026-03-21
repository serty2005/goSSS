package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"etalon-agent/internal/protocol"
)

func TestServiceDeskClientRegister_DecodesEnvelopeResponse(t *testing.T) {
	t.Parallel()

	accessExpiresAt := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	refreshExpiresAt := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("ожидался POST, получено %s", r.Method)
		}
		if r.URL.Path != "/api/agents/register" {
			t.Fatalf("ожидался путь /api/agents/register, получено %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer bootstrap-key" {
			t.Fatalf("ожидался bootstrap Authorization, получено %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"status":                   "ok",
				"agent_uuid":               "agent-1",
				"access_token":             "access-token",
				"access_token_expires_at":  accessExpiresAt.Format(time.RFC3339),
				"refresh_token":            "refresh-token",
				"refresh_token_expires_at": refreshExpiresAt.Format(time.RFC3339),
			},
		})
	}))
	defer server.Close()

	client := NewServiceDeskClient(server.URL)
	resp, err := client.Register(context.Background(), "bootstrap-key", protocol.RegistrationRequestDTO{
		AgentUUID: "agent-1",
	})
	if err != nil {
		t.Fatalf("register завершился ошибкой: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("ожидался статус ok, получено %q", resp.Status)
	}
	if resp.AccessToken != "access-token" || resp.RefreshToken != "refresh-token" {
		t.Fatalf("получены неожиданные токены: %+v", resp)
	}
	if !resp.AccessTokenExpiresAt.Equal(accessExpiresAt) {
		t.Fatalf("ожидался срок жизни access token %s, получено %s", accessExpiresAt, resp.AccessTokenExpiresAt)
	}
	if !resp.RefreshTokenExpiresAt.Equal(refreshExpiresAt) {
		t.Fatalf("ожидался срок жизни refresh token %s, получено %s", refreshExpiresAt, resp.RefreshTokenExpiresAt)
	}
}

func TestServiceDeskClientRegister_DecodesPendingApprovalResponseWithoutTokens(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("ожидался POST, получено %s", r.Method)
		}
		if r.URL.Path != "/api/agents/register" {
			t.Fatalf("ожидался путь /api/agents/register, получено %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "pending_approval",
			"message":    "Регистрация ожидает подтверждения оператором",
			"agent_uuid": "agent-1",
		})
	}))
	defer server.Close()

	client := NewServiceDeskClient(server.URL)
	resp, err := client.Register(context.Background(), "bootstrap-key", protocol.RegistrationRequestDTO{
		AgentUUID: "agent-1",
	})
	if err != nil {
		t.Fatalf("register завершился ошибкой: %v", err)
	}
	if resp.Status != "pending_approval" {
		t.Fatalf("ожидался статус pending_approval, получено %q", resp.Status)
	}
	if resp.Message != "Регистрация ожидает подтверждения оператором" {
		t.Fatalf("ожидалось сообщение pending approval, получено %q", resp.Message)
	}
	if resp.AccessToken != "" || resp.RefreshToken != "" {
		t.Fatalf("до подтверждения регистрации токены не должны приходить: %+v", resp)
	}
}

func TestServiceDeskClientRefreshTokens_DecodesEnvelopeResponse(t *testing.T) {
	t.Parallel()

	accessExpiresAt := time.Date(2026, 3, 21, 13, 0, 0, 0, time.UTC)
	refreshExpiresAt := time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("ожидался POST, получено %s", r.Method)
		}
		if r.URL.Path != "/api/agents/auth/refresh" {
			t.Fatalf("ожидался путь /api/agents/auth/refresh, получено %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"status":                   "ok",
				"agent_uuid":               "agent-1",
				"access_token":             "new-access-token",
				"access_token_expires_at":  accessExpiresAt.Format(time.RFC3339),
				"refresh_token":            "new-refresh-token",
				"refresh_token_expires_at": refreshExpiresAt.Format(time.RFC3339),
			},
		})
	}))
	defer server.Close()

	client := NewServiceDeskClient(server.URL)
	resp, err := client.RefreshTokens(context.Background(), protocol.AgentTokenRefreshRequestDTO{
		AgentUUID:    "agent-1",
		RefreshToken: "refresh-token",
	})
	if err != nil {
		t.Fatalf("refresh завершился ошибкой: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("ожидался статус ok, получено %q", resp.Status)
	}
	if resp.AccessToken != "new-access-token" || resp.RefreshToken != "new-refresh-token" {
		t.Fatalf("получены неожиданные токены: %+v", resp)
	}
}

func TestServiceDeskClientSendHeartbeat_DecodesRawResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("ожидался POST, получено %s", r.Method)
		}
		if r.URL.Path != "/api/agents/agent-1/data" {
			t.Fatalf("ожидался путь /api/agents/agent-1/data, получено %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer access-token" {
			t.Fatalf("ожидался access Authorization, получено %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":            "ok",
			"tasks":             []any{},
			"adapter_manifests": []any{},
		})
	}))
	defer server.Close()

	client := NewServiceDeskClient(server.URL)
	resp, err := client.SendHeartbeat(context.Background(), "agent-1", protocol.AgentDataDTO{
		Hostname: "ws-01",
	}, "access-token")
	if err != nil {
		t.Fatalf("heartbeat завершился ошибкой: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("ожидался статус ok, получено %q", resp.Status)
	}
}
