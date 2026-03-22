package handlers

import (
	"context"
	"encoding/json"
	dbpkg "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	infrarepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/models"
	"etalon-server/pkg/eventbus"

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

	handler := newAgentDiagnosticsHandler(db)
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
			OperatorFlow struct {
				AvailableAdapters []struct {
					AdapterID  string `json:"adapter_id"`
					Published  bool   `json:"published"`
					Selectable bool   `json:"selectable"`
				} `json:"available_adapters"`
				SelectedAdapterIDs []string `json:"selected_adapter_ids"`
			} `json:"operator_flow"`
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
	require.Len(t, body.Data.OperatorFlow.AvailableAdapters, 3)
	availableAdapterIDs := make([]string, 0, len(body.Data.OperatorFlow.AvailableAdapters))
	for _, item := range body.Data.OperatorFlow.AvailableAdapters {
		availableAdapterIDs = append(availableAdapterIDs, item.AdapterID)
	}
	require.Contains(t, availableAdapterIDs, "fiscal-atol")
	require.Empty(t, body.Data.OperatorFlow.SelectedAdapterIDs)
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

	handler := newAgentDiagnosticsHandler(db)
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

	handler := newAgentDiagnosticsHandler(db)
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

	handler := newAgentDiagnosticsHandler(db)
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

func TestAgentDiagnosticsHandler_SaveAdapterSelectionОбновляетConfigИВлияетНаHeartbeatResponse(t *testing.T) {
	ctx := t.Context()
	db := setupAgentDiagnosticsDB(t)
	agentUUID := "agent-profile-save"
	require.NoError(t, db.Create(&models.Agent{
		UUID:                    agentUUID,
		Type:                    "sssruner",
		Status:                  models.StatusActive,
		Hostname:                "cash-save",
		LatestInventorySnapshot: datatypes.JSON([]byte(`{"hostname":"cash-save","os":"windows","arch":"amd64","host_info":{"cash_server_url":"http://cash-01:8080"},"known_components":[{"key":"iiko-front","detected":true}]}`)),
	}).Error)

	handler := newAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	body := strings.NewReader(`{
		"selected_adapter_ids": ["fiscal-atol"],
		"runtime_profiles": [
			{
				"adapter_id": "fiscal-atol",
				"command": "run",
				"operation": "collect",
				"timeout_seconds": 45,
				"devices": [
					{
						"connection_type": "tcp",
						"ip": "10.25.1.22",
						"port": 5555,
						"model": "АТОЛ 22Ф"
					}
				],
				"schedule": {
					"enabled": true,
					"interval_seconds": 300
				}
			}
		]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/agent-diagnostics/"+agentUUID+"/adapter-selection", body)
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.UserIDContextKey, "user-77"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var stored models.Agent
	require.NoError(t, db.WithContext(ctx).Where("uuid = ?", agentUUID).First(&stored).Error)

	var config api.AgentConfigDTO
	require.NoError(t, json.Unmarshal(stored.Config, &config))
	require.Nil(t, config.MachineProfile)
	require.Equal(t, []string{"fiscal-atol"}, config.SelectedAdapterIDs)
	require.Empty(t, config.AdapterManifests)
	require.Len(t, config.AdapterRuntimeProfiles, 1)
	require.Equal(t, "fiscal-atol", config.AdapterRuntimeProfiles[0].AdapterID)
	require.Equal(t, "collect", config.AdapterRuntimeProfiles[0].Operation)
	require.Len(t, config.AdapterRuntimeProfiles[0].Devices, 1)
	require.Equal(t, "tcp", config.AdapterRuntimeProfiles[0].Devices[0].ConnectionType)
	require.True(t, config.AdapterRuntimeProfiles[0].Schedule.Enabled)
	require.Equal(t, 300, config.AdapterRuntimeProfiles[0].Schedule.IntervalSeconds)

	agentRepo := infrarepos.NewAgentRepo(db)
	bus := eventbus.NewInMemoryEventBus(16)
	operatorFlow := services.NewAgentOperatorFlowService(db)
	agentService := services.NewAgentService(logger.New("", "test", "error", true), agentRepo, nil, bus, operatorFlow)

	resp, err := agentService.ProcessData(ctx, agentUUID, &api.AgentDataDTO{
		AgentUUID:   agentUUID,
		AgentType:   "sssruner",
		Hostname:    "cash-save",
		CurrentTime: "2026-03-22T10:00:00Z",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.AdapterManifests)
	require.Len(t, *resp.AdapterManifests, 1)
	require.Equal(t, "fiscal-atol", (*resp.AdapterManifests)[0].AdapterID)
	require.Equal(t, "https://example.test/agents/adapters/fiscal-atol/releases/0.1.0-demo/windows/amd64/fiscal-atol-0.1.0-demo.exe", (*resp.AdapterManifests)[0].DownloadURL)
	require.Equal(t, "0.1.0-demo", (*resp.AdapterManifests)[0].Version)
	require.Equal(t, "fiscal-atol-0.1.0-demo.exe", (*resp.AdapterManifests)[0].FileName)
}

func TestAgentDiagnosticsHandler_SaveAdapterSelectionВалидируетPublishedCatalog(t *testing.T) {
	tests := []struct {
		name          string
		release       models.AgentAdapterRelease
		expectedError string
	}{
		{
			name: "неопубликованный адаптер",
			release: models.AgentAdapterRelease{
				AdapterID:       "draft-adapter",
				Title:           "Черновой адаптер",
				Description:     "Не должен выбираться",
				Published:       false,
				Version:         "0.1.0",
				AdapterType:     "draft-adapter",
				TargetOS:        "windows",
				TargetArch:      "amd64",
				ProtocolVersion: "1",
				DownloadURL:     "https://example.test/agents/adapters/draft-adapter/releases/0.1.0/windows/amd64/draft.exe",
				SHA256:          "abc",
				SourceKey:       "adapters/draft-adapter/releases/0.1.0/windows/amd64/draft.exe",
				FileName:        "draft.exe",
			},
			expectedError: "не опубликован",
		},
		{
			name: "неполный manifest",
			release: models.AgentAdapterRelease{
				AdapterID:       "broken-adapter",
				Title:           "Сломанный адаптер",
				Description:     "Не должен выбираться",
				Published:       true,
				Version:         "0.1.0",
				AdapterType:     "broken-adapter",
				TargetOS:        "windows",
				TargetArch:      "amd64",
				ProtocolVersion: "1",
				SHA256:          "abc",
				FileName:        "broken.exe",
			},
			expectedError: "manifest неполон",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupAgentDiagnosticsDB(t)
			agentUUID := "agent-invalid-selection-" + strings.ReplaceAll(tt.release.AdapterID, "_", "-")
			require.NoError(t, db.Create(&models.Agent{
				UUID:     agentUUID,
				Type:     "sssruner",
				Status:   models.StatusActive,
				Hostname: "cash-invalid",
			}).Error)
			seedAdapterReleaseChannels(t, db, tt.release)

			handler := newAgentDiagnosticsHandler(db)
			router := chi.NewRouter()
			handler.RegisterRoutes(router)

			body := strings.NewReader(`{"selected_adapter_ids":["` + tt.release.AdapterID + `"]}`)
			req := httptest.NewRequest(http.MethodPost, "/agent-diagnostics/"+agentUUID+"/adapter-selection", body)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Contains(t, rec.Body.String(), tt.expectedError)
		})
	}
}

func TestAgentDiagnosticsHandler_EnqueueAdapterRunСоздаетКоманду(t *testing.T) {
	db := setupAgentDiagnosticsDB(t)
	agentUUID := "agent-run-now"

	configRaw, err := json.Marshal(api.AgentConfigDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
		AdapterRuntimeProfiles: []api.AgentAdapterRuntimeProfileDTO{
			{
				AdapterID:      "fiscal-atol",
				Command:        "run",
				Operation:      "collect",
				TimeoutSeconds: 45,
				Devices: []api.AgentAdapterRuntimeDeviceDTO{
					{
						ConnectionType: "tcp",
						IP:             "10.25.1.22",
						Port:           5555,
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.Agent{
		UUID:     agentUUID,
		Type:     "sssruner",
		Status:   models.StatusActive,
		Hostname: "cash-run-now",
		Config:   datatypes.JSON(configRaw),
	}).Error)

	handler := newAgentDiagnosticsHandler(db)
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/agent-diagnostics/"+agentUUID+"/adapter-runs", strings.NewReader(`{"adapter_id":"fiscal-atol"}`))
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.UserIDContextKey, "user-88"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var commands []models.AgentCommand
	require.NoError(t, db.Where("agent_uuid = ?", agentUUID).Find(&commands).Error)
	require.Len(t, commands, 1)
	require.Equal(t, "run_adapter", commands[0].Type)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(commands[0].Payload, &payload))
	require.Equal(t, "fiscal-atol", payload["adapter_id"])
	require.Equal(t, "run", payload["command"])
	require.Equal(t, "collect", payload["operation"])
}

func newAgentDiagnosticsHandler(db *gorm.DB) *AgentDiagnosticsHandler {
	return NewAgentDiagnosticsHandler(db, services.NewAgentOperatorFlowService(db))
}

func setupAgentDiagnosticsDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Agent{},
		&models.AgentRegistrationAttempt{},
		&models.AgentCOMSignatureRule{},
		&models.PublishedAgentAdapter{},
		&models.AgentAdapterRelease{},
		&models.AgentAdapterChannel{},
		&models.AgentCommand{},
	))
	require.NoError(t, dbpkg.EnsureDefaultPublishedAgentAdapters(db))
	return db
}

func seedAdapterReleaseChannels(t *testing.T, db *gorm.DB, release models.AgentAdapterRelease) {
	t.Helper()

	require.NoError(t, db.Create(&release).Error)
	for _, channelName := range []string{"stable", "latest"} {
		require.NoError(t, db.Create(&models.AgentAdapterChannel{
			AdapterID: release.AdapterID,
			Channel:   channelName,
			ReleaseID: release.ID,
		}).Error)
	}
}
