package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"etalon-server/internal/domain/models"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestAgentDiagnosticsHandler_ВозвращаетДеталиРегистрацииИСнапшоты(t *testing.T) {
	db := setupAgentDiagnosticsDB(t)
	lastRegistrationAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	lastObservedAt := time.Date(2026, 3, 21, 10, 5, 0, 0, time.UTC)
	workstationID := "ws-100"
	agentUUID := "agent-phase1"

	require.NoError(t, db.Create(&models.Agent{
		UUID:                    agentUUID,
		Type:                    "sssruner",
		Status:                  models.StatusPendingOwner,
		Hostname:                "cash-01",
		WorkstationID:           &workstationID,
		LastObservedAt:          &lastObservedAt,
		LastHeartbeat:           time.Date(2026, 3, 21, 10, 6, 0, 0, time.UTC),
		LastRegistrationAt:      &lastRegistrationAt,
		LastRegistrationStatus:  models.AgentRegistrationStatusSuccess,
		MachineFingerprint:      "fp-123",
		RegistrationPayload:     datatypes.JSON([]byte(`{"agent_uuid":"agent-phase1","hostname":"cash-01"}`)),
		RegistrationSystemInfo:  datatypes.JSON([]byte(`{"os":"windows","arch":"amd64"}`)),
		LatestInventorySnapshot: datatypes.JSON([]byte(`{"hostname":"cash-01","os":"windows"}`)),
		LatestAdapterStatuses:   datatypes.JSON([]byte(`[{"adapter_id":"atol","status":"ready"}]`)),
	}).Error)
	require.NoError(t, db.Create(&models.AgentRegistrationAttempt{
		AgentUUID:          &agentUUID,
		Status:             models.AgentRegistrationStatusSuccess,
		MachineFingerprint: "fp-123",
		Payload:            datatypes.JSON([]byte(`{"agent_uuid":"agent-phase1"}`)),
		SystemInfo:         datatypes.JSON([]byte(`{"os":"windows"}`)),
		RemoteAddr:         "10.0.0.10",
		CreatedAt:          lastRegistrationAt,
	}).Error)

	handler := NewAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-diagnostics/agent-phase1", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Agent struct {
				UUID                   string `json:"uuid"`
				LastRegistrationStatus string `json:"last_registration_status"`
				MachineFingerprint     string `json:"machine_fingerprint"`
			} `json:"agent"`
			RegistrationPayload map[string]any   `json:"registration_payload"`
			LatestInventory     map[string]any   `json:"latest_inventory"`
			LatestAdapterStatus []map[string]any `json:"latest_adapter_statuses"`
			RecentRegistrations []struct {
				Status     string `json:"status"`
				RemoteAddr string `json:"remote_addr"`
			} `json:"recent_registrations"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "success", body.Status)
	require.Equal(t, agentUUID, body.Data.Agent.UUID)
	require.Equal(t, models.AgentRegistrationStatusSuccess, body.Data.Agent.LastRegistrationStatus)
	require.Equal(t, "fp-123", body.Data.Agent.MachineFingerprint)
	require.Equal(t, "agent-phase1", body.Data.RegistrationPayload["agent_uuid"])
	require.Equal(t, "cash-01", body.Data.LatestInventory["hostname"])
	require.Len(t, body.Data.LatestAdapterStatus, 1)
	require.Equal(t, "atol", body.Data.LatestAdapterStatus[0]["adapter_id"])
	require.Len(t, body.Data.RecentRegistrations, 1)
	require.Equal(t, "10.0.0.10", body.Data.RecentRegistrations[0].RemoteAddr)
}

func TestAgentDiagnosticsHandler_ListAgentsФильтруетПоСтатусуРегистрации(t *testing.T) {
	db := setupAgentDiagnosticsDB(t)
	require.NoError(t, db.Create(&models.Agent{
		UUID:                   "agent-success",
		Type:                   "sssruner",
		Hostname:               "cash-success",
		LastRegistrationStatus: models.AgentRegistrationStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&models.Agent{
		UUID:                   "agent-failed",
		Type:                   "sssruner",
		Hostname:               "cash-failed",
		LastRegistrationStatus: models.AgentRegistrationStatusUnauthorized,
	}).Error)

	handler := NewAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-diagnostics?registration_status=unauthorized&term=failed", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "agent-failed")
	require.NotContains(t, rec.Body.String(), "agent-success")
}

func setupAgentDiagnosticsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentRegistrationAttempt{}))
	return db
}
