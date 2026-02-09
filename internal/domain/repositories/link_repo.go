// internal/repositories/link_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

type linkRepo struct {
	db *gorm.DB
}

// NewLinkRepo создает новый экземпляр репозитория.
func NewLinkRepo(db *gorm.DB) LinkRepo {
	return &linkRepo{db: db}
}

func (r *linkRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// GetByExternalID находит связь по внешнему ID.
func (r *linkRepo) GetByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (*models.ExternalSystemLink, error) {
	var link models.ExternalSystemLink
	err := r.dbOrTx(tx).WithContext(ctx).
		Where("system_name = ? AND service_desk_uuid = ?", systemName, externalID).
		First(&link).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &link, err
}

// GetByInternalID находит связь по внутреннему ID.
func (r *linkRepo) GetByInternalID(ctx context.Context, tx *gorm.DB, systemName, internalID string) (*models.ExternalSystemLink, error) {
	var link models.ExternalSystemLink
	err := r.dbOrTx(tx).WithContext(ctx).
		Where("system_name = ? AND internal_id = ?", systemName, internalID).
		First(&link).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &link, err
}

// Create создает новую запись о связи.
func (r *linkRepo) Create(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(link).Error
}

// Upsert выполняет идемпотентную запись/обновление внешней связи.
func (r *linkRepo) Upsert(ctx context.Context, tx *gorm.DB, link *models.ExternalSystemLink) error {
	return r.dbOrTx(tx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "system_name"}, {Name: "service_desk_uuid"}},
		DoUpdates: clause.AssignmentColumns([]string{"internal_id", "entity_type", "last_synced_at"}),
	}).Create(link).Error
}

// DeleteByInternalID удаляет связь по внутреннему ID.
func (r *linkRepo) DeleteByInternalID(ctx context.Context, tx *gorm.DB, systemName, internalID string) error {
	return r.dbOrTx(tx).WithContext(ctx).
		Where("system_name = ? AND internal_id = ?", systemName, internalID).
		Delete(&models.ExternalSystemLink{}).Error
}

// FindInternalIDByExternalID быстро находит внутренний ID по внешнему без загрузки всей структуры.
func (r *linkRepo) FindInternalIDByExternalID(ctx context.Context, tx *gorm.DB, systemName, externalID string) (string, error) {
	var internalID string
	err := r.dbOrTx(tx).WithContext(ctx).Model(&models.ExternalSystemLink{}).
		Where("system_name = ? AND service_desk_uuid = ?", systemName, externalID).
		Pluck("internal_id", &internalID).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return internalID, err
}
