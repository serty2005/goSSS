package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
)

type stubScanner struct {
	calls  int
	report domain.ScanReport
	err    error
}

func (s *stubScanner) Scan(context.Context) (domain.ScanReport, error) {
	s.calls++
	return s.report, s.err
}

func TestAppRunIgnoresPayloadPaths(t *testing.T) {
	t.Parallel()

	request := `{
		"protocol_version": "1",
		"request_id": "run-1",
		"task_type": "collect",
		"payload": {
			"paths": [
				"C:\\temp\\override",
				"D:\\manual\\config.xml"
			],
			"appdata_path": "C:\\temp\\fake-appdata"
		}
	}`

	candidate := domain.Candidate{
		SoftwareType: domain.SoftwareTypeIiko,
		RootPath:     `C:\Users\demo\AppData\Roaming\iiko`,
		ActivityPath: `C:\Users\demo\AppData\Roaming\iiko\cashserver\config.xml`,
		ActivityAt:   time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
	}
	scanner := &stubScanner{
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
			RMSURL:              "https://demo.iiko.local/resto/",
			SourceFile:          candidate.ActivityPath,
			DetectionReason:     "Выбран самый свежий путь активности",
		},
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := New("1.0.0", scanner)
	app.stdin = bytes.NewBufferString(request)
	app.stdout = &stdout
	app.stderr = &stderr

	exitCode := app.Execute(context.Background(), []string{"run"})
	if exitCode != 0 {
		t.Fatalf("ожидался код завершения 0, получено %d, stderr=%s", exitCode, stderr.String())
	}
	if scanner.calls != 1 {
		t.Fatalf("ожидался один вызов scanner.Scan, получено %d", scanner.calls)
	}

	var response contract.RunResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("не удалось разобрать JSON ответа: %v", err)
	}
	if response.Status != "success" {
		t.Fatalf("ожидался статус success, получено %q", response.Status)
	}
	if response.Result.SoftwareType != domain.SoftwareTypeIiko {
		t.Fatalf("ожидался software_type=iiko, получено %q", response.Result.SoftwareType)
	}
	if response.Result.RMSURL != "https://demo.iiko.local/resto/" {
		t.Fatalf("ожидался RMS URL %q, получено %q", "https://demo.iiko.local/resto/", response.Result.RMSURL)
	}
}
