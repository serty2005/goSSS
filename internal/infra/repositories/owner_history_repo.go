package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"

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
