package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"etalon-agent/internal/fiscalmitsu/contract"
	"etalon-agent/internal/fiscalmitsu/domain"
	"etalon-agent/internal/fiscalmitsu/fakes"
	"etalon-agent/internal/fiscalmitsu/orchestrator"
)

func TestAppRunReturnsPartialForMixedEndpoints(t *testing.T) {
	t.Parallel()

	request := `{
		"protocol_version": "1",
		"request_id": "req-1",
		"task_type": "collect",
		"payload": {
			"devices": [
				{ "transport": "tcp", "ip": "10.127.1.124" },
				{ "transport": "tcp", "ip": "", "port": 8200 }
			]
		}
	}`

	bridge := &fakes.Bridge{
		Results: map[string]fakes.CollectResult{
			"10.127.1.124:8200": {
				Payload: domain.FiscalPayload{
					ModelName:       "MITSU M1",
					InstalledDriver: "1.2.3.4",
					Licenses:        "None",
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
