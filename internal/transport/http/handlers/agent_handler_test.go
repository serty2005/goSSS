package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/services"
	"etalon-server/internal/services/agentauth"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type stubAgentService struct {
	lastUUID string
	lastData *api.AgentDataDTO
	response *api.AgentHeartbeatResponseDTO
	err      error
}

func (s *stubAgentService) RegisterAgent(_ context.Context, _ *api.RegistrationRequestDTO) (*models.Agent, error) {
	return nil, nil
}

func (s *stubAgentService) ProcessData(_ context.Context, agentUUID string, data *api.AgentDataDTO) (*api.AgentHeartbeatResponseDTO, error) {
	s.lastUUID = agentUUID
	if data != nil {
		copyData := *data
		s.lastData = &copyData
	}
	if s.response != nil {
		return s.response, s.err
	}
	return &api.AgentHeartbeatResponseDTO{Status: "ok"}, s.err
}

func (s *stubAgentService) GetAgentConfig(_ context.Context, _ string) (*api.AgentConfigDTO, error) {
	return &api.AgentConfigDTO{}, nil
}

type stubAgentAuthService struct {
	validateErr      error
	recordedStatuses []agentauth.RegistrationAttemptStatus
	recordedErrors   []string
	recordedUUIDs    []string
	registerResp     *api.AgentRegistrationResponseDTO
	registerErr      error
	refreshResp      *api.AgentTokenRefreshResponseDTO
	refreshErr       error
}

func (s *stubAgentAuthService) RegisterAndIssueTokens(_ context.Context, _ *api.RegistrationRequestDTO, _ agentauth.RegistrationAttemptMeta) (*api.AgentRegistrationResponseDTO, error) {
	return s.registerResp, s.registerErr
}

func (s *stubAgentAuthService) RecordRegistrationAttempt(_ context.Context, req *api.RegistrationRequestDTO, _ agentauth.RegistrationAttemptMeta, status agentauth.RegistrationAttemptStatus, errorText string) error {
	s.recordedStatuses = append(s.recordedStatuses, status)
	s.recordedErrors = append(s.recordedErrors, errorText)
	if req != nil {
		s.recordedUUIDs = append(s.recordedUUIDs, req.AgentUUID)
	} else {
		s.recordedUUIDs = append(s.recordedUUIDs, "")
	}
	return nil
}

func (s *stubAgentAuthService) RefreshTokens(_ context.Context, _ *api.AgentTokenRefreshRequestDTO) (*api.AgentTokenRefreshResponseDTO, error) {
	return s.refreshResp, s.refreshErr
}

func (s *stubAgentAuthService) ValidateAccessToken(_ context.Context, _, _ string) error {
	return s.validateErr
}

var _ services.AgentService = (*stubAgentService)(nil)
var _ agentauth.Service = (*stubAgentAuthService)(nil)

func TestPostAgentData_ПринимаетInventoryИAdapterStatuses(t *testing.T) {
	service := &stubAgentService{
		response: &api.AgentHeartbeatResponseDTO{Status: "ok"},
	}
	handler := NewAgentHandler(service, &stubAgentAuthService{}, "test-key")

	router := chi.NewRouter()
	router.Route("/api/agents", handler.RegisterRoutes)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-1/data", strings.NewReader(`{
		"hostname": "ws-phase0",
		"agent_type": "sssruner",
		"inventory": {
			"collected_at": "2026-03-20T10:00:00Z",
			"hostname": "ws-phase0",
			"os": "windows",
			"arch": "amd64"
		},
		"adapter_statuses": [
			{"adapter_id": "atol", "status": "ready", "version": "1.0.0"}
		]
	}`))
	req.Header.Set("Authorization", "Bearer access-token")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "agent-1", service.lastUUID)
	require.NotNil(t, service.lastData)
	require.NotNil(t, service.lastData.Inventory)
	require.Equal(t, "ws-phase0", service.lastData.Inventory.Hostname)
	require.Len(t, service.lastData.AdapterStatuses, 1)
	require.Equal(t, "atol", service.lastData.AdapterStatuses[0].AdapterID)

	var responseBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &responseBody))
	require.Equal(t, "ok", responseBody["status"])
	_, hasEnvelope := responseBody["data"]
	require.False(t, hasEnvelope, "heartbeat active-agent не должен возвращать API-конверт")
}

func TestPostAgentData_LegacyPayloadБезНовыхПолейПроходит(t *testing.T) {
	service := &stubAgentService{
		response: &api.AgentHeartbeatResponseDTO{Status: "ok"},
	}
	handler := NewAgentHandler(service, &stubAgentAuthService{}, "test-key")

	router := chi.NewRouter()
	router.Route("/api/agents", handler.RegisterRoutes)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/agent-legacy/data", strings.NewReader(`{
		"hostname": "legacy-host",
		"serialNumber": "SN-001",
		"fn_serial": "FN-001"
	}`))
	req.Header.Set("Authorization", "Bearer access-token")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, service.lastData)
	require.Equal(t, "legacy-host", service.lastData.Hostname)
	require.Equal(t, "SN-001", service.lastData.SerialNumber)
	require.Nil(t, service.lastData.Inventory)
	require.Empty(t, service.lastData.AdapterStatuses)
}

func TestHandleSubmitJSON_LegacyEndpointНеЛомается(t *testing.T) {
	service := &stubAgentService{
		response: &api.AgentHeartbeatResponseDTO{Status: "ok"},
	}
	handler := NewAgentHandler(service, &stubAgentAuthService{}, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/api/submit_json", strings.NewReader(`{
		"uuid": "legacy-agent",
		"hostname": "cash-1",
		"serialNumber": "SN-100",
		"RNM": "RNM-100"
	}`))
	req.Header.Set("X-API-Key", "test-key")

	rec := httptest.NewRecorder()
	handler.HandleSubmitJSON(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "legacy-agent", service.lastUUID)
	require.NotNil(t, service.lastData)
	require.Equal(t, "getad", service.lastData.AgentType)
	require.Equal(t, "SN-100", service.lastData.SerialNumber)

	var responseBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &responseBody))
	require.Equal(t, "success", responseBody["status"])
	responseData, ok := responseBody["data"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ok", responseData["status"])
}

func TestRegisterAgent_401БезAuthorizationСохраняетПричинуДиагностики(t *testing.T) {
	authService := &stubAgentAuthService{}
	handler := NewAgentHandler(&stubAgentService{}, authService, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/api/agents/register", strings.NewReader(`{
		"agent_uuid": "agent-auth-missing",
		"hostname": "ws-01"
	}`))
	rec := httptest.NewRecorder()

	handler.registerAgent(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, []agentauth.RegistrationAttemptStatus{agentauth.RegistrationAttemptStatusUnauthorized}, authService.recordedStatuses)
	require.Equal(t, []string{"Отсутствует заголовок Authorization"}, authService.recordedErrors)
	require.Equal(t, []string{"agent-auth-missing"}, authService.recordedUUIDs)
}

func TestRegisterAgent_400ПриБитомJSONСохраняетПопытку(t *testing.T) {
	authService := &stubAgentAuthService{}
	handler := NewAgentHandler(&stubAgentService{}, authService, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/api/agents/register", strings.NewReader(`{"agent_uuid":`))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	handler.registerAgent(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, []agentauth.RegistrationAttemptStatus{agentauth.RegistrationAttemptStatusInvalidRequest}, authService.recordedStatuses)
	require.Equal(t, []string{"Неверный формат тела запроса"}, authService.recordedErrors)
	require.Equal(t, []string{""}, authService.recordedUUIDs)
}

func TestRegisterAgent_УспешныйОтветВозвращаетсяБезAPIEnvelope(t *testing.T) {
	authService := &stubAgentAuthService{
		registerResp: &api.AgentRegistrationResponseDTO{
			Status:                "ok",
			AgentUUID:             "agent-raw-register",
			AccessToken:           "access-token",
			RefreshToken:          "refresh-token",
			AccessTokenExpiresAt:  time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC),
			RefreshTokenExpiresAt: time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC),
		},
	}
	handler := NewAgentHandler(&stubAgentService{}, authService, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/api/agents/register", strings.NewReader(`{
		"agent_uuid": "agent-raw-register",
		"hostname": "ws-raw",
		"agent_version": "0.1.0"
	}`))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()

	handler.registerAgent(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var responseBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &responseBody))
	require.Equal(t, "ok", responseBody["status"])
	require.Equal(t, "agent-raw-register", responseBody["agent_uuid"])
	require.Equal(t, "access-token", responseBody["access_token"])
	_, hasEnvelope := responseBody["data"]
	require.False(t, hasEnvelope, "bootstrap-регистрация не должна возвращать API-конверт")
}

func TestRefreshAgentToken_УспешныйОтветВозвращаетсяБезAPIEnvelope(t *testing.T) {
	authService := &stubAgentAuthService{
		refreshResp: &api.AgentTokenRefreshResponseDTO{
			Status:                "ok",
			AgentUUID:             "agent-refresh",
			AccessToken:           "new-access-token",
			RefreshToken:          "new-refresh-token",
			AccessTokenExpiresAt:  time.Date(2026, 3, 21, 13, 0, 0, 0, time.UTC),
			RefreshTokenExpiresAt: time.Date(2026, 4, 21, 13, 0, 0, 0, time.UTC),
		},
	}
	handler := NewAgentHandler(&stubAgentService{}, authService, "test-key")

	req := httptest.NewRequest(http.MethodPost, "/api/agents/auth/refresh", strings.NewReader(`{
		"agent_uuid": "agent-refresh",
		"refresh_token": "old-refresh-token"
	}`))
	rec := httptest.NewRecorder()

	handler.refreshAgentToken(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var responseBody map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &responseBody))
	require.Equal(t, "ok", responseBody["status"])
	require.Equal(t, "new-access-token", responseBody["access_token"])
	_, hasEnvelope := responseBody["data"]
	require.False(t, hasEnvelope, "refresh токенов не должен возвращать API-конверт")
}
