package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"time"
)

type DuplicateValueCount struct {
	Value string
	Count int
}

// TaskRepo определяет интерфейс для работы с хранилищем задач.
type TaskRepo interface {
	GetByID(ctx context.Context, id uint) (*models.ReconciliationTask, error)
	List(ctx context.Context, status string, limit, offset int) ([]models.ReconciliationTask, error)
	Update(ctx context.Context, id uint, updates map[string]interface{}) (bool, error)

	FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error)
	FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error)
	FindRecentlyResolvedTask(ctx context.Context, taskType, entityUUID string, window time.Duration) (*models.ReconciliationTask, error)

	FindDuplicateValues(ctx context.Context, entityType, field string) ([]DuplicateValueCount, error)
	FindWorkstationsByFieldValue(ctx context.Context, field, value string) ([]workstation.Workstation, error)
	FindServersByFieldValue(ctx context.Context, field, value string) ([]server.Server, error)
}
