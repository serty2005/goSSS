package adapters

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type stubDownloader struct {
	payloads map[string][]byte
	errs     map[string]error
	calls    []string
}

func (d *stubDownloader) DownloadFile(_ context.Context, url string) ([]byte, error) {
	d.calls = append(d.calls, url)
	if err := d.errs[url]; err != nil {
		return nil, err
	}
	payload, ok := d.payloads[url]
	if !ok {
		return nil, errors.New("не найден тестовый payload")
	}
	return slices.Clone(payload), nil
}

func TestManagerEnsureLayoutCreatesDirectories(t *testing.T) {
	t.Parallel()

	rootDir := filepath.Join(t.TempDir(), "adapters")
	manager := NewManager(rootDir, &stubDownloader{})

	if err := manager.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout вернул ошибку: %v", err)
	}

	for _, path := range []string{manager.rootDir, manager.binariesDir, manager.descriptorsDir, manager.tmpDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("не удалось получить информацию о каталоге %s: %v", path, err)
		}
		if !info.IsDir() {
			t.Fatalf("ожидался каталог %s", path)
		}
	}
}

func TestManagerSyncSkipsManifestWithoutRequiredFields(t *testing.T) {
	t.Parallel()

	downloader := &stubDownloader{}
	manager := NewManager(t.TempDir(), downloader)

	statuses, err := manager.Sync(t.Context(), []ManifestItem{
		{Version: "1.0.0", DownloadURL: "https://example.invalid/adapter-a"},
		{AdapterID: "adapter-b", DownloadURL: "https://example.invalid/adapter-b"},
		{AdapterID: "adapter-c", Version: "1.0.0"},
	})
	if err != nil {
		t.Fatalf("Sync не должен возвращать ошибку для manifest без обязательных полей: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("ожидалось отсутствие статусов, получено %d", len(statuses))
	}
	if len(downloader.calls) != 0 {
		t.Fatalf("скачивание не должно выполняться для невалидных manifest, вызовов: %d", len(downloader.calls))
	}
}

func TestManagerSyncSkipsIncompatibleManifest(t *testing.T) {
	t.Parallel()

	downloader := &stubDownloader{}
	manager := NewManager(t.TempDir(), downloader)

	targetOS := "linux"
	if runtime.GOOS == "linux" {
		targetOS = "windows"
	}

	statuses, err := manager.Sync(t.Context(), []ManifestItem{{
		AdapterID:   "adapter-a",
		Version:     "1.0.0",
		DownloadURL: "https://example.invalid/adapter-a",
		TargetOS:    targetOS,
	}})
	if err != nil {
		t.Fatalf("Sync не должен возвращать ошибку для несовместимого manifest: %v", err)
	}
	if len(statuses) != 0 {
		t.Fatalf("ожидалось отсутствие статусов, получено %d", len(statuses))
	}
	if len(downloader.calls) != 0 {
		t.Fatalf("скачивание не должно выполняться для несовместимого manifest, вызовов: %d", len(downloader.calls))
	}
}

func TestManagerSyncDoesNotRedownloadSameRevision(t *testing.T) {
	t.Parallel()

	content := []byte("adapter-binary")
	downloadURL := "https://example.invalid/adapter-a.exe"
	downloader := &stubDownloader{
		payloads: map[string][]byte{downloadURL: content},
	}
	manager := NewManager(t.TempDir(), downloader)

	manifest := ManifestItem{
		AdapterID:   "adapter-a",
		Version:     "1.0.0",
		DownloadURL: downloadURL,
		SHA256:      checksum(content),
	}

	if _, err := manager.Sync(t.Context(), []ManifestItem{manifest}); err != nil {
		t.Fatalf("первая Sync завершилась ошибкой: %v", err)
	}
	if _, err := manager.Sync(t.Context(), []ManifestItem{manifest}); err != nil {
		t.Fatalf("повторная Sync завершилась ошибкой: %v", err)
	}

	if len(downloader.calls) != 1 {
		t.Fatalf("ожидался один вызов скачивания, получено %d", len(downloader.calls))
	}
}

func TestManagerSyncChecksumMismatchSetsErrorStatus(t *testing.T) {
	t.Parallel()

	content := []byte("broken-binary")
	downloadURL := "https://example.invalid/adapter-a.exe"
	downloader := &stubDownloader{
		payloads: map[string][]byte{downloadURL: content},
	}
	manager := NewManager(t.TempDir(), downloader)

	statuses, err := manager.Sync(t.Context(), []ManifestItem{{
		AdapterID:   "adapter-a",
		Version:     "1.0.0",
		DownloadURL: downloadURL,
		SHA256:      strings.Repeat("0", 64),
	}})
	if err == nil {
		t.Fatal("ожидалась ошибка checksum mismatch")
	}
	if !strings.Contains(err.Error(), "sha256 не совпадает") {
		t.Fatalf("ожидалась ошибка про sha256, получено: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("ожидался один статус после ошибки checksum, получено %d", len(statuses))
	}
	if statuses[0].Status != "error" {
		t.Fatalf("ожидался статус error, получено %q", statuses[0].Status)
	}
	if !strings.Contains(statuses[0].LastError, "sha256 не совпадает") {
		t.Fatalf("ожидался текст ошибки checksum в статусе, получено %q", statuses[0].LastError)
	}
	if statuses[0].LocalPath != "" {
		t.Fatalf("для ошибочного descriptor локальный путь должен быть пустым, получено %q", statuses[0].LocalPath)
	}
}

func TestManagerSyncPersistsDescriptorAndStatusesAreSorted(t *testing.T) {
	t.Parallel()

	contentA := []byte("adapter-a")
	contentB := []byte("adapter-b")
	urlA := "https://example.invalid/adapter-a.exe"
	urlB := "https://example.invalid/adapter-b.exe"
	downloader := &stubDownloader{
		payloads: map[string][]byte{
			urlA: contentA,
			urlB: contentB,
		},
	}
	manager := NewManager(t.TempDir(), downloader)

	_, err := manager.Sync(t.Context(), []ManifestItem{
		{
			AdapterID:       "adapter-b",
			AdapterType:     "beta",
			Version:         "2.0.0",
			ProtocolVersion: "1",
			DownloadURL:     urlB,
			SHA256:          checksum(contentB),
		},
		{
			AdapterID:       "adapter-a",
			AdapterType:     "alpha",
			Version:         "1.0.0",
			ProtocolVersion: "1",
			DownloadURL:     urlA,
			SHA256:          checksum(contentA),
		},
	})
	if err != nil {
		t.Fatalf("Sync завершилась ошибкой: %v", err)
	}

	statuses, err := manager.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses завершилась ошибкой: %v", err)
	}
	if len(statuses) != 2 {
		t.Fatalf("ожидалось 2 статуса, получено %d", len(statuses))
	}
	if statuses[0].AdapterID != "adapter-a" || statuses[1].AdapterID != "adapter-b" {
		t.Fatalf("ожидался детерминированный порядок статусов, получено %+v", statuses)
	}

	statusA := statuses[0]
	if statusA.Status != "ready" {
		t.Fatalf("ожидался статус ready, получено %q", statusA.Status)
	}
	if statusA.AdapterType != "alpha" || statusA.ProtocolVersion != "1" {
		t.Fatalf("поля descriptor не попали в status: %+v", statusA)
	}
	if statusA.SHA256 != checksum(contentA) {
		t.Fatalf("ожидался сохраненный checksum %q, получено %q", checksum(contentA), statusA.SHA256)
	}
	if statusA.FileSize != int64(len(contentA)) {
		t.Fatalf("ожидался размер файла %d, получено %d", len(contentA), statusA.FileSize)
	}
	if statusA.LocalPath == "" {
		t.Fatal("ожидался путь к локальному бинарнику")
	}
	if _, err := os.Stat(statusA.LocalPath); err != nil {
		t.Fatalf("локальный бинарник не найден по пути %s: %v", statusA.LocalPath, err)
	}
}
