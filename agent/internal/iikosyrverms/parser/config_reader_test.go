package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadConfigFileBuildsSettingsTreeAndPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "config.xml")
	content := `<configuration env="prod"><serverUrl>https://demo.iiko.local/resto/</serverUrl><endpoints><endpoint kind="main"><url>https://one.local</url></endpoint><endpoint kind="backup"><url>https://two.local</url></endpoint></endpoints><empty-node enabled="true"></empty-node></configuration>`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("не удалось записать config.xml: %v", err)
	}

	snapshot, err := ReadConfigFile(path)
	if err != nil {
		t.Fatalf("ReadConfigFile вернул ошибку: %v", err)
	}
	if snapshot.RootElement != "configuration" {
		t.Fatalf("ожидался корневой элемент configuration, получено %q", snapshot.RootElement)
	}
	if snapshot.ServerURL != "https://demo.iiko.local/resto/" {
		t.Fatalf("ожидался serverUrl, получено %q", snapshot.ServerURL)
	}
	if !snapshot.HasRepeatedNodes {
		t.Fatal("ожидался признак повторяющихся узлов")
	}
	if snapshot.Tree == nil {
		t.Fatal("ожидалось дерево конфигурации")
	}

	paths := make(map[string]ConfigSettingView, len(snapshot.Settings))
	for _, item := range snapshot.Settings {
		paths[item.Path] = ConfigSettingView(item)
	}

	if _, ok := paths["/configuration/endpoints/endpoint[0]/url"]; !ok {
		t.Fatal("не найден путь для первого повторяющегося endpoint")
	}
	if _, ok := paths["/configuration/endpoints/endpoint[1]/url"]; !ok {
		t.Fatal("не найден путь для второго повторяющегося endpoint")
	}
	emptyNode, ok := paths["/configuration/empty-node"]
	if !ok {
		t.Fatal("не найден пустой узел с атрибутами")
	}
	if emptyNode.Attributes["enabled"] != "true" {
		t.Fatalf("ожидался атрибут enabled=true, получено %#v", emptyNode.Attributes)
	}
}

type ConfigSettingView struct {
	Path       string
	Name       string
	Value      string
	Attributes map[string]string
	ParentPath string
	Index      int
	Repeated   bool
}

func TestReadConfigFilesReturnsFreshestReadableFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	olderPath := filepath.Join(root, "older.xml")
	newerPath := filepath.Join(root, "newer.xml")

	if err := os.WriteFile(olderPath, []byte(`<configuration><serverUrl>https://older.local/resto/</serverUrl></configuration>`), 0o644); err != nil {
		t.Fatalf("не удалось записать older.xml: %v", err)
	}
	if err := os.WriteFile(newerPath, []byte(`<configuration><node /></configuration>`), 0o644); err != nil {
		t.Fatalf("не удалось записать newer.xml: %v", err)
	}

	newerTime := time.Date(2026, 3, 22, 10, 0, 0, 0, time.UTC)
	olderTime := time.Date(2026, 3, 18, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(olderPath, olderTime, olderTime); err != nil {
		t.Fatalf("не удалось выставить время older.xml: %v", err)
	}
	if err := os.Chtimes(newerPath, newerTime, newerTime); err != nil {
		t.Fatalf("не удалось выставить время newer.xml: %v", err)
	}

	snapshot, reason := ReadConfigFiles([]string{olderPath, newerPath})
	if snapshot.SourceFile != newerPath {
		t.Fatalf("ожидался самый свежий config.xml %q, получено %q", newerPath, snapshot.SourceFile)
	}
	if reason != "config.xml найден, но поле serverUrl отсутствует или пустое" {
		t.Fatalf("получено неожиданное сообщение: %q", reason)
	}
}
