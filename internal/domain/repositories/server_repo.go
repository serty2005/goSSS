// internal/repositories/server_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"
	"time"

	"gorm.io/gorm"
)

// ServerRepo определяет интерфейс для работы с хранилищем серверов.
type ServerRepo interface {
	Create(ctx context.Context, tx *gorm.DB, server *models.Server) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*models.Server, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*models.Server, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*models.Server, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error)
	FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.Server, error)
	FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error)
}

type serverRepo struct {
	db *gorm.DB
}

// NewServerRepo создает новый экземпляр репозитория.
func NewServerRepo(db *gorm.DB) ServerRepo {
	return &serverRepo{db: db}
}

func (r *serverRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create создает новый сервер в базе данных.
func (r *serverRepo) Create(ctx context.Context, tx *gorm.DB, server *models.Server) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(server).Error
}

// Update обновляет данные сервера по его внутреннему ID.
func (r *serverRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Server{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" сервера по его внутреннему ID.
func (r *serverRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("id = ?", internalID).Delete(&models.Server{})
	return res.RowsAffected > 0, res.Error
}

// GetByID находит сервер по его внутреннему ID.
func (r *serverRepo) GetByID(ctx context.Context, internalID string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Preload("AdditionalOwners").Where("id = ?", internalID).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}

// GetByIDUnscoped находит сервер по внутреннему ID, включая "мягко удаленные".
func (r *serverRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Unscoped().Preload("AdditionalOwners").Where("id = ?", internalID).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}

// GetAllIDsAndDates извлекает все внутренние ID, даты модификации и статусы удаления.
func (r *serverRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*models.Server, error) {
	var servers []*models.Server
	if err := r.db.WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&servers).Error; err != nil {
		return nil, err
	}
	serverMap := make(map[string]*models.Server, len(servers))
	for _, s := range servers {
		serverMap[s.ID] = s
	}
	return serverMap, nil
}

// Search выполняет поиск серверов по текстовому запросу.
func (r *serverRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error) {
	var servers []models.Server
	err := r.db.WithContext(ctx).
		Where("device_name ILIKE ? OR ip ILIKE ? OR unique_id ILIKE ? OR description ILIKE ? OR server_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&servers).Error
	return servers, err
}

// FindForPolling находит серверы, которые необходимо опросить.
func (r *serverRepo) FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error) {
	var servers []models.Server
	threshold := time.Now().Add(-interval)

	err := r.db.WithContext(ctx).
		Where("ip IS NOT NULL AND ip != ''").
		Where("status NOT IN (?, ?)", "archived", "locked").
		Where("last_polled_at IS NULL OR last_polled_at < ?", threshold).
		Limit(limit).
		Order("last_polled_at ASC NULLS FIRST").
		Find(&servers).Error
	return servers, err
}

// FindByCRMidOrIP ищет сервер по CRMid (приоритет) или по IP.
func (r *serverRepo) FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error) {
	var server models.Server
	query := r.db.WithContext(ctx).Preload("AdditionalOwners")

	if crmid != "" {
		err := query.Where("crm_id = ? AND status != ?", crmid, "locked").First(&server).Error
		if err == nil {
			return &server, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	if ip != "" {
		err := query.Where("ip = ? AND status != ?", ip, "locked").First(&server).Error
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return &server, err
	}

	return nil, nil
}

// FindByOwnerIDs находит все серверы, принадлежащие указанным владельцам.
func (r *serverRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.Server, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var servers []models.Server
	err := r.db.WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&servers).Error
	return servers, err
}
