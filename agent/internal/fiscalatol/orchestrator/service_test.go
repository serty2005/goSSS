package orchestrator

import (
	"context"
	"errors"
	"testing"

	"etalon-agent/internal/fiscalatol/domain"
	"etalon-agent/internal/fiscalatol/fakes"
	"etalon-agent/internal/fiscalatol/libfptr"
)

func TestServiceCollectReturnsPartialStatusForMixedEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []domain.Endpoint{
		{Transport: domain.TransportCOM, COMPort: "COM4", BaudRate: "115200"},
		{Transport: domain.TransportTCP, IP: "192.168.0.90", Port: 5555},
	}
	bridge := &fakes.Bridge{
		Results: map[string]fakes.CollectResult{
			"COM4": {
				Payload: domain.FiscalPayload{
					ModelName:       "АТОЛ 30Ф",
					InstalledDriver: "10.10.8.0",
					Licenses:        map[string]domain.License{},
				},
				Meta: libfptr.CollectMeta{
					ConnectionLabel: "COM4",
					Transport:       domain.TransportCOM,
					DriverVersion:   "10.10.8.0",
				},
			},
			"192.168.0.90:5555": {
				Meta: libfptr.CollectMeta{
					ConnectionLabel: "192.168.0.90:5555",
					Transport:       domain.TransportTCP,
					DriverVersion:   "10.10.8.0",
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
