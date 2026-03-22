package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/stretchr/testify/require"
)

func TestProcessDataСохраняетSagaResultВResultPayload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-saga-result",
			Type:          "sssruner",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
		},
		commands: []models.AgentCommand{{
			ID:        91,
			AgentUUID: "agent-saga-result",
			Type:      "saga_run",
			Status:    "sent",
			CreatedAt: time.Now().Add(-time.Minute),
		}},
	}
	bus := &fakeEventBus{}
	service := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	completedAt := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC)
	response, err := service.ProcessData(ctx, "agent-saga-result", &api.AgentDataDTO{
		AgentUUID:   "agent-saga-result",
		AgentType:   "sssruner",
		Hostname:    "cash-saga-result",
		CurrentTime: "2026-03-23T09:00:00Z",
		TaskResults: []api.AgentTaskResultDTO{{
			ID:   91,
			Type: "saga_run",
			TaskExecutionResultDTO: api.TaskExecutionResultDTO{
				Status:         "completed",
				SagaID:         "saga-update-1",
				SagaType:       "agent_self_update",
				RequestID:      "req-1",
				CorrelationID:  "corr-1",
				CompletedAt:    &completedAt,
				DurationMS:     4200,
				FinalResult:    json.RawMessage(`{"target_version":"2.0.0","downloaded_path":"C:/Temp/agent.exe"}`),
				Result:         json.RawMessage(`{"target_version":"2.0.0","downloaded_path":"C:/Temp/agent.exe"}`),
				Resumed:        true,
				IdempotencyKey: "saga-update-1",
				Steps: []api.SagaStepResultDTO{
					{ID: "preflight", Type: "runner.self_update_preflight", Status: "completed"},
					{ID: "apply_update", Type: "native.agent_self_update", Status: "completed"},
				},
				ExecutionLog: []api.SagaExecutionLogEntryDTO{
					{Timestamp: completedAt.Add(-2 * time.Second), Level: "info", Event: "saga.started", Message: "Старт саги"},
				},
			},
		}},
	})
	require.NoError(t, err)
	require.Equal(t, "ok", response.Status)
	require.Len(t, repo.completedResults, 1)
	require.Equal(t, uint(91), repo.completedResults[0].ID)
	require.Equal(t, "completed", repo.completedResults[0].Status)

	var stored map[string]any
	require.NoError(t, json.Unmarshal(repo.commands[0].ResultPayload, &stored))
	require.Equal(t, "completed", stored["status"])
	require.Equal(t, "saga-update-1", stored["saga_id"])
	require.Equal(t, "agent_self_update", stored["saga_type"])
	require.Equal(t, "req-1", stored["request_id"])
	require.Equal(t, "corr-1", stored["correlation_id"])
	require.Equal(t, true, stored["resumed"])
	require.Equal(t, "saga-update-1", stored["idempotency_key"])

	finalResult, ok := stored["final_result"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "2.0.0", finalResult["target_version"])

	steps, ok := stored["steps"].([]any)
	require.True(t, ok)
	require.Len(t, steps, 2)
}
