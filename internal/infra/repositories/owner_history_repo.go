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

func (r *ownerHistoryRepo) ListByEntitiesAndSources(
	ctx context.Context,
	entityTypes []string,
	entityIDs []string,
	sources []string,
	limit int,
) ([]models.OwnerChangeHistory, error) {
	if limit <= 0 {
		limit = 500
	}
	trimmedEntityTypes := make([]string, 0, len(entityTypes))
	for _, item := range entityTypes {
		value := strings.TrimSpace(item)
		if value != "" {
			trimmedEntityTypes = append(trimmedEntityTypes, value)
		}
	}
	trimmedEntityIDs := make([]string, 0, len(entityIDs))
	for _, item := range entityIDs {
		value := strings.TrimSpace(item)
		if value != "" {
			trimmedEntityIDs = append(trimmedEntityIDs, value)
		}
	}
	trimmedSources := make([]string, 0, len(sources))
	for _, item := range sources {
		value := strings.TrimSpace(item)
		if value != "" {
			trimmedSources = append(trimmedSources, value)
		}
	}
	if len(trimmedEntityTypes) == 0 || len(trimmedEntityIDs) == 0 || len(trimmedSources) == 0 {
		return []models.OwnerChangeHistory{}, nil
	}

	var items []models.OwnerChangeHistory
	err := r.db.WithContext(ctx).
		Where("entity_type IN ? AND entity_id IN ? AND change_source IN ?", trimmedEntityTypes, trimmedEntityIDs, trimmedSources).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
