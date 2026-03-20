package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	validateErr error
}

func (s *stubAgentAuthService) RegisterAndIssueTokens(_ context.Context, _ *api.RegistrationRequestDTO) (*api.AgentRegistrationResponseDTO, error) {
	return nil, nil
}

func (s *stubAgentAuthService) RefreshTokens(_ context.Context, _ *api.AgentTokenRefreshRequestDTO) (*api.AgentTokenRefreshResponseDTO, error) {
	return nil, nil
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
