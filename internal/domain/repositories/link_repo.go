// internal/repositories/link_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"

	"gorm.io/gorm"
)

// LinkRepo определяет интерфейс для работы с хранилищем связей с внешними системами.
type LinkRepo interface {
	GetByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (*models.ExternalSystemLink, error)
	GetByInternalID(ctx context.Context, tx *gorm.DB, systemName, internalID string) (*models.ExternalSystemLink, error)
	Create(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error
	Upsert(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error
	DeleteByInternalID(ctx context.Context, tx *gorm.DB, systemName, internalID string) error
	FindInternalIDByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (string, error)
}
