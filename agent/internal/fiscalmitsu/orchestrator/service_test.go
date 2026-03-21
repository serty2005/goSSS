package orchestrator

import (
	"context"
	"errors"
	"testing"

	"etalon-agent/internal/fiscalmitsu/domain"
	"etalon-agent/internal/fiscalmitsu/fakes"
	"etalon-agent/internal/fiscalmitsu/protocol"
)

func TestServiceCollectReturnsPartialStatusForMixedEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []domain.Endpoint{
		{Transport: domain.TransportCOM, COMPort: "COM7", BaudRate: "115200"},
		{Transport: domain.TransportTCP, IP: "10.127.1.124", Port: 8200},
	}
	bridge := &fakes.Bridge{
		Results: map[string]fakes.CollectResult{
			"COM7": {
				Payload: domain.FiscalPayload{
					ModelName:       "MITSU M1",
					InstalledDriver: "1.2.3.4",
					Licenses:        "None",
				},
				Meta: protocol.CollectMeta{
					ConnectionLabel: "COM7",
					Transport:       domain.TransportCOM,
					DriverVersion:   "1.2.3.4",
				},
			},
			"10.127.1.124:8200": {
				Meta: protocol.CollectMeta{
					ConnectionLabel: "10.127.1.124:8200",
					Transport:       domain.TransportTCP,
					DriverVersion:   "1.2.3.4",
				},
				Err: errors.New("таймаут соединения"),
			},
		},
	}

	results := NewService(bridge).Collect(context.Background(), endpoints)
	if len(results) != 2 {
		t.Fatalf("ожидались результаты по двум endpoint, получено %d", len(results))
	}
	if results[0].Status != domain.DeviceStatusSuccess {
		t.Fatalf("первый endpoint должен быть успешным, получено %q", results[0].Status)
	}
	if results[1].Status != domain.DeviceStatusFailed {
		t.Fatalf("второй endpoint должен быть неуспешным, получено %q", results[1].Status)
	}
	if status := OverallStatus(results); status != "partial" {
		t.Fatalf("ожидался общий статус partial, получено %q", status)
	}
}
