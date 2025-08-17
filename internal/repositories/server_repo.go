package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// ServerRepo определяет интерфейс для работы с хранилищем серверов.
type ServerRepo interface {
	Create(ctx context.Context, tx *gorm.DB, server *models.Server) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Server, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Server, error)
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

func (r *serverRepo) GetByUUID(ctx context.Context, uuid string) (*models.Server, error) {
	var server models.Server
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&server).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &server, err
}

func (r *serverRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Server, error) {
	var servers []*models.Server
	if err := r.db.WithContext(ctx).Select("service_desk_uuid", "last_modified_date").Find(&servers).Error; err != nil {
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
		Where("device_name ILIKE ? OR ip ILIKE ? OR unique_id ILIKE ? OR description ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&servers).Error
	return servers, err
}
