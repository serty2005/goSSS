package pluginscanner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScannerReadsManifestAndUsesVersionReader(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pluginDir := filepath.Join(root, "Resto.Front.Api.Transport.V9Preview7")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("не удалось создать каталог плагина: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "manifest.xml"), []byte(`<Manifest><FileName>Resto.Front.Api.Transport.dll</FileName><ApiVersion>V9Preview7</ApiVersion></Manifest>`), 0o644); err != nil {
		t.Fatalf("не удалось записать manifest.xml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "Resto.Front.Api.Transport.dll"), []byte("stub"), 0o644); err != nil {
		t.Fatalf("не удалось записать DLL: %v", err)
	}

	scanner := &Scanner{
		readVersion: func(path string) (string, error) {
			if filepath.Base(path) != "Resto.Front.Api.Transport.dll" {
				t.Fatalf("ожидался вызов для основной DLL, получено %q", path)
			}
			return "9.7.20", nil
		},
	}

	plugins, warnings, err := scanner.Scan(root)
	if err != nil {
		t.Fatalf("Scan вернул ошибку: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("не ожидались предупреждения, получено %v", warnings)
	}
	if len(plugins) != 1 {
		t.Fatalf("ожидался один плагин, получено %d", len(plugins))
	}
	if plugins[0].Name != "Transport" {
		t.Fatalf("ожидалось имя Transport, получено %q", plugins[0].Name)
	}
	if plugins[0].APIVersion != "V9Preview7" {
		t.Fatalf("ожидалась api_version V9Preview7, получено %q", plugins[0].APIVersion)
	}
	if plugins[0].Version != "9.7.20" {
		t.Fatalf("ожидалась версия 9.7.20, получено %q", plugins[0].Version)
	}
}

func TestScannerFallsBackWithoutManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	pluginDir := filepath.Join(root, "Plugin.Front.Api.Loyalty")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("не удалось создать каталог плагина: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "Plugin.Front.Api.Loyalty.dll"), []byte("stub"), 0o644); err != nil {
		t.Fatalf("не удалось записать DLL: %v", err)
	}

	scanner := &Scanner{
		readVersion: func(string) (string, error) {
			return "", nil
		},
	}

	plugins, warnings, err := scanner.Scan(root)
	if err != nil {
		t.Fatalf("Scan вернул ошибку: %v", err)
	}
	if len(plugins) != 1 {
		t.Fatalf("ожидался один плагин, получено %d", len(plugins))
	}
	if plugins[0].Name != "Loyalty" {
		t.Fatalf("ожидалось имя Loyalty, получено %q", plugins[0].Name)
	}
	if len(warnings) != 0 {
		t.Fatalf("не ожидались предупреждения, получено %v", warnings)
	}
}
