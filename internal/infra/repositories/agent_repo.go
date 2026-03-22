package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainRepos "etalon-server/internal/domain/repositories"
	"strings"
	"time"

	"gorm.io/gorm"
)

type agentRepo struct {
	db *gorm.DB
}

// NewAgentRepo создает новый экземпляр репозитория агентов.
func NewAgentRepo(db *gorm.DB) domainRepos.AgentRepo {
	return &agentRepo{db: db}
}

func (r *agentRepo) GetByUUID(ctx context.Context, uuid string) (*models.Agent, error) {
	var agent models.Agent
	err := r.db.WithContext(ctx).Where("uuid = ?", uuid).First(&agent).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &agent, err
}

func (r *agentRepo) Create(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Create(agent).Error
}

func (r *agentRepo) Update(ctx context.Context, agent *models.Agent) error {
	return r.db.WithContext(ctx).Save(agent).Error
}

func (r *agentRepo) CountByOwnerUUID(ctx context.Context, ownerUUID string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&models.Agent{}).Where("owner_service_desk_uuid = ?", ownerUUID).Count(&count).Error
	return count, err
}

func (r *agentRepo) GetPendingCommands(ctx context.Context, agentUUID string) ([]models.AgentCommand, error) {
	var commands []models.AgentCommand
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

func (r *agentRepo) CompleteCommands(ctx context.Context, agentUUID string, results []domainRepos.AgentCommandResult) error {
	if len(results) == 0 {
		return nil
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now().UTC()
		for _, item := range results {
			if item.ID == 0 {
				continue
			}

			completedAt := item.CompletedAt.UTC()
			if completedAt.IsZero() {
				completedAt = now
			}

			if err := tx.Model(&models.AgentCommand{}).
				Where("id = ? AND agent_uuid = ? AND status IN ?", item.ID, agentUUID, []string{"new", "sent"}).
				Updates(map[string]interface{}{
					"status":         normalizeCommandResultStatus(item.Status),
					"completed_at":   completedAt,
					"result_payload": item.ResultPayload,
					"updated_at":     now,
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeCommandResultStatus(status string) string {
	if strings.EqualFold(strings.TrimSpace(status), "completed") {
		return "completed"
	}
	return "failed"
}
