package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"etalon-agent/internal/adapterexec"
	"etalon-agent/internal/adapters"
	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
)

type AdapterRunner interface {
	Run(context.Context, adapters.RunRequest) (adapters.RunResult, error)
}

func NewAdapterRunHandler(runner AdapterRunner) saga.StepHandler {
	return saga.StepHandlerFunc(func(ctx context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		if runner == nil {
			return saga.StepOutcome{}, fmt.Errorf("runner адаптеров не настроен")
		}

		input, err := decodeAdapterStepInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}

		prepared, err := adapterexec.PrepareSagaRunRequest(input)
		if err != nil {
			return saga.StepOutcome{}, err
		}

		startedAt := time.Now().UTC()
		runResult, runErr := runner.Run(ctx, adapterexec.ToRunRequest(prepared))
		completedAt := time.Now().UTC()

		output := mustMarshalJSON(map[string]any{
			"status":       normalizeAdapterStepStatus(runResult.RunStatus, runErr),
			"adapter_id":   prepared.AdapterID,
			"command":      prepared.Command,
			"operation":    prepared.Operation,
			"completed_at": completedAt.Format(time.RFC3339),
			"duration_ms":  completedAt.Sub(startedAt).Milliseconds(),
			"exit_code":    cloneIntPointer(runResult.ExitCode),
			"stdout":       runResult.Stdout,
			"stderr":       runResult.Stderr,
			"result":       decodeRaw(runResult.StructuredResult),
		})
		if runErr != nil {
			return saga.StepOutcome{}, &saga.StepExecutionError{
				Err:    runErr,
				Output: output,
			}
		}
		return saga.StepOutcome{Output: output}, nil
	})
}

func decodeAdapterStepInput(step saga.Step, execution *saga.Execution) (protocol.SagaAdapterStepInput, error) {
	raw := step.Input
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = execution.Input()
	}

	var input protocol.SagaAdapterStepInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return protocol.SagaAdapterStepInput{}, fmt.Errorf("невалидный input шага %s: %w", step.ID, err)
	}
	return input, nil
}

func normalizeAdapterStepStatus(runStatus string, runErr error) string {
	status := strings.TrimSpace(runStatus)
	if status != "" {
		return status
	}
	if runErr != nil {
		return "failed"
	}
	return "completed"
}

func decodeRaw(raw json.RawMessage) any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return value
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
