package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Store interface {
	Load(context.Context, string) (*Journal, error)
	Save(context.Context, Journal) error
	Path(string) string
}

type FileStore struct {
	rootDir string
}

func NewFileStore(rootDir string) *FileStore {
	return &FileStore{rootDir: filepath.Clean(strings.TrimSpace(rootDir))}
}

func (s *FileStore) Load(_ context.Context, sagaID string) (*Journal, error) {
	path := s.Path(sagaID)
	if path == "" {
		return nil, fmt.Errorf("не удалось вычислить путь state-file для saga_id %q", sagaID)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("не удалось прочитать state-file %s: %w", path, err)
	}

	var journal Journal
	if err := json.Unmarshal(raw, &journal); err != nil {
		return nil, fmt.Errorf("не удалось распарсить state-file %s: %w", path, err)
	}
	cloned := cloneJournal(journal)
	return &cloned, nil
}

func (s *FileStore) Save(_ context.Context, journal Journal) error {
	path := s.Path(journal.Request.SagaID)
	if path == "" {
		return fmt.Errorf("не удалось вычислить путь state-file для saga_id %q", journal.Request.SagaID)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("не удалось создать каталог state-file %s: %w", filepath.Dir(path), err)
	}

	raw, err := json.MarshalIndent(cloneJournal(journal), "", "  ")
	if err != nil {
		return fmt.Errorf("не удалось сериализовать journal saga %s: %w", journal.Request.SagaID, err)
	}
	return writeFileAtomically(path, append(raw, '\n'), 0o644)
}

func (s *FileStore) Path(sagaID string) string {
	if strings.TrimSpace(s.rootDir) == "" || strings.TrimSpace(sagaID) == "" {
		return ""
	}
	return filepath.Join(s.rootDir, sanitizeFileName(sagaID)+".json")
}

func writeFileAtomically(path string, content []byte, perm os.FileMode) error {
	tmpFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-saga-*")
	if err != nil {
		return err
	}

	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}

	_ = os.Remove(path)
	return os.Rename(tmpPath, path)
}

func sanitizeFileName(value string) string {
	value = strings.TrimSpace(filepath.Base(value))
	replacer := strings.NewReplacer("..", "", "/", "-", "\\", "-", ":", "-", "*", "-", "?", "-", "\"", "-", "<", "-", ">", "-", "|", "-")
	value = replacer.Replace(value)
	if value == "" || value == "." {
		return "saga"
	}
	return value
}
