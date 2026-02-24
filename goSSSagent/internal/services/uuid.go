package services

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const uuidFileName = "agent.uuid"

type UUIDService struct {
	uuid     string
	filePath string
}

func NewUUIDService(dataDir string) (*UUIDService, error) {
	if strings.TrimSpace(dataDir) == "" {
		exePath, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("не удалось определить путь к исполняемому файлу: %w", err)
		}
		dataDir = filepath.Dir(exePath)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог данных агента: %w", err)
	}

	s := &UUIDService{filePath: filepath.Join(dataDir, uuidFileName)}
	if err := s.loadOrGenerate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *UUIDService) Get() string {
	return s.uuid
}

func (s *UUIDService) loadOrGenerate() error {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			newUUID := uuid.New().String()
			if err := os.WriteFile(s.filePath, []byte(newUUID), 0o644); err != nil {
				return fmt.Errorf("не удалось сохранить UUID в файл %s: %w", s.filePath, err)
			}
			s.uuid = newUUID
			log.Printf("UUID агента создан: %s", s.uuid)
			return nil
		}
		return fmt.Errorf("не удалось прочитать файл UUID %s: %w", s.filePath, err)
	}

	parsedUUID, err := uuid.Parse(strings.TrimSpace(string(data)))
	if err != nil {
		return fmt.Errorf("файл UUID содержит некорректные данные: %w", err)
	}
	s.uuid = parsedUUID.String()
	log.Printf("UUID агента загружен: %s", s.uuid)
	return nil
}
