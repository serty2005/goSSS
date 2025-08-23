// internal/repositories/server_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"
	"time"

	"gorm.io/gorm"
)

// ServerRepo определяет интерфейс для работы с хранилищем серверов.
type ServerRepo interface {
	Create(ctx context.Context, tx *gorm.DB, server *models.Server) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Server, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Server, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error)
	FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error)
	FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Server, error)
	FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error) // НОВЫЙ МЕТОД
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

func (r *serverRepo) Create(ctx context.Context, tx *gorm.DB, server *models.Server) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(server).Error
}

func (r *serverRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Server{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" сервера по его ServiceDesk UUID.
func (r *serverRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Server{})
	return res.RowsAffected > 0, res.Error
}

func (r *serverRepo) GetByUUID(ctx context.Context, uuid string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}
func (r *serverRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}

func (r *serverRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error) {
	var servers []*models.Server
	if err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&servers).Error; err != nil {
		return nil, err
	}
	serverMap := make(map[string]*models.Server, len(servers))
	for _, s := range servers {
		if s.ServiceDeskUUID != nil {
			serverMap[*s.ServiceDeskUUID] = s
		}
	}
	return serverMap, nil
}

func (r *serverRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error) {
	var servers []models.Server
	err := r.db.WithContext(ctx).
		Where("device_name ILIKE ? OR ip ILIKE ? OR unique_id ILIKE ? OR description ILIKE ? OR server_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&servers).Error
	return servers, err
}

// FindForPolling находит серверы, которые необходимо опросить.
// Выбирает серверы, которые еще не опрашивались или чья последняя проверка была раньше, чем `interval` назад.
func (r *serverRepo) FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]models.Server, error) {
	var servers []models.Server
	threshold := time.Now().Add(-interval)

	err := r.db.WithContext(ctx).
		Where("ip IS NOT NULL AND ip != ''").
		Where("status != ?", "archived").
		Where("last_polled_at IS NULL OR last_polled_at < ?", threshold).
		Limit(limit).
		Order("last_polled_at ASC"). // Начинаем с самых старых
		Find(&servers).Error
	return servers, err
}

// FindByCRMidOrIP ищет сервер по CRMid (приоритет) или по IP.
func (r *serverRepo) FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*models.Server, error) {
	var server models.Server

	// CRMid является более надежным идентификатором
	if crmid != "" {
		err := r.db.WithContext(ctx).Where("crm_id = ?", crmid).First(&server).Error
		if err == nil {
			return &server, nil
		}
		if err != gorm.ErrRecordNotFound {
			return nil, err
		}
	}

	// Если по CRMid не нашли, ищем по IP с точным совпадением
	if ip != "" {
		err := r.db.WithContext(ctx).Where("ip = ?", ip).First(&server).Error
		if err == gorm.ErrRecordNotFound {
			return nil, nil // Явно возвращаем nil, если не найдено
		}
		return &server, err
	}

	return nil, nil
}

// FindByOwnerUUIDs находит все серверы, принадлежащие указанным владельцам.
func (r *serverRepo) FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Server, error) {
	if len(ownerUUIDs) == 0 {
		return nil, nil
	}
	var servers []models.Server
	err := r.db.WithContext(ctx).Where("owner_service_desk_uuid IN ?", ownerUUIDs).Find(&servers).Error
	return servers, err
}
