package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	"time"

	"gorm.io/gorm"
)

// AgentRepo определяет интерфейс для работы с хранилищем агентов.
type AgentRepo interface {
	GetByUUID(ctx context.Context, uuid string) (*models.Agent, error)
	Create(ctx context.Context, agent *models.Agent) error
	Update(ctx context.Context, agent *models.Agent) error
	CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error)

	GetPendingCommands(ctx context.Context, agentUUID string) ([]models.AgentCommand, error)
	MarkCommandsAsSent(ctx context.Context, commandIDs []uint) error
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

func (r *agentRepo) GetPendingCommands(ctx context.Context, agentUUID string) ([]models.AgentCommand, error) {
	var commands []models.AgentCommand
	// Получаем команды со статусом 'new' для данного агента
	err := r.db.WithContext(ctx).
		Where("agent_uuid = ? AND status = ?", agentUUID, "new").
		Order("created_at ASC").
		Find(&commands).Error
	return commands, err
}

func (r *agentRepo) MarkCommandsAsSent(ctx context.Context, commandIDs []uint) error {
	if len(commandIDs) == 0 {
		return nil
	}
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.AgentCommand{}).
		Where("id IN ?", commandIDs).
		Updates(map[string]interface{}{
			"status":  "sent",
			"sent_at": now,
		}).Error
}
