package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoad_ЧитаетВнешнийJSONКонфиг(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)

	configPath := writeTestConfig(t, t.TempDir(), "custom-agent.json", `{
		"brand_name": "MyHoreca_Xenion",
		"server_url": "http://example.test:8080/",
		"bootstrap_api_key": "test-bootstrap-key",
		"debug": true,
		"heartbeat_interval": "20s",
		"retry_jitter_factor": 0.15
	}`)

	cfg, err := Load("1.2.3", LoadOptions{ConfigPath: configPath})
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.ConfigSource != configPath {
		t.Fatalf("ConfigSource = %q, ожидается %q", cfg.ConfigSource, configPath)
	}
	if cfg.BrandCompany != "MyHoreca" {
		t.Fatalf("BrandCompany = %q, ожидается %q", cfg.BrandCompany, "MyHoreca")
	}
	if cfg.BrandProduct != "Xenion" {
		t.Fatalf("BrandProduct = %q, ожидается %q", cfg.BrandProduct, "Xenion")
	}
	if cfg.AgentProcessName != "XenionAgent" {
		t.Fatalf("AgentProcessName = %q, ожидается %q", cfg.AgentProcessName, "XenionAgent")
	}
	if cfg.RegistryPath != `Software\MyHoreca\XenionAgent` {
		t.Fatalf("RegistryPath = %q, ожидается %q", cfg.RegistryPath, `Software\MyHoreca\XenionAgent`)
	}
	if cfg.DataDir != filepath.Join(programData, "MyHoreca", "XenionAgent") {
		t.Fatalf("DataDir = %q", cfg.DataDir)
	}
	if cfg.AdapterDir != filepath.Join(programData, "MyHoreca", "XenionAgent", "adapters") {
		t.Fatalf("AdapterDir = %q", cfg.AdapterDir)
	}
	if cfg.ServerURL != "http://example.test:8080" {
		t.Fatalf("ServerURL = %q, ожидается %q", cfg.ServerURL, "http://example.test:8080")
	}
	if !cfg.Debug {
		t.Fatal("Debug должен быть включен")
	}
	if cfg.HeartbeatInterval != 20*time.Second {
		t.Fatalf("HeartbeatInterval = %s, ожидается %s", cfg.HeartbeatInterval, 20*time.Second)
	}
	if cfg.RetryJitterFactor != 0.15 {
		t.Fatalf("RetryJitterFactor = %v, ожидается %v", cfg.RetryJitterFactor, 0.15)
	}
}

func TestLoad_ИщетКонфигЧерезПеременнуюОкружения(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)

	configDir := t.TempDir()
	configPath := writeTestConfig(t, configDir, "env-agent.json", `{
		"brand_name": "Demo_Product",
		"server_url": "https://env.example.test",
		"bootstrap_api_key": "env-key"
	}`)
	t.Setenv(ConfigPathEnv, configPath)

	cfg, err := Load("2.0.0", LoadOptions{
		ExecutablePath: filepath.Join(t.TempDir(), "bin", "etalon-agent.exe"),
	})
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.ConfigSource != configPath {
		t.Fatalf("ConfigSource = %q, ожидается %q", cfg.ConfigSource, configPath)
	}
	if cfg.ServerURL != "https://env.example.test" {
		t.Fatalf("ServerURL = %q, ожидается %q", cfg.ServerURL, "https://env.example.test")
	}
}

func TestLoad_ИщетКонфигРядомСИсполняемымФайлом(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)
	t.Setenv(ConfigPathEnv, "")

	executableDir := filepath.Join(t.TempDir(), "bin")
	configPath := writeTestConfig(t, executableDir, DefaultConfigFileName, `{
		"brand_name": "Demo_Product",
		"server_url": "https://file.example.test/",
		"bootstrap_api_key": "file-key",
		"inventory_interval": "10m"
	}`)

	cfg, err := Load("3.0.0", LoadOptions{
		ExecutablePath: filepath.Join(executableDir, "etalon-agent.exe"),
	})
	if err != nil {
		t.Fatalf("Load вернул ошибку: %v", err)
	}

	if cfg.ConfigSource != configPath {
		t.Fatalf("ConfigSource = %q, ожидается %q", cfg.ConfigSource, configPath)
	}
	if cfg.InventoryInterval != 10*time.Minute {
		t.Fatalf("InventoryInterval = %s, ожидается %s", cfg.InventoryInterval, 10*time.Minute)
	}
}

func TestLoad_СоздаетШаблонКонфигаРядомСИсполняемымФайломЕслиЕгоНет(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)
	t.Setenv(ConfigPathEnv, "")

	executableDir := filepath.Join(t.TempDir(), "bin")
	configPath, absErr := filepath.Abs(filepath.Join(executableDir, DefaultConfigFileName))
	if absErr != nil {
		t.Fatalf("не удалось получить абсолютный путь для шаблона конфига: %v", absErr)
	}

	_, err := Load("3.0.0", LoadOptions{
		ExecutablePath: filepath.Join(executableDir, "etalon-agent.exe"),
	})
	if err == nil {
		t.Fatal("Load должен был вернуть ошибку после создания шаблона конфига")
	}
	if !errors.Is(err, ErrDefaultConfigCreated) {
		t.Fatalf("ожидалась ошибка ErrDefaultConfigCreated, получено: %v", err)
	}

	var createdErr *DefaultConfigCreatedError
	if !errors.As(err, &createdErr) {
		t.Fatalf("ожидалась ошибка *DefaultConfigCreatedError, получено %T", err)
	}
	if createdErr.Path != configPath {
		t.Fatalf("созданный путь = %q, ожидается %q", createdErr.Path, configPath)
	}

	raw, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("не удалось прочитать созданный шаблон конфига: %v", readErr)
	}
	if string(raw) == "" {
		t.Fatal("созданный шаблон конфига не должен быть пустым")
	}
	if !containsAll(string(raw), `"brand_name": "Company_Product"`, `"server_url": ""`, `"bootstrap_api_key": ""`) {
		t.Fatalf("созданный шаблон конфига содержит неожиданные данные: %s", string(raw))
	}
}

func TestLoad_ВозвращаетОшибкуПриПустомBootstrapAPIKey(t *testing.T) {
	programData := filepath.Join(t.TempDir(), "ProgramData")
	t.Setenv("ProgramData", programData)

	configPath := writeTestConfig(t, t.TempDir(), "bad-agent.json", `{
		"brand_name": "Demo_Product",
		"server_url": "https://bad.example.test"
	}`)

	if _, err := Load("1.0.0", LoadOptions{ConfigPath: configPath}); err == nil {
		t.Fatal("Load должен был вернуть ошибку для пустого bootstrap_api_key")
	}
}

func writeTestConfig(t *testing.T, dir string, fileName string, body string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("не удалось создать каталог %s: %v", dir, err)
	}

	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("не удалось записать тестовый конфиг %s: %v", path, err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("не удалось получить абсолютный путь для %s: %v", path, err)
	}
	return filepath.Clean(absPath)
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
