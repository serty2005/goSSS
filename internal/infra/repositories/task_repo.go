package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"fmt"
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

func (r *taskRepo) List(ctx context.Context, status string, limit, offset int) ([]models.ReconciliationTask, error) {
	var tasks []models.ReconciliationTask
	query := r.db.WithContext(ctx).Model(&models.ReconciliationTask{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	err := query.Limit(limit).Offset(offset).Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

func (r *taskRepo) Update(ctx context.Context, id uint, updates map[string]interface{}) (bool, error) {
	res := r.db.WithContext(ctx).Model(&models.ReconciliationTask{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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

func (r *taskRepo) FindDuplicateValues(ctx context.Context, entityType, field string) ([]domainRepos.DuplicateValueCount, error) {
	var model interface{}
	switch entityType {
	case "Workstation":
		model = &workstation.Workstation{}
	case "Server":
		model = &server.Server{}
	default:
		return nil, fmt.Errorf("неизвестный тип сущности: %s", entityType)
	}

	var results []domainRepos.DuplicateValueCount
	err := r.db.WithContext(ctx).Model(model).
		Select(fmt.Sprintf("%s as value, count(*) as count", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&results).Error
	return results, err
}

func (r *taskRepo) FindWorkstationsByFieldValue(ctx context.Context, field, value string) ([]workstation.Workstation, error) {
	var items []workstation.Workstation
	err := r.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), value).Find(&items).Error
	return items, err
}

func (r *taskRepo) FindServersByFieldValue(ctx context.Context, field, value string) ([]server.Server, error) {
	var items []server.Server
	err := r.db.WithContext(ctx).Where(fmt.Sprintf("%s = ?", field), value).Find(&items).Error
	return items, err
}
