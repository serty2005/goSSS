package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"time"

	"gorm.io/gorm"
)

type taskRepo struct {
	db *gorm.DB
}

// NewTaskRepo создает новый экземпляр репозитория задач.
func NewTaskRepo(db *gorm.DB) domainRepos.TaskRepo {
	return &taskRepo{db: db}
}

func (r *taskRepo) FindActiveTask(ctx context.Context, taskType, entityUUID string) (*models.ReconciliationTask, error) {
	if taskType == "" || entityUUID == "" {
		return nil, nil
	}

	var task models.ReconciliationTask
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

func (r *taskRepo) FindActiveDuplicateTaskByMemberUUIDs(ctx context.Context, uuids []string) (*models.ReconciliationTask, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	var task models.ReconciliationTask

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

func (r *taskRepo) GetByID(ctx context.Context, id uint) (*models.ReconciliationTask, error) {
	var task models.ReconciliationTask
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &task, err
}

func (r *taskRepo) FindRecentlyResolvedTask(ctx context.Context, taskType, entityUUID string, window time.Duration) (*models.ReconciliationTask, error) {
	if taskType == "" || entityUUID == "" {
		return nil, nil
	}

	var task models.ReconciliationTask
	err := r.db.WithContext(ctx).
		Where("task_type = ? AND entity_uuid = ? AND status = 'resolved' AND updated_at > ?", taskType, entityUUID, time.Now().Add(-window)).
		Order("updated_at DESC").
		First(&task).Error

	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}
