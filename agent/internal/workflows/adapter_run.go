package workflows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/protocol"

	"github.com/google/uuid"
)

type adapterRunner interface {
	Run(context.Context, adapters.RunRequest) (adapters.RunResult, error)
}

type AdapterRunWorkflow struct {
	runner    adapterRunner
	debugLogf func(string, ...any)
}

func NewAdapterRunWorkflow(runner adapterRunner, debugLoggers ...func(string, ...any)) *AdapterRunWorkflow {
	var debugf func(string, ...any)
	if len(debugLoggers) > 0 {
		debugf = debugLoggers[0]
	}
	return &AdapterRunWorkflow{runner: runner, debugLogf: debugf}
}

func (w *AdapterRunWorkflow) Type() string {
	return "run_adapter"
}

func (w *AdapterRunWorkflow) Run(ctx context.Context, payload []byte) (protocol.TaskExecutionResult, error) {
	if w.runner == nil {
		err := fmt.Errorf("runner запуска адаптера не настроен")
		return protocol.TaskExecutionResult{
			Status:      "failed",
			CompletedAt: timePointer(time.Now().UTC()),
			Error:       err.Error(),
		}, err
	}

	var req protocol.AdapterRunTaskPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		return protocol.TaskExecutionResult{
			Status:      "failed",
			CompletedAt: timePointer(time.Now().UTC()),
			Error:       fmt.Sprintf("невалидный payload запуска адаптера: %v", err),
		}, fmt.Errorf("невалидный payload запуска адаптера: %w", err)
	}

	adapterID := strings.TrimSpace(req.AdapterID)
	if adapterID == "" {
		err := fmt.Errorf("в payload запуска адаптера отсутствует adapter_id")
		return protocol.TaskExecutionResult{
			Status:      "failed",
			CompletedAt: timePointer(time.Now().UTC()),
			Error:       err.Error(),
		}, err
	}

	command := strings.TrimSpace(req.Command)
	if command == "" {
		command = "run"
	}

	timeout, err := resolveAdapterRunTimeout(req)
	if err != nil {
		return protocol.TaskExecutionResult{
			Status:      "failed",
			AdapterID:   adapterID,
			Command:     command,
			Operation:   strings.TrimSpace(req.Operation),
			CompletedAt: timePointer(time.Now().UTC()),
			Error:       err.Error(),
		}, err
	}

	commandInput, err := buildAdapterCommandInput(req, command, timeout)
	if err != nil {
		return protocol.TaskExecutionResult{
			Status:      "failed",
			AdapterID:   adapterID,
			Command:     command,
			Operation:   strings.TrimSpace(req.Operation),
			CompletedAt: timePointer(time.Now().UTC()),
			Error:       err.Error(),
		}, err
	}
	w.debugf("Payload run_adapter от сервера: adapter_id=%s command=%s operation=%s payload=%s", adapterID, command, strings.TrimSpace(req.Operation), compactJSONForDebug(payload))
	w.debugf("Нормализованный payload для адаптера: adapter_id=%s command=%s timeout=%s stdin=%s", adapterID, command, timeout, compactJSONForDebug(commandInput))

	runResult, runErr := w.runner.Run(ctx, adapters.RunRequest{
		AdapterID: adapterID,
		Command:   command,
		Timeout:   timeout,
		Input:     commandInput,
	})

	execution := protocol.TaskExecutionResult{
		Status:      normalizeTaskStatus(runResult.RunStatus, runErr),
		AdapterID:   adapterID,
		Command:     command,
		Operation:   strings.TrimSpace(req.Operation),
		CompletedAt: timePointer(nonZeroTime(runResult.CompletedAt, time.Now().UTC())),
		DurationMS:  runResult.Duration.Milliseconds(),
		ExitCode:    cloneIntPointer(runResult.ExitCode),
		Stdout:      runResult.Stdout,
		Stderr:      runResult.Stderr,
		Result:      cloneRawMessage(runResult.StructuredResult),
	}
	if runErr != nil {
		execution.Error = runErr.Error()
	}
	w.debugf(
		"Результат запуска адаптера: adapter_id=%s command=%s run_status=%s exit_code=%v duration_ms=%d stdout=%q stderr=%q result=%s error=%q",
		adapterID,
		command,
		execution.Status,
		execution.ExitCode,
		execution.DurationMS,
		execution.Stdout,
		execution.Stderr,
		compactJSONForDebug(execution.Result),
		execution.Error,
	)
	return execution, runErr
}

func buildAdapterCommandInput(req protocol.AdapterRunTaskPayload, command string, timeout time.Duration) (json.RawMessage, error) {
	protocolVersion := strings.TrimSpace(req.ProtocolVersion)
	if protocolVersion == "" {
		protocolVersion = "1"
	}

	requestID := strings.TrimSpace(req.RequestID)
	if requestID == "" {
		requestID = uuid.NewString()
	}

	envelope := protocol.AdapterCommandInputDTO{
		ProtocolVersion: protocolVersion,
		RequestID:       requestID,
		TimeoutSeconds:  int(timeout / time.Second),
	}
	if envelope.TimeoutSeconds <= 0 {
		envelope.TimeoutSeconds = 1
	}

	switch strings.ToLower(strings.TrimSpace(command)) {
	case "run":
		operation := strings.TrimSpace(req.Operation)
		if operation == "" {
			return nil, fmt.Errorf("для команды run обязательно поле operation")
		}
		envelope.TaskType = operation
		envelope.Payload = resolveAdapterPayload(req)
	case "describe":
	case "health":
	default:
		envelope.TaskType = strings.TrimSpace(req.Operation)
		envelope.Payload = resolveAdapterPayload(req)
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать payload адаптера: %w", err)
	}
	return raw, nil
}

func resolveAdapterPayload(req protocol.AdapterRunTaskPayload) json.RawMessage {
	for _, candidate := range []json.RawMessage{req.DeviceParams, req.Payload} {
		if len(strings.TrimSpace(string(candidate))) > 0 {
			return cloneRawMessage(candidate)
		}
	}
	return json.RawMessage(`{}`)
}

func resolveAdapterRunTimeout(req protocol.AdapterRunTaskPayload) (time.Duration, error) {
	if req.TimeoutSeconds > 0 {
		return time.Duration(req.TimeoutSeconds) * time.Second, nil
	}

	raw := strings.TrimSpace(req.Timeout)
	if raw == "" {
		return 30 * time.Second, nil
	}

	if duration, err := time.ParseDuration(raw); err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("timeout должен быть больше нуля")
		}
		return duration, nil
	}

	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return 0, fmt.Errorf("не удалось разобрать timeout %q", raw)
	}
	return time.Duration(seconds) * time.Second, nil
}

func normalizeTaskStatus(runStatus string, runErr error) string {
	status := strings.TrimSpace(runStatus)
	if status != "" {
		return status
	}
	if runErr != nil {
		return "failed"
	}
	return "completed"
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func nonZeroTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback.UTC()
	}
	return value.UTC()
}

func (w *AdapterRunWorkflow) debugf(format string, args ...any) {
	if w.debugLogf == nil {
		return
	}
	w.debugLogf(format, args...)
}

func compactJSONForDebug(raw []byte) string {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "{}"
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}

	normalized, err := json.Marshal(value)
	if err != nil {
		return strings.TrimSpace(string(raw))
	}
	return string(normalized)
}
