// internal/repositories/task_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	"time"
)

// TaskRepo определяет интерфейс для работы с хранилищем задач.
type TaskRepo interface {
	GetByID(ctx context.Context, id uint) (*models.ReconciliationTask, error)
	FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error)
	FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error)
	FindRecentlyResolvedTask(ctx context.Context, taskType, entityUUID string, window time.Duration) (*models.ReconciliationTask, error)
}
