// internal/repositories/task_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	"time"

	"gorm.io/gorm"
)

// TaskRepo определяет интерфейс для работы с хранилищем задач.
type TaskRepo interface {
	GetByID(ctx context.Context, id uint) (*models.ReconciliationTask, error) // <-- ДОБАВЛЕНО
	FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error)
	FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error)
	FindRecentlyResolvedTask(ctx context.Context, taskType, entityUUID string, window time.Duration) (*models.ReconciliationTask, error)
}

type taskRepo struct {
	db *gorm.DB
}

// NewTaskRepo создает новый экземпляр репозитория.
func NewTaskRepo(db *gorm.DB) TaskRepo {
	return &taskRepo{db: db}
}

// FindActiveTask ищет активную задачу по типу и UUID связанной сущности.
// Активной считается задача, которая не находится в конечных статусах (resolved, rejected).
func (r *taskRepo) FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error) {
	if taskType == "" || entityUUID == "" {
		return nil, nil
	}

	var task models.ReconciliationTask
	// ИЗМЕНЕНИЕ: Используем NOT IN для исключения закрытых задач
	err := r.db.WithContext(ctx).
		Where("task_type = ? AND entity_uuid = ? AND status NOT IN ?",
			taskType, entityUUID, []string{"resolved", "rejected", "closed"}).
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

// GetByID находит задачу по ее первичному ключу (ID).
func (r *taskRepo) GetByID(ctx context.Context, id uint) (*models.ReconciliationTask, error) {
	var task models.ReconciliationTask
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &task, err
}

// FindRecentlyResolvedTask ищет решенную задачу в заданном временном окне.
// Это нужно, чтобы найти исходную задачу add_equipment после того, как сущность была создана в SD и пришла через шлюз.
func (r *taskRepo) FindRecentlyResolvedTask(ctx context.Context, taskType, entityUUID string, window time.Duration) (*models.ReconciliationTask, error) {
	if taskType == "" || entityUUID == "" {
		return nil, nil
	}

	var task models.ReconciliationTask
	// Ищем задачу, которая была решена (updated_at) не так давно
	err := r.db.WithContext(ctx).
		Where("task_type = ? AND entity_uuid = ? AND status = 'resolved' AND updated_at > ?", taskType, entityUUID, time.Now().Add(-window)).
		Order("updated_at DESC"). // Берем самую свежую
		First(&task).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}
