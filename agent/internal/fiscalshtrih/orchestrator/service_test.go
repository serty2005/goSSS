package orchestrator

import (
	"context"
	"errors"
	"testing"

	"etalon-agent/internal/fiscalshtrih/domain"
	"etalon-agent/internal/fiscalshtrih/drvfr"
	"etalon-agent/internal/fiscalshtrih/fakes"
)

func TestServiceCollectReturnsPartialStatusForMixedEndpoints(t *testing.T) {
	t.Parallel()

	endpoints := []domain.Endpoint{
		{Transport: domain.TransportCOM, COMPort: "COM4", BaudRate: "115200"},
		{Transport: domain.TransportTCP, IP: "10.25.1.22", Port: 5555},
	}
	bridge := &fakes.Bridge{
		Results: map[string]fakes.CollectResult{
			"COM4": {
				Payload: domain.FiscalPayload{
					ModelName:       "ШТРИХ-М-01Ф",
					InstalledDriver: "4.17.0.0",
					Licenses:        "Подписка до 4 квартала 2027 года",
				},
				Meta: drvfr.CollectMeta{
					ConnectionLabel: "COM4",
					Transport:       domain.TransportCOM,
					DriverVersion:   "4.17.0.0",
				},
			},
			"10.25.1.22:5555": {
				Meta: drvfr.CollectMeta{
					ConnectionLabel: "10.25.1.22:5555",
					Transport:       domain.TransportTCP,
					DriverVersion:   "4.17.0.0",
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
