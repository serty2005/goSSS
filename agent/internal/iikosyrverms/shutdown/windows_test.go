//go:build windows

package shutdown

import (
	"context"
	"testing"

	"etalon-agent/internal/iikosyrverms/domain"
)

func TestParseTasklistCSVFindsMatchingPIDs(t *testing.T) {
	t.Parallel()

	raw := []byte("\"iikoFront.Net.exe\",\"1234\",\"Console\",\"1\",\"20 000 K\"\r\n\"notepad.exe\",\"99\",\"Console\",\"1\",\"10 000 K\"\r\n\"iikoFront.Net.Backup.exe\",\"5678\",\"Console\",\"1\",\"22 000 K\"\r\n")
	pids, err := parseTasklistCSV(raw, "iikoFront.Net")
	if err != nil {
		t.Fatalf("parseTasklistCSV вернул ошибку: %v", err)
	}
	if len(pids) != 2 {
		t.Fatalf("ожидалось два PID, получено %d", len(pids))
	}
}

func TestSoftShutdownReturnsErrorWhenProcessMissing(t *testing.T) {
	t.Parallel()

	controller := &Controller{
		tasklistOutput: func(context.Context) ([]byte, error) {
			return []byte("\"notepad.exe\",\"99\"\r\n"), nil
		},
		sendClose: func(uint32) (int, error) {
			return 1, nil
		},
	}

	result, err := controller.SoftShutdown(t.Context(), domain.SoftwareTypeIiko, "iikoFront.Net")
	if err != ErrProcessNotFound {
		t.Fatalf("ожидалась ошибка ErrProcessNotFound, получено %v", err)
	}
	if result.ProcessName != "iikoFront.Net" {
		t.Fatalf("ожидалось имя процесса iikoFront.Net, получено %q", result.ProcessName)
	}
}
