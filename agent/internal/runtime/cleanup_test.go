package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"etalon-agent/internal/config"
)

type stubRegistryCleaner struct {
	deleteAllCalls int
	deleteAllErr   error
}

func (s *stubRegistryCleaner) DeleteAll() error {
	s.deleteAllCalls++
	return s.deleteAllErr
}

func TestDataCleanerCleanupRemovesAgentDataAndUpdateArtifacts(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	dataDir := filepath.Join(rootDir, "ProgramData", "MyHoreca", "XenionAgent")
	if err := os.MkdirAll(filepath.Join(dataDir, "adapters", "bin"), 0o755); err != nil {
		t.Fatalf("не удалось подготовить каталог данных: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "connectivity_state.json"), []byte(`{"state":"online"}`), 0o644); err != nil {
		t.Fatalf("не удалось создать connectivity_state.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "adapters", "bin", "adapter.exe"), []byte("adapter"), 0o644); err != nil {
		t.Fatalf("не удалось создать бинарник адаптера: %v", err)
	}

	exeDir := filepath.Join(rootDir, "install")
	if err := os.MkdirAll(exeDir, 0o755); err != nil {
		t.Fatalf("не удалось подготовить каталог exe: %v", err)
	}
	exePath := filepath.Join(exeDir, "XenionAgent.exe")
	if err := os.WriteFile(filepath.Join(exeDir, "agent-update.cmd"), []byte("@echo off"), 0o644); err != nil {
		t.Fatalf("не удалось создать agent-update.cmd: %v", err)
	}
	if err := os.WriteFile(exePath+".bak", []byte("backup"), 0o644); err != nil {
		t.Fatalf("не удалось создать backup exe: %v", err)
	}

	registry := &stubRegistryCleaner{}
	cleaner := &dataCleaner{
		cfg: config.Config{
			AgentProcessName: "XenionAgent",
			RegistryPath:     `Software\MyHoreca\XenionAgent`,
			DataDir:          dataDir,
		},
		registry: registry,
		executablePath: func() (string, error) {
			return exePath, nil
		},
		removeAll: os.RemoveAll,
		remove:    os.Remove,
	}

	if err := cleaner.Cleanup(); err != nil {
		t.Fatalf("Cleanup завершился ошибкой: %v", err)
	}
	if registry.deleteAllCalls != 1 {
		t.Fatalf("ожидался один вызов очистки реестра, получено %d", registry.deleteAllCalls)
	}
	if _, err := os.Stat(dataDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("каталог данных агента должен быть удален, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(exeDir, "agent-update.cmd")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("agent-update.cmd должен быть удален, stat err=%v", err)
	}
	if _, err := os.Stat(exePath + ".bak"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup exe должен быть удален, stat err=%v", err)
	}
}

func TestDataCleanerCleanupRejectsUnsafeDataDir(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	registry := &stubRegistryCleaner{}
	removeAllCalled := false

	cleaner := &dataCleaner{
		cfg: config.Config{
			AgentProcessName: "XenionAgent",
			RegistryPath:     `Software\MyHoreca\XenionAgent`,
			DataDir:          filepath.Join(rootDir, "ProgramData", "MyHoreca"),
		},
		registry: registry,
		executablePath: func() (string, error) {
			return filepath.Join(rootDir, "XenionAgent.exe"), nil
		},
		removeAll: func(path string) error {
			removeAllCalled = true
			return os.RemoveAll(path)
		},
		remove: os.Remove,
	}

	err := cleaner.Cleanup()
	if err == nil {
		t.Fatal("ожидалась ошибка для небезопасного каталога данных")
	}
	if !strings.Contains(err.Error(), "небезопасный каталог данных") {
		t.Fatalf("ожидалась ошибка про небезопасный каталог, получено: %v", err)
	}
	if removeAllCalled {
		t.Fatal("удаление каталога данных не должно вызываться для небезопасного пути")
	}
	if registry.deleteAllCalls != 0 {
		t.Fatalf("очистка реестра не должна вызываться при небезопасном каталоге, получено %d", registry.deleteAllCalls)
	}
}
