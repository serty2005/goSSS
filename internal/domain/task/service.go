package task

import (
	"context"
	"etalon-server/internal/domain/models"
)

// DuplicateGroup представляет группу дубликатов для сервиса.
type DuplicateGroup struct {
	Field      string
	Value      string
	MainRecord interface{}
	Duplicates []interface{}
	EntityType string
}

// Service определяет интерфейс для сервиса задач.
type Service interface {
	// GetTasks возвращает список задач с фильтрацией.
	GetTasks(ctx context.Context, status string, limit, offset int) ([]models.ReconciliationTask, error)
	// GetDuplicates возвращает группы дубликатов.
	GetDuplicates(ctx context.Context) ([]DuplicateGroup, error)
}
