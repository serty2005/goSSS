package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"strings"

	"gorm.io/gorm"
)

type ownerHistoryRepo struct {
	db *gorm.DB
}

func NewOwnerHistoryRepo(db *gorm.DB) domainrepos.OwnerHistoryRepo {
	return &ownerHistoryRepo{db: db}
}

func (r *ownerHistoryRepo) Create(ctx context.Context, event *models.OwnerChangeHistory) error {
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *ownerHistoryRepo) ListByEntity(ctx context.Context, entityType, entityID string, limit int) ([]models.OwnerChangeHistory, error) {
	if limit <= 0 {
		limit = 100
	}
	var items []models.OwnerChangeHistory
	err := r.db.WithContext(ctx).
		Where("entity_type = ? AND entity_id = ?", strings.TrimSpace(entityType), strings.TrimSpace(entityID)).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
