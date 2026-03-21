package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"etalon-agent/internal/fiscalatol/contract"
	"etalon-agent/internal/fiscalatol/domain"
	"etalon-agent/internal/fiscalatol/fakes"
	"etalon-agent/internal/fiscalatol/orchestrator"
)

func TestAppRunReturnsPartialForMixedEndpoints(t *testing.T) {
	t.Parallel()

	request := `{
		"protocol_version": "1",
		"request_id": "req-1",
		"task_type": "collect",
		"payload": {
			"devices": [
				{ "transport": "com", "com_port": "COM4" },
				{ "transport": "tcp", "ip": "", "port": 5555 }
			]
		}
	}`

	bridge := &fakes.Bridge{
		Results: map[string]fakes.CollectResult{
			"COM4": {
				Payload: domain.FiscalPayload{
					ModelName:       "АТОЛ 30Ф",
					InstalledDriver: "10.10.8.0",
					Licenses:        map[string]domain.License{},
				},
			},
		},
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := &App{
		version: "1.0.0",
		stdin:   bytes.NewBufferString(request),
		stdout:  &stdout,
		stderr:  &stderr,
		bridge:  bridge,
		service: orchestrator.NewService(bridge),
	}

	exitCode := app.Execute(context.Background(), []string{"run"})
	if exitCode != 0 {
		t.Fatalf("ожидался код завершения 0, получено %d, stderr=%s", exitCode, stderr.String())
	}

	var response contract.RunResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("не удалось разобрать JSON ответа: %v", err)
	}
	if response.Status != "partial" {
		t.Fatalf("ожидался статус partial, получено %q", response.Status)
	}
	if response.Result == nil || len(response.Result.Devices) != 2 {
		t.Fatalf("ожидались результаты по двум endpoint, получено %+v", response.Result)
	}
	if response.Result.Devices[0].Status != domain.DeviceStatusSuccess {
		t.Fatalf("первый endpoint должен быть успешным, получено %q", response.Result.Devices[0].Status)
	}
	if response.Result.Devices[1].Status != domain.DeviceStatusFailed {
		t.Fatalf("второй endpoint должен быть неуспешным, получено %q", response.Result.Devices[1].Status)
	}
}
