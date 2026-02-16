package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

type OwnerHistoryRepo interface {
	Create(ctx context.Context, event *models.OwnerChangeHistory) error
	ListByEntity(ctx context.Context, entityType, entityID string, limit int) ([]models.OwnerChangeHistory, error)
}
