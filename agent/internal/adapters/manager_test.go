package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestManagerCompatibleAllowsWindows386OnWindowsAMD64(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	manager.targetOS = "windows"
	manager.targetArch = "amd64"

	if !manager.Compatible(ManifestItem{TargetOS: "windows", TargetArch: "386"}) {
		t.Fatal("windows/amd64 должен принимать windows/386 адаптер")
	}
	if !manager.Compatible(ManifestItem{TargetOS: "windows", TargetArch: "amd64"}) {
		t.Fatal("windows/amd64 должен принимать windows/amd64 адаптер")
	}
	if manager.Compatible(ManifestItem{TargetOS: "linux", TargetArch: "386"}) {
		t.Fatal("windows/amd64 не должен принимать linux-адаптер")
	}
}

func TestManagerCompatibleRejectsWindowsAMD64OnWindows386(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	manager.targetOS = "windows"
	manager.targetArch = "386"

	if manager.Compatible(ManifestItem{TargetOS: "windows", TargetArch: "amd64"}) {
		t.Fatal("windows/386 не должен принимать windows/amd64 адаптер")
	}
	if !manager.Compatible(ManifestItem{TargetOS: "windows", TargetArch: "386"}) {
		t.Fatal("windows/386 должен принимать windows/386 адаптер")
	}
}

func TestManagerSyncDownloadsWindows386ManifestOnWindowsAMD64(t *testing.T) {
	t.Parallel()

	content := []byte("adapter-x86")
	downloadURL := "https://example.invalid/adapter-x86.exe"
	downloader := &stubDownloader{
		payloads: map[string][]byte{downloadURL: content},
	}
	manager := NewManager(t.TempDir(), downloader)
	manager.targetOS = "windows"
	manager.targetArch = "amd64"

	statuses, err := manager.Sync(t.Context(), []ManifestItem{{
		AdapterID:   "adapter-x86",
		Version:     "1.0.0",
		DownloadURL: downloadURL,
		TargetOS:    "windows",
		TargetArch:  "386",
		SHA256:      checksum(content),
	}})
	if err != nil {
		t.Fatalf("Sync завершилась ошибкой: %v", err)
	}
	if len(downloader.calls) != 1 {
		t.Fatalf("ожидался один вызов скачивания, получено %d", len(downloader.calls))
	}
	if len(statuses) != 1 {
		t.Fatalf("ожидался один статус, получено %d", len(statuses))
	}
	if statuses[0].Status != "ready" {
		t.Fatalf("ожидался статус ready, получено %q", statuses[0].Status)
	}
	if statuses[0].TargetArch != "386" {
		t.Fatalf("ожидалась сохраненная архитектура manifest 386, получено %q", statuses[0].TargetArch)
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

func TestManagerRunByDescriptorPassesPayloadAndCapturesResult(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	adapterPath := writePythonAdapter(t, `
import json
import sys

payload = json.load(sys.stdin)
json.dump({
    "status": "success",
    "received": payload,
    "argv": sys.argv[1:],
}, sys.stdout)
sys.stdout.write("\n")
sys.stderr.write("adapter warning\n")
`)

	writeReadyDescriptor(t, manager, Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:   "adapter-runner",
			Version:     "1.0.0",
			TargetOS:    runtime.GOOS,
			TargetArch:  runtime.GOARCH,
			DownloadURL: "https://example.invalid/adapter-runner",
		},
		LocalPath:   adapterPath,
		Status:      "ready",
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	input := json.RawMessage(`{"protocol_version":"1","request_id":"req-1","task_type":"collect","payload":{"connection_type":"tcp","ip":"10.0.0.5","port":5555}}`)
	result, err := manager.Run(t.Context(), RunRequest{
		AdapterID: "adapter-runner",
		Command:   "run",
		Timeout:   3 * time.Second,
		Input:     input,
	})
	if err != nil {
		t.Fatalf("Run завершился ошибкой: %v", err)
	}
	if result.RunStatus != "completed" {
		t.Fatalf("ожидался run_status completed, получено %q", result.RunStatus)
	}
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("ожидался exit code 0, получено %+v", result.ExitCode)
	}
	if !strings.Contains(result.Stderr, "adapter warning") {
		t.Fatalf("ожидался stderr адаптера, получено %q", result.Stderr)
	}

	var structured map[string]any
	if err := json.Unmarshal(result.StructuredResult, &structured); err != nil {
		t.Fatalf("не удалось распарсить structured result: %v", err)
	}
	received, ok := structured["received"].(map[string]any)
	if !ok {
		t.Fatalf("ожидался объект received, получено %#v", structured["received"])
	}
	payloadObject, ok := received["payload"].(map[string]any)
	if !ok {
		t.Fatalf("ожидался объект payload, получено %#v", received["payload"])
	}
	if payloadObject["connection_type"] != "tcp" {
		t.Fatalf("ожидался connection_type=tcp, получено %#v", payloadObject["connection_type"])
	}
	if received["task_type"] != "collect" {
		t.Fatalf("ожидался task_type=collect, получено %#v", received["task_type"])
	}

	statuses, err := manager.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses завершился ошибкой: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("ожидался один статус, получено %d", len(statuses))
	}
	if statuses[0].RunStatus != "completed" {
		t.Fatalf("ожидался run_status completed в descriptor, получено %q", statuses[0].RunStatus)
	}
	if statuses[0].LastRunAt == nil {
		t.Fatal("ожидался last_run_at после запуска адаптера")
	}
	if statuses[0].LastExitCode == nil || *statuses[0].LastExitCode != 0 {
		t.Fatalf("ожидался сохраненный exit code 0, получено %+v", statuses[0].LastExitCode)
	}
	if statuses[0].LastError != "" {
		t.Fatalf("при успешном запуске last_error должен быть пустым, получено %q", statuses[0].LastError)
	}
}

func TestManagerRunTimeoutTerminatesHungProcess(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	adapterPath := writePythonAdapter(t, `
import time
time.sleep(5)
`)

	writeReadyDescriptor(t, manager, Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:   "adapter-timeout",
			Version:     "1.0.0",
			TargetOS:    runtime.GOOS,
			TargetArch:  runtime.GOARCH,
			DownloadURL: "https://example.invalid/adapter-timeout",
		},
		LocalPath:   adapterPath,
		Status:      "ready",
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	result, err := manager.Run(t.Context(), RunRequest{
		AdapterID: "adapter-timeout",
		Command:   "run",
		Timeout:   150 * time.Millisecond,
		Input:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("ожидалась ошибка таймаута")
	}
	if !strings.Contains(err.Error(), "превысило таймаут") {
		t.Fatalf("ожидалась ошибка таймаута, получено: %v", err)
	}
	if result.RunStatus != "timeout" {
		t.Fatalf("ожидался run_status timeout, получено %q", result.RunStatus)
	}

	statuses, err := manager.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses завершился ошибкой: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("ожидался один статус, получено %d", len(statuses))
	}
	if statuses[0].RunStatus != "timeout" {
		t.Fatalf("ожидался сохраненный run_status timeout, получено %q", statuses[0].RunStatus)
	}
	if !strings.Contains(statuses[0].LastError, "превысило таймаут") {
		t.Fatalf("ожидался last_error c таймаутом, получено %q", statuses[0].LastError)
	}
}

func TestManagerRunMissingBinaryReturnsControlledError(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	writeReadyDescriptor(t, manager, Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:   "adapter-missing",
			Version:     "1.0.0",
			TargetOS:    runtime.GOOS,
			TargetArch:  runtime.GOARCH,
			DownloadURL: "https://example.invalid/adapter-missing",
		},
		LocalPath:   filepath.Join(t.TempDir(), "missing-adapter.exe"),
		Status:      "ready",
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	result, err := manager.Run(t.Context(), RunRequest{
		AdapterID: "adapter-missing",
		Command:   "run",
		Timeout:   time.Second,
		Input:     json.RawMessage(`{}`),
	})
	if err == nil {
		t.Fatal("ожидалась контролируемая ошибка отсутствующего бинарника")
	}
	if !strings.Contains(err.Error(), "локальный бинарник адаптера") {
		t.Fatalf("ожидалась ошибка про отсутствующий бинарник, получено: %v", err)
	}
	if result.RunStatus != "failed" {
		t.Fatalf("ожидался run_status failed, получено %q", result.RunStatus)
	}

	statuses, err := manager.ListStatuses()
	if err != nil {
		t.Fatalf("ListStatuses завершился ошибкой: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("ожидался один статус, получено %d", len(statuses))
	}
	if statuses[0].RunStatus != "failed" {
		t.Fatalf("ожидался сохраненный run_status failed, получено %q", statuses[0].RunStatus)
	}
	if !strings.Contains(statuses[0].LastError, "локальный бинарник адаптера") {
		t.Fatalf("ожидался last_error про бинарник, получено %q", statuses[0].LastError)
	}
}

func TestManagerRunAllowsWindows386OnWindowsAMD64(t *testing.T) {
	t.Parallel()

	manager := NewManager(t.TempDir(), &stubDownloader{})
	manager.targetOS = "windows"
	manager.targetArch = "amd64"

	binaryPath := filepath.Join(t.TempDir(), "fiscal-shtrih-adapter-386.exe")
	if err := os.WriteFile(binaryPath, []byte("test"), 0o755); err != nil {
		t.Fatalf("не удалось создать тестовый бинарник: %v", err)
	}

	var runnerCalls int
	manager.runner = processRunnerFunc(func(_ context.Context, path string, args []string, stdin []byte) (processResult, error) {
		runnerCalls++
		if path != binaryPath {
			t.Fatalf("ожидался путь %s, получено %s", binaryPath, path)
		}
		if !slices.Equal(args, []string{"run"}) {
			t.Fatalf("ожидались аргументы [run], получено %v", args)
		}
		if string(stdin) != "{}" {
			t.Fatalf("ожидался stdin {}, получено %q", string(stdin))
		}
		exitCode := 0
		now := time.Now().UTC()
		return processResult{
			StartedAt:   now,
			CompletedAt: now.Add(10 * time.Millisecond),
			Stdout:      []byte(`{"status":"success"}`),
			ExitCode:    &exitCode,
		}, nil
	})

	writeReadyDescriptor(t, manager, Descriptor{
		ManifestItem: ManifestItem{
			AdapterID:       "fiscal-shtrih",
			Version:         "0.1.0",
			TargetOS:        "windows",
			TargetArch:      "386",
			ProtocolVersion: "1",
			DownloadURL:     "https://example.invalid/fiscal-shtrih-386.exe",
		},
		LocalPath:   binaryPath,
		Status:      "ready",
		InstalledAt: time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})

	result, err := manager.Run(t.Context(), RunRequest{
		AdapterID: "fiscal-shtrih",
		Command:   "run",
		Timeout:   time.Second,
		Input:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("Run завершился ошибкой: %v", err)
	}
	if runnerCalls != 1 {
		t.Fatalf("ожидался один вызов runner, получено %d", runnerCalls)
	}
	if result.RunStatus != "completed" {
		t.Fatalf("ожидался run_status completed, получено %q", result.RunStatus)
	}
}

type processRunnerFunc func(context.Context, string, []string, []byte) (processResult, error)

func (f processRunnerFunc) Run(ctx context.Context, path string, args []string, stdin []byte) (processResult, error) {
	return f(ctx, path, args, stdin)
}

func writeReadyDescriptor(t *testing.T, manager *Manager, descriptor Descriptor) {
	t.Helper()
	if err := manager.EnsureLayout(); err != nil {
		t.Fatalf("EnsureLayout завершился ошибкой: %v", err)
	}
	if descriptor.UpdatedAt.IsZero() {
		descriptor.UpdatedAt = time.Now().UTC()
	}
	if descriptor.InstalledAt.IsZero() {
		descriptor.InstalledAt = descriptor.UpdatedAt
	}
	if err := manager.writeDescriptor(descriptor); err != nil {
		t.Fatalf("не удалось сохранить descriptor: %v", err)
	}
}

func writePythonAdapter(t *testing.T, source string) string {
	t.Helper()

	scriptDir := t.TempDir()
	pythonPath := filepath.Join(scriptDir, "adapter_impl.py")
	pythonBody := "#!/usr/bin/env python3\n" + strings.TrimSpace(source) + "\n"
	if err := os.WriteFile(pythonPath, []byte(pythonBody), 0o755); err != nil {
		t.Fatalf("не удалось записать python-скрипт адаптера: %v", err)
	}

	if runtime.GOOS == "windows" {
		launcherPath := filepath.Join(scriptDir, "adapter.cmd")
		launcherBody := "@echo off\r\npython \"%~dp0adapter_impl.py\" %*\r\n"
		if err := os.WriteFile(launcherPath, []byte(launcherBody), 0o755); err != nil {
			t.Fatalf("не удалось записать launcher адаптера: %v", err)
		}
		return launcherPath
	}

	return pythonPath
}
