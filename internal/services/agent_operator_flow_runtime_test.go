package services

import (
	"encoding/json"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestAgentOperatorFlowService_SaveAdapterSelectionСохраняетRuntimeProfiles(t *testing.T) {
	ctx := t.Context()
	db := setupAgentRuntimeFlowDB(t)
	seedRuntimeFlowAdapterRelease(t, db, "fiscal-atol")
	require.NoError(t, db.Create(&models.Agent{
		UUID:     "agent-runtime-save",
		Type:     "sssruner",
		Status:   models.StatusActive,
		Hostname: "cash-save",
	}).Error)

	service := NewAgentOperatorFlowService(db)
	err := service.SaveAdapterSelection(ctx, "agent-runtime-save", api.SaveAgentAdapterSelectionRequestDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
		RuntimeProfiles: []api.AgentAdapterRuntimeProfileDTO{
			{
				AdapterID:      "fiscal-atol",
				Command:        "run",
				Operation:      "collect",
				TimeoutSeconds: 60,
				Devices: []api.AgentAdapterRuntimeDeviceDTO{
					{
						ConnectionType: "tcp",
						Address:        "10.25.1.22:5555",
					},
				},
				Schedule: api.AgentAdapterRuntimeScheduleDTO{
					Enabled:         true,
					IntervalSeconds: 900,
				},
			},
		},
	}, "user-1")
	require.NoError(t, err)

	var stored models.Agent
	require.NoError(t, db.WithContext(ctx).Where("uuid = ?", "agent-runtime-save").First(&stored).Error)

	var config api.AgentConfigDTO
	require.NoError(t, json.Unmarshal(stored.Config, &config))
	require.Equal(t, []string{"fiscal-atol"}, config.SelectedAdapterIDs)
	require.Len(t, config.AdapterRuntimeProfiles, 1)
	require.Equal(t, "fiscal-atol", config.AdapterRuntimeProfiles[0].AdapterID)
	require.Equal(t, "run", config.AdapterRuntimeProfiles[0].Command)
	require.Equal(t, "collect", config.AdapterRuntimeProfiles[0].Operation)
	require.Equal(t, 60, config.AdapterRuntimeProfiles[0].TimeoutSeconds)
	require.Len(t, config.AdapterRuntimeProfiles[0].Devices, 1)
	require.Equal(t, "tcp", config.AdapterRuntimeProfiles[0].Devices[0].ConnectionType)
	require.Equal(t, "10.25.1.22:5555", config.AdapterRuntimeProfiles[0].Devices[0].Address)
	require.True(t, config.AdapterRuntimeProfiles[0].Schedule.Enabled)
	require.Equal(t, 900, config.AdapterRuntimeProfiles[0].Schedule.IntervalSeconds)
}

func TestBuildAdapterRunCommandPayload_СтроитPayloadИзМинимальногоTCPВвода(t *testing.T) {
	profile := api.AgentAdapterRuntimeProfileDTO{
		AdapterID: "fiscal-atol",
		Devices: []api.AgentAdapterRuntimeDeviceDTO{
			{
				ConnectionType: "tcp",
				Address:        "10.25.1.22:5555",
			},
		},
	}

	payload, err := buildAdapterRunCommandPayload(profile)
	require.NoError(t, err)
	require.Equal(t, "fiscal-atol", payload.AdapterID)
	require.Equal(t, "run", payload.Command)
	require.Equal(t, "collect", payload.Operation)

	devices, ok := payload.DeviceParams["devices"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, devices, 1)
	require.Equal(t, "tcp", devices[0]["connection_type"])
	require.Equal(t, "tcp", devices[0]["transport"])
	require.Equal(t, "10.25.1.22", devices[0]["ip"])
	require.Equal(t, 5555, devices[0]["port"])
}

func TestBuildAdapterRunCommandPayload_СтроитPayloadИзМинимальногоCOMВвода(t *testing.T) {
	profile := api.AgentAdapterRuntimeProfileDTO{
		AdapterID: "fiscal-shtrih",
		Devices: []api.AgentAdapterRuntimeDeviceDTO{
			{
				ConnectionType: "com",
				Address:        "com7",
			},
		},
	}

	payload, err := buildAdapterRunCommandPayload(profile)
	require.NoError(t, err)
	require.Equal(t, "fiscal-shtrih", payload.AdapterID)

	devices, ok := payload.DeviceParams["devices"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, devices, 1)
	require.Equal(t, "com", devices[0]["connection_type"])
	require.Equal(t, "com", devices[0]["transport"])
	require.Equal(t, "COM7", devices[0]["com_port"])
}

func TestAgentOperatorFlowService_EnqueueAdapterRunСоздаетКомандуИзСохраненногоПрофиля(t *testing.T) {
	ctx := t.Context()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

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
						Address:        "10.25.1.22:5555",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.Agent{
		UUID:     "agent-enqueue",
		Type:     "sssruner",
		Status:   models.StatusActive,
		Hostname: "cash-enqueue",
		Config:   datatypes.JSON(configRaw),
	}).Error)

	err = service.EnqueueAdapterRun(ctx, "agent-enqueue", api.EnqueueAgentAdapterRunRequestDTO{
		AdapterID: "fiscal-atol",
	}, "user-2")
	require.NoError(t, err)

	var commands []models.AgentCommand
	require.NoError(t, db.WithContext(ctx).Where("agent_uuid = ?", "agent-enqueue").Find(&commands).Error)
	require.Len(t, commands, 1)
	require.Equal(t, "run_adapter", commands[0].Type)
	require.Equal(t, "new", commands[0].Status)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(commands[0].Payload, &payload))
	require.Equal(t, "fiscal-atol", payload["adapter_id"])
	require.Equal(t, "run", payload["command"])
	require.Equal(t, "collect", payload["operation"])
	require.Equal(t, float64(45), payload["timeout_seconds"])

	deviceParams, ok := payload["device_params"].(map[string]any)
	require.True(t, ok)
	devices, ok := deviceParams["devices"].([]any)
	require.True(t, ok)
	require.Len(t, devices, 1)
	firstDevice, ok := devices[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "tcp", firstDevice["transport"])
	require.Equal(t, "tcp", firstDevice["connection_type"])
	require.Equal(t, "10.25.1.22", firstDevice["ip"])
	require.Equal(t, float64(5555), firstDevice["port"])
}

func TestAgentOperatorFlowService_EnsureScheduledAdapterRunsСоздаетКомандуКогдаИнтервалИстек(t *testing.T) {
	ctx := t.Context()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

	lastRunAt := time.Now().UTC().Add(-20 * time.Minute)
	configRaw, err := json.Marshal(api.AgentConfigDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
		AdapterRuntimeProfiles: []api.AgentAdapterRuntimeProfileDTO{
			{
				AdapterID: "fiscal-atol",
				Devices: []api.AgentAdapterRuntimeDeviceDTO{
					{
						ConnectionType: "tcp",
						Address:        "10.25.1.22:5555",
					},
				},
				Schedule: api.AgentAdapterRuntimeScheduleDTO{
					Enabled:         true,
					IntervalSeconds: 300,
				},
			},
		},
	})
	require.NoError(t, err)
	statusRaw, err := json.Marshal([]api.AdapterStatusDTO{{
		AdapterID: "fiscal-atol",
		LastRunAt: &lastRunAt,
	}})
	require.NoError(t, err)
	agent := &models.Agent{
		UUID:                  "agent-scheduled",
		Type:                  "sssruner",
		Status:                models.StatusActive,
		Hostname:              "cash-scheduled",
		Config:                datatypes.JSON(configRaw),
		LatestAdapterStatuses: datatypes.JSON(statusRaw),
	}
	require.NoError(t, db.Create(agent).Error)

	require.NoError(t, service.EnsureScheduledAdapterRuns(ctx, agent))

	var commands []models.AgentCommand
	require.NoError(t, db.WithContext(ctx).Where("agent_uuid = ?", "agent-scheduled").Find(&commands).Error)
	require.Len(t, commands, 1)
	require.Equal(t, "run_adapter", commands[0].Type)
}

func TestAgentOperatorFlowService_EnsureScheduledAdapterRunsПовторяетЗапускЕслиLastRunAtНеЗаполнен(t *testing.T) {
	ctx := t.Context()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

	configRaw, err := json.Marshal(api.AgentConfigDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
		AdapterRuntimeProfiles: []api.AgentAdapterRuntimeProfileDTO{
			{
				AdapterID: "fiscal-atol",
				Devices: []api.AgentAdapterRuntimeDeviceDTO{
					{
						ConnectionType: "tcp",
						Address:        "10.25.1.22:5555",
					},
				},
				Schedule: api.AgentAdapterRuntimeScheduleDTO{
					Enabled:         true,
					IntervalSeconds: 300,
				},
			},
		},
	})
	require.NoError(t, err)
	statusRaw, err := json.Marshal([]api.AdapterStatusDTO{{
		AdapterID: "fiscal-atol",
		RunStatus: "failed",
		LastError: "локальный бинарник адаптера отсутствует",
	}})
	require.NoError(t, err)
	agent := &models.Agent{
		UUID:                  "agent-scheduled-retry",
		Type:                  "sssruner",
		Status:                models.StatusActive,
		Hostname:              "cash-scheduled-retry",
		Config:                datatypes.JSON(configRaw),
		LatestAdapterStatuses: datatypes.JSON(statusRaw),
	}
	require.NoError(t, db.Create(agent).Error)

	require.NoError(t, service.EnsureScheduledAdapterRuns(ctx, agent))

	var commands []models.AgentCommand
	require.NoError(t, db.WithContext(ctx).Where("agent_uuid = ?", "agent-scheduled-retry").Find(&commands).Error)
	require.Len(t, commands, 1)
	require.Equal(t, "run_adapter", commands[0].Type)
	require.Equal(t, "new", commands[0].Status)
}

func TestAgentOperatorFlowService_EnsureScheduledAdapterRunsНеДублируетPendingCommand(t *testing.T) {
	ctx := t.Context()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

	lastRunAt := time.Now().UTC().Add(-20 * time.Minute)
	configRaw, err := json.Marshal(api.AgentConfigDTO{
		SelectedAdapterIDs: []string{"fiscal-atol"},
		AdapterRuntimeProfiles: []api.AgentAdapterRuntimeProfileDTO{
			{
				AdapterID: "fiscal-atol",
				Devices: []api.AgentAdapterRuntimeDeviceDTO{
					{
						ConnectionType: "tcp",
						IP:             "10.25.1.22",
						Port:           5555,
					},
				},
				Schedule: api.AgentAdapterRuntimeScheduleDTO{
					Enabled:         true,
					IntervalSeconds: 300,
				},
			},
		},
	})
	require.NoError(t, err)
	statusRaw, err := json.Marshal([]api.AdapterStatusDTO{{
		AdapterID: "fiscal-atol",
		LastRunAt: &lastRunAt,
	}})
	require.NoError(t, err)
	agent := &models.Agent{
		UUID:                  "agent-scheduled-pending",
		Type:                  "sssruner",
		Status:                models.StatusActive,
		Hostname:              "cash-scheduled",
		Config:                datatypes.JSON(configRaw),
		LatestAdapterStatuses: datatypes.JSON(statusRaw),
	}
	require.NoError(t, db.Create(agent).Error)

	pendingPayload, err := json.Marshal(api.AgentAdapterRunCommandPayloadDTO{
		AdapterID:      "fiscal-atol",
		Command:        "run",
		Operation:      "collect",
		TimeoutSeconds: 45,
		DeviceParams: map[string]any{
			"devices": []map[string]any{{
				"transport": "tcp",
				"ip":        "10.25.1.22",
				"port":      5555,
			}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.AgentCommand{
		AgentUUID: "agent-scheduled-pending",
		Type:      "run_adapter",
		Status:    "new",
		Payload:   datatypes.JSON(pendingPayload),
	}).Error)

	require.NoError(t, service.EnsureScheduledAdapterRuns(ctx, agent))

	var count int64
	require.NoError(t, db.WithContext(ctx).Model(&models.AgentCommand{}).Where("agent_uuid = ?", "agent-scheduled-pending").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func setupAgentRuntimeFlowDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Agent{},
		&models.AgentCommand{},
		&models.AgentAdapterRelease{},
		&models.AgentAdapterChannel{},
	))
	return db
}

func seedRuntimeFlowAdapterRelease(t *testing.T, db *gorm.DB, adapterID string) {
	t.Helper()

	release := models.AgentAdapterRelease{
		AdapterID:       adapterID,
		Title:           adapterID,
		Description:     "test release",
		Published:       true,
		Version:         "1.0.0",
		AdapterType:     adapterID,
		TargetOS:        "windows",
		TargetArch:      "amd64",
		ProtocolVersion: "1",
		DownloadURL:     "https://example.test/" + adapterID + ".exe",
		SHA256:          "abc123",
		SourceKey:       "adapters/" + adapterID + "/1.0.0/windows/amd64/" + adapterID + ".exe",
		FileName:        adapterID + ".exe",
	}
	require.NoError(t, db.Create(&release).Error)
	for _, channel := range []string{"stable", "latest"} {
		require.NoError(t, db.Create(&models.AgentAdapterChannel{
			AdapterID: adapterID,
			Channel:   channel,
			ReleaseID: release.ID,
		}).Error)
	}
}
