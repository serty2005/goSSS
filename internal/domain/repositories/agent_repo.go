package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
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
