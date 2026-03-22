package command

import (
	"context"
	"testing"
	"time"

	"etalon-agent/internal/iikosyrverms/domain"
)

type stubScanner struct {
	report domain.ScanReport
	err    error
}

func (s stubScanner) Scan(context.Context) (domain.ScanReport, error) {
	return s.report, s.err
}

func TestHealthReturnsDegradedWhenPathFoundButRMSURLMissing(t *testing.T) {
	t.Parallel()

	candidate := domain.Candidate{
		SoftwareType: domain.SoftwareTypeIiko,
		RootPath:     `C:\Users\demo\AppData\Roaming\iiko`,
		ActivityPath: `C:\Users\demo\AppData\Roaming\iiko\CashServer\config.xml`,
		ActivityAt:   time.Date(2026, 3, 19, 10, 0, 0, 0, time.UTC),
	}
	handler := HealthHandler{
		Scanner: stubScanner{
			report: domain.ScanReport{
				Supported:           true,
				CurrentOS:           "windows",
				CurrentArch:         "amd64",
				ExpectedOS:          "windows",
				ExpectedArch:        "amd64",
				AppDataEnvAvailable: true,
				AppDataRoots:        []string{`C:\Users\demo\AppData\Roaming`},
				ActiveCandidate:     &candidate,
				Candidates:          []domain.Candidate{candidate},
				SoftwareType:        domain.SoftwareTypeIiko,
				DetectionReason:     "Выбран самый свежий путь активности",
			},
		},
	}

	response := handler.Handle(context.Background())
	if response.Status != "degraded" {
		t.Fatalf("ожидался статус degraded, получено %q", response.Status)
	}
}
