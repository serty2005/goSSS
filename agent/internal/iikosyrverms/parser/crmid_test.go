package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCRMIDFindsLastMatch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "cash-server.log")
	content := "INFO ID организации : 12345\nWARN ID организации:\t67890\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("не удалось записать лог: %v", err)
	}

	crmID, diagnostic, err := ReadCRMID(path)
	if err != nil {
		t.Fatalf("ReadCRMID вернул ошибку: %v", err)
	}
	if crmID != "67890" {
		t.Fatalf("ожидался CRMID 67890, получено %q", crmID)
	}
	if diagnostic == "" {
		t.Fatal("ожидалась диагностическая строка")
	}
}

func TestReadCRMIDReturnsDiagnosticWhenMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "cash-server.log")
	if err := os.WriteFile(path, []byte("INFO without organization id\n"), 0o644); err != nil {
		t.Fatalf("не удалось записать лог: %v", err)
	}

	crmID, diagnostic, err := ReadCRMID(path)
	if err != nil {
		t.Fatalf("ReadCRMID вернул ошибку: %v", err)
	}
	if crmID != "" {
		t.Fatalf("ожидался пустой CRMID, получено %q", crmID)
	}
	if diagnostic != "cash-server.log найден, но строка с ID организации не обнаружена" {
		t.Fatalf("получена неожиданная диагностика: %q", diagnostic)
	}
}

func TestReadCRMIDReadsTailOfLargeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "cash-server.log")
	prefix := make([]byte, maxCashServerLogReadBytes+1024)
	for i := range prefix {
		prefix[i] = 'A'
	}
	content := append(prefix, []byte("\nINFO ID организации : 1740537\n")...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("не удалось записать большой лог: %v", err)
	}

	crmID, _, err := ReadCRMID(path)
	if err != nil {
		t.Fatalf("ReadCRMID вернул ошибку: %v", err)
	}
	if crmID != "1740537" {
		t.Fatalf("ожидался CRMID 1740537, получено %q", crmID)
	}
}
