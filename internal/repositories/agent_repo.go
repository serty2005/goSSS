package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// AgentRepo определяет интерфейс для работы с хранилищем агентов.
type AgentRepo interface {
	GetByUUID(ctx context.Context, uuid string) (*models.Agent, error)
	Create(ctx context.Context, agent *models.Agent) error
	Update(ctx context.Context, agent *models.Agent) error
	CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error)
}

type agentRepo struct {
	db *gorm.DB
}

// NewAgentRepo создает новый экземпляр репозитория агентов.
func NewAgentRepo(db *gorm.DB) AgentRepo {
	return &agentRepo{db: db}
}

func (r *agentRepo) GetByUUID(ctx context.Context, uuid string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&agent).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil // Это не ошибка, а нормальный случай "не найдено"
	}
	return &agent, err
}

func (r *agentRepo) Create(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepo) Update(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

// CountByOwnerUUID подсчитывает количество агентов, принадлежащих одной компании.
func (r *agentRepo) CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Agent{}).Where("owner_service_desk_uuid = ?", ownerUUID).Count(&count).Error
	return count, err
}
