package workflows

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/protocol"
)

type stubAdapterRunner struct {
	lastRequest adapters.RunRequest
	result      adapters.RunResult
	err         error
}

func (s *stubAdapterRunner) Run(_ context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
	s.lastRequest = req
	return s.result, s.err
}

func TestAdapterRunWorkflowBuildsRunEnvelopeAndReturnsStructuredResult(t *testing.T) {
	t.Parallel()

	exitCode := 0
	completedAt := time.Date(2026, 3, 22, 12, 0, 5, 0, time.UTC)
	runner := &stubAdapterRunner{
		result: adapters.RunResult{
			AdapterID:        "fiscal-atol",
			Command:          "run",
			CompletedAt:      completedAt,
			Duration:         5 * time.Second,
			ExitCode:         &exitCode,
			Stdout:           `{"status":"success","devices":[{"serial":"123"}]}`,
			Stderr:           "driver hint",
			StructuredResult: json.RawMessage(`{"status":"success","devices":[{"serial":"123"}]}`),
			RunStatus:        "completed",
		},
	}
	workflow := NewAdapterRunWorkflow(runner)

	rawPayload := []byte(`{
		"adapter_id": "fiscal-atol",
		"operation": "collect",
		"timeout": "45s",
		"request_id": "run-123",
		"device_params": {
			"connection_type": "tcp",
			"ip": "10.10.10.15",
			"port": 5555,
			"model": "АТОЛ 22Ф",
			"driver_hints": {
				"branch": "10.9+"
			}
		}
	}`)

	result, err := workflow.Run(t.Context(), rawPayload)
	if err != nil {
		t.Fatalf("Run завершился ошибкой: %v", err)
	}
	if runner.lastRequest.AdapterID != "fiscal-atol" {
		t.Fatalf("ожидался adapter_id fiscal-atol, получено %q", runner.lastRequest.AdapterID)
	}
	if runner.lastRequest.Command != "run" {
		t.Fatalf("ожидалась команда run, получено %q", runner.lastRequest.Command)
	}
	if runner.lastRequest.Timeout != 45*time.Second {
		t.Fatalf("ожидался timeout 45s, получено %s", runner.lastRequest.Timeout)
	}

	var input protocol.AdapterCommandInputDTO
	if err := json.Unmarshal(runner.lastRequest.Input, &input); err != nil {
		t.Fatalf("не удалось распарсить stdin адаптера: %v", err)
	}
	if input.ProtocolVersion != "1" {
		t.Fatalf("ожидалась protocol_version=1, получено %q", input.ProtocolVersion)
	}
	if input.RequestID != "run-123" {
		t.Fatalf("ожидался request_id run-123, получено %q", input.RequestID)
	}
	if input.TaskType != "collect" {
		t.Fatalf("ожидался task_type collect, получено %q", input.TaskType)
	}
	if input.TimeoutSeconds != 45 {
		t.Fatalf("ожидался timeout_seconds=45, получено %d", input.TimeoutSeconds)
	}

	var deviceParams map[string]any
	if err := json.Unmarshal(input.Payload, &deviceParams); err != nil {
		t.Fatalf("не удалось распарсить payload адаптера: %v", err)
	}
	if deviceParams["connection_type"] != "tcp" {
		t.Fatalf("ожидался connection_type=tcp, получено %#v", deviceParams["connection_type"])
	}
	if deviceParams["ip"] != "10.10.10.15" {
		t.Fatalf("ожидался ip 10.10.10.15, получено %#v", deviceParams["ip"])
	}

	if result.Status != "completed" {
		t.Fatalf("ожидался статус completed, получено %q", result.Status)
	}
	if result.AdapterID != "fiscal-atol" {
		t.Fatalf("ожидался adapter_id fiscal-atol, получено %q", result.AdapterID)
	}
	if result.Operation != "collect" {
		t.Fatalf("ожидалась operation collect, получено %q", result.Operation)
	}
	if result.CompletedAt == nil || !result.CompletedAt.Equal(completedAt) {
		t.Fatalf("ожидался completed_at %s, получено %+v", completedAt, result.CompletedAt)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("ожидался exit code 0, получено %+v", result.ExitCode)
	}
	if result.DurationMS != 5000 {
		t.Fatalf("ожидался duration_ms=5000, получено %d", result.DurationMS)
	}
	if string(result.Result) != `{"status":"success","devices":[{"serial":"123"}]}` {
		t.Fatalf("получен неожиданный structured result: %s", string(result.Result))
	}
	if result.Stderr != "driver hint" {
		t.Fatalf("ожидался stderr driver hint, получено %q", result.Stderr)
	}
}
