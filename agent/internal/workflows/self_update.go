package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
)

type SelfUpdateWorkflow struct {
	sagaWorkflow *SagaRunWorkflow
}

func NewSelfUpdateWorkflow(sagaWorkflow *SagaRunWorkflow) *SelfUpdateWorkflow {
	return &SelfUpdateWorkflow{sagaWorkflow: sagaWorkflow}
}

func (w *SelfUpdateWorkflow) Type() string {
	return "self_update"
}

func (w *SelfUpdateWorkflow) Run(ctx context.Context, payload []byte) (protocol.TaskExecutionResult, error) {
	if w.sagaWorkflow == nil {
		err := fmt.Errorf("workflow saga_run не настроен")
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  err.Error(),
		}, err
	}

	var legacy protocol.SelfUpdateTaskPayload
	if err := json.Unmarshal(payload, &legacy); err != nil {
		parseErr := fmt.Errorf("невалидный payload self_update: %w", err)
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  parseErr.Error(),
		}, parseErr
	}

	inputRaw, err := json.Marshal(protocol.AgentSelfUpdateSagaInput{
		TargetVersion: strings.TrimSpace(legacy.Version),
		DownloadURL:   strings.TrimSpace(legacy.DownloadURL),
		SHA256:        strings.TrimSpace(legacy.SHA256),
		FileName:      strings.TrimSpace(legacy.FileName),
		RestartPolicy: "immediate",
		Args:          append([]string(nil), legacy.Args...),
	})
	if err != nil {
		marshalErr := fmt.Errorf("не удалось подготовить saga input для self_update: %w", err)
		return protocol.TaskExecutionResult{
			Status: "failed",
			Error:  marshalErr.Error(),
		}, marshalErr
	}

	sagaTask := protocol.SagaRunTaskPayload{
		SagaID:    legacySelfUpdateSagaID(legacy),
		SagaType:  saga.AgentSelfUpdateSagaType,
		RequestID: strings.TrimSpace(legacy.Version),
		Input:     inputRaw,
		IdempotencyHint: protocol.SagaIdempotencyHint{
			Key: legacySelfUpdateSagaID(legacy),
		},
	}
	return w.sagaWorkflow.RunTask(ctx, sagaTask)
}

func legacySelfUpdateSagaID(payload protocol.SelfUpdateTaskPayload) string {
	parts := []string{
		strings.TrimSpace(payload.Version),
		strings.TrimSpace(payload.DownloadURL),
		strings.TrimSpace(payload.SHA256),
		strings.TrimSpace(payload.FileName),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "legacy-self-update-" + hex.EncodeToString(sum[:8])
}
