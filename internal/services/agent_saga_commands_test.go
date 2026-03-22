package services

import (
	"context"
	"encoding/json"
	"testing"

	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestBuildAgentSelfUpdateSagaPayloadСоздаетНормализованныйPayload(t *testing.T) {
	payload, err := BuildAgentSelfUpdateSagaPayload("saga-update-1", api.AgentSelfUpdateSagaInputDTO{
		TargetVersion: " 2.0.0 ",
		DownloadURL:   " https://example.test/agent.exe ",
		SHA256:        "ABCDEF",
	})
	require.NoError(t, err)
	require.Equal(t, "saga-update-1", payload.SagaID)
	require.Equal(t, "agent_self_update", payload.SagaType)
	require.Equal(t, "saga-update-1", payload.RequestID)
	require.Equal(t, 300, payload.TimeoutSeconds)
	require.Equal(t, "saga-update-1", payload.IdempotencyHint.Key)

	var input map[string]any
	require.NoError(t, json.Unmarshal(payload.Input, &input))
	require.Equal(t, "2.0.0", input["target_version"])
	require.Equal(t, "https://example.test/agent.exe", input["download_url"])
	require.Equal(t, "abcdef", input["sha256"])
	require.Equal(t, "immediate", input["restart_policy"])
}

func TestAgentOperatorFlowServiceEnqueueSagaRunСоздаетКоманду(t *testing.T) {
	ctx := context.Background()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

	require.NoError(t, db.Create(&models.Agent{
		UUID:     "agent-saga-enqueue",
		Type:     "sssruner",
		Status:   models.StatusActive,
		Hostname: "cash-saga",
	}).Error)

	payload, err := BuildAgentSelfUpdateSagaPayload("saga-update-1", api.AgentSelfUpdateSagaInputDTO{
		TargetVersion: "2.0.0",
		DownloadURL:   "https://example.test/agent.exe",
	})
	require.NoError(t, err)

	err = service.EnqueueSagaRun(ctx, "agent-saga-enqueue", api.EnqueueAgentSagaRunRequestDTO{
		Payload: payload,
	}, "user-1")
	require.NoError(t, err)

	var commands []models.AgentCommand
	require.NoError(t, db.WithContext(ctx).Where("agent_uuid = ?", "agent-saga-enqueue").Find(&commands).Error)
	require.Len(t, commands, 1)
	require.Equal(t, "saga_run", commands[0].Type)
	require.Equal(t, "new", commands[0].Status)

	var stored api.AgentSagaRunCommandPayloadDTO
	require.NoError(t, json.Unmarshal(commands[0].Payload, &stored))
	require.Equal(t, "saga-update-1", stored.SagaID)
	require.Equal(t, "agent_self_update", stored.SagaType)
	require.Equal(t, "saga-update-1", stored.IdempotencyHint.Key)
}

func TestAgentOperatorFlowServiceEnqueueSagaRunНеДублируетPendingSaga(t *testing.T) {
	ctx := context.Background()
	db := setupAgentRuntimeFlowDB(t)
	service := NewAgentOperatorFlowService(db)

	require.NoError(t, db.Create(&models.Agent{
		UUID:     "agent-saga-pending",
		Type:     "sssruner",
		Status:   models.StatusActive,
		Hostname: "cash-saga-pending",
	}).Error)

	payload, err := BuildAgentSelfUpdateSagaPayload("saga-update-1", api.AgentSelfUpdateSagaInputDTO{
		TargetVersion: "2.0.0",
		DownloadURL:   "https://example.test/agent.exe",
	})
	require.NoError(t, err)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, db.Create(&models.AgentCommand{
		AgentUUID: "agent-saga-pending",
		Type:      "saga_run",
		Status:    "new",
		Payload:   datatypes.JSON(raw),
	}).Error)

	err = service.EnqueueSagaRun(ctx, "agent-saga-pending", api.EnqueueAgentSagaRunRequestDTO{
		Payload: payload,
	}, "user-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "уже есть незавершённая команда")
}
