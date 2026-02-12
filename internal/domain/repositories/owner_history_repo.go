package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
)

type OwnerHistoryRepo interface {
	Create(ctx context.Context, event *models.OwnerChangeHistory) error
}
