// internal/repositories/task_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// TaskRepo определяет интерфейс для работы с хранилищем задач.
type TaskRepo interface {
	FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error)
	FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error)
}

type taskRepo struct {
	db *gorm.DB
}

// NewTaskRepo создает новый экземпляр репозитория.
func NewTaskRepo(db *gorm.DB) TaskRepo {
	return &taskRepo{db: db}
}

// FindActiveTask ищет активную задачу по типу и UUID связанной сущности.
func (r *taskRepo) FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error) {
	if taskType == "" || entityUUID == "" {
		// Не ищем задачи без ключевых идентификаторов
		return nil, nil
	}

	var task models.ReconciliationTask
	err := r.db.WithContext(ctx).
		Where("task_type = ? AND entity_uuid = ? AND status = 'new'", taskType, entityUUID).
		First(&task).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// FindActiveDuplicateTaskByMemberUUIDs ищет активную задачу 'resolve_duplicate',
// в которой участвует хотя бы одна из переданных сущностей.
func (r *taskRepo) FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	var task models.ReconciliationTask

	// Этот запрос более сложный, но более надежный для GORM.
	// Он "разворачивает" JSON-массив entityUUIDs в строки и проверяет,
	// есть ли среди них хотя бы одно совпадение с нашим списком uuids.
	subQuery := r.db.Model(&models.ReconciliationTask{}).
		Select("id").
		Joins("join jsonb_array_elements_text(details->'entityUUIDs') as uuid on true").
		Where("task_type = ? AND status = 'new'", "resolve_duplicate").
		Where("uuid IN ?", uuids)

	err := r.db.WithContext(ctx).
		Where("id IN (?)", subQuery).
		First(&task).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}
