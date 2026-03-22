package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractRMSURLReadsElement(t *testing.T) {
	t.Parallel()

	raw := []byte(`<configuration><serverUrl>https://demo.iiko.local/resto/</serverUrl></configuration>`)
	got := ExtractRMSURL(raw)
	want := "https://demo.iiko.local/resto/"

	if got != want {
		t.Fatalf("ожидался URL %q, получено %q", want, got)
	}
}

func TestExtractRMSURLReadsAttribute(t *testing.T) {
	t.Parallel()

	raw := []byte(`<cashServer serverUrl="https://demo.syrve.local/resto/" />`)
	got := ExtractRMSURL(raw)
	want := "https://demo.syrve.local/resto/"

	if got != want {
		t.Fatalf("ожидался URL %q, получено %q", want, got)
	}
}

func TestParseConfigFilesReturnsURLAndSourceFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	olderPath := filepath.Join(root, "iiko", "cashserver", "config.xml")
	newerPath := filepath.Join(root, "syrve", "CashServer", "config.xml")
	if err := os.MkdirAll(filepath.Dir(olderPath), 0o755); err != nil {
		t.Fatalf("не удалось создать каталог для olderPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(newerPath), 0o755); err != nil {
		t.Fatalf("не удалось создать каталог для newerPath: %v", err)
	}

	if err := os.WriteFile(olderPath, []byte(`<configuration><serverUrl>https://older.local/resto/</serverUrl></configuration>`), 0o644); err != nil {
		t.Fatalf("не удалось записать olderPath: %v", err)
	}
	if err := os.WriteFile(newerPath, []byte(`<cashServer serverUrl="https://newer.local/resto/" />`), 0o644); err != nil {
		t.Fatalf("не удалось записать newerPath: %v", err)
	}

	olderTime := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	newerTime := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(olderPath, olderTime, olderTime); err != nil {
		t.Fatalf("не удалось обновить время olderPath: %v", err)
	}
	if err := os.Chtimes(newerPath, newerTime, newerTime); err != nil {
		t.Fatalf("не удалось обновить время newerPath: %v", err)
	}

	url, source, _ := ParseConfigFiles([]string{olderPath, newerPath})
	if url != "https://newer.local/resto/" {
		t.Fatalf("ожидался URL более свежего файла, получено %q", url)
	}
	if source != newerPath {
		t.Fatalf("ожидался source_file %q, получено %q", newerPath, source)
	}
}
