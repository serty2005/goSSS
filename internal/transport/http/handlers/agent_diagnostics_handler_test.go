package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"etalon-server/internal/contextkeys"
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
				RegistrationApprovedBy string `json:"registration_approved_by"`
				MachineFingerprint     string `json:"machine_fingerprint"`
				HasLatestInventory     bool   `json:"has_latest_inventory"`
				HasAdapterStatuses     bool   `json:"has_adapter_statuses"`
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
	require.Empty(t, body.Data.Agent.RegistrationApprovedBy)
	require.Equal(t, "fp-123", body.Data.Agent.MachineFingerprint)
	require.True(t, body.Data.Agent.HasLatestInventory)
	require.True(t, body.Data.Agent.HasAdapterStatuses)
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

func TestAgentDiagnosticsHandler_ListAgentsОтдаётФлагиНаличияSnapshotов(t *testing.T) {
	db := setupAgentDiagnosticsDB(t)
	require.NoError(t, db.Create(&models.Agent{
		UUID:                    "agent-with-snapshots",
		Type:                    "sssruner",
		Hostname:                "cash-phase1",
		LastRegistrationStatus:  models.AgentRegistrationStatusSuccess,
		LatestInventorySnapshot: datatypes.JSON([]byte(`{"hostname":"cash-phase1"}`)),
		LatestAdapterStatuses:   datatypes.JSON([]byte(`[]`)),
	}).Error)

	handler := NewAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/agent-diagnostics?term=with-snapshots", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string `json:"status"`
		Data   []struct {
			UUID               string `json:"uuid"`
			HasLatestInventory bool   `json:"has_latest_inventory"`
			HasAdapterStatuses bool   `json:"has_adapter_statuses"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "success", body.Status)
	require.Len(t, body.Data, 1)
	require.Equal(t, "agent-with-snapshots", body.Data[0].UUID)
	require.True(t, body.Data[0].HasLatestInventory)
	require.True(t, body.Data[0].HasAdapterStatuses)
}

func TestAgentDiagnosticsHandler_ApproveRegistrationПодтверждаетОжидающегоАгента(t *testing.T) {
	db := setupAgentDiagnosticsDB(t)
	agentUUID := "agent-approval"
	require.NoError(t, db.Create(&models.Agent{
		UUID:                   agentUUID,
		Type:                   "sssruner",
		Status:                 models.StatusPendingRegistration,
		Hostname:               "cash-approval",
		LastRegistrationStatus: models.AgentRegistrationStatusPendingApproval,
		LastRegistrationError:  "Регистрация ожидает подтверждения оператором",
	}).Error)

	handler := NewAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/agent-diagnostics/agent-approval/approve-registration", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.UserIDContextKey, "user-42"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Agent struct {
				UUID                   string  `json:"uuid"`
				Status                 string  `json:"status"`
				LastRegistrationStatus string  `json:"last_registration_status"`
				LastRegistrationError  string  `json:"last_registration_error"`
				RegistrationApprovedBy string  `json:"registration_approved_by"`
				RegistrationApprovedAt *string `json:"registration_approved_at"`
			} `json:"agent"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "success", body.Status)
	require.Equal(t, agentUUID, body.Data.Agent.UUID)
	require.Equal(t, models.StatusPendingRegistration, body.Data.Agent.Status)
	require.Equal(t, models.AgentRegistrationStatusPendingApproval, body.Data.Agent.LastRegistrationStatus)
	require.Equal(t, "user-42", body.Data.Agent.RegistrationApprovedBy)
	require.NotNil(t, body.Data.Agent.RegistrationApprovedAt)
	require.Contains(t, body.Data.Agent.LastRegistrationError, "повторный запрос агента")

	var stored models.Agent
	require.NoError(t, db.Where("uuid = ?", agentUUID).First(&stored).Error)
	require.Equal(t, models.StatusPendingRegistration, stored.Status)
	require.NotNil(t, stored.RegistrationApprovedAt)
	require.Equal(t, "user-42", stored.RegistrationApprovedBy)
}

func setupAgentDiagnosticsDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Agent{}, &models.AgentRegistrationAttempt{}))
	return db
}
