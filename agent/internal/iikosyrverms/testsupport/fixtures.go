package testsupport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

type fixture struct {
	Entries []fixtureEntry `json:"entries"`
}

type fixtureEntry struct {
	Type    string `json:"type"`
	Path    string `json:"path"`
	Content string `json:"content,omitempty"`
	ModTime string `json:"mod_time"`
}

func MaterializeAppDataFixture(rootDir, name string) error {
	path := filepath.Join(fixturesDir(), name+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("не удалось прочитать фикстуру %q: %w", name, err)
	}

	var data fixture
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("не удалось разобрать фикстуру %q: %w", name, err)
	}

	for _, entry := range data.Entries {
		fullPath := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
		switch strings.ToLower(strings.TrimSpace(entry.Type)) {
		case "dir":
			if err := os.MkdirAll(fullPath, 0o755); err != nil {
				return fmt.Errorf("не удалось создать каталог %q: %w", fullPath, err)
			}
		case "file":
			if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
				return fmt.Errorf("не удалось создать каталог файла %q: %w", fullPath, err)
			}
			if err := os.WriteFile(fullPath, []byte(entry.Content), 0o644); err != nil {
				return fmt.Errorf("не удалось записать файл %q: %w", fullPath, err)
			}
		default:
			return fmt.Errorf("неподдерживаемый тип записи %q в фикстуре %q", entry.Type, name)
		}
	}

	entries := slices.Clone(data.Entries)
	slices.SortFunc(entries, func(a, b fixtureEntry) int {
		if a.Type != b.Type {
			if a.Type == "file" {
				return -1
			}
			return 1
		}
		aDepth := strings.Count(filepath.Clean(filepath.FromSlash(a.Path)), string(os.PathSeparator))
		bDepth := strings.Count(filepath.Clean(filepath.FromSlash(b.Path)), string(os.PathSeparator))
		if aDepth != bDepth {
			if aDepth > bDepth {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Path), strings.ToLower(b.Path))
	})

	for _, entry := range entries {
		modTime, err := time.Parse(time.RFC3339, entry.ModTime)
		if err != nil {
			return fmt.Errorf("не удалось разобрать mod_time %q: %w", entry.ModTime, err)
		}
		fullPath := filepath.Join(rootDir, filepath.FromSlash(entry.Path))
		if err := os.Chtimes(fullPath, modTime, modTime); err != nil {
			return fmt.Errorf("не удалось выставить время для %q: %w", fullPath, err)
		}
	}

	return nil
}

func fixturesDir() string {
	_, currentFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(currentFile), "..", "testdata", "fixtures")
}
