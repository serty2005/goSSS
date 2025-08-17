package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// FiscalRegisterRepo определяет интерфейс для работы с хранилищем фискальных регистраторов.
type FiscalRegisterRepo interface {
	Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.FiscalRegister, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error)
}

// frRepo реализует интерфейс FiscalRegisterRepo.
type frRepo struct {
	db *gorm.DB
}

// NewFiscalRegisterRepo создает новый экземпляр репозитория.
func NewFiscalRegisterRepo(db *gorm.DB) FiscalRegisterRepo {
	return &frRepo{db: db}
}

func (r *frRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *frRepo) Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(fr).Error
}

func (r *frRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.FiscalRegister{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *frRepo) GetByUUID(ctx context.Context, uuid string) (*models.FiscalRegister, error) {
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

func (r *frRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error) {
	var frs []*models.FiscalRegister
	if err := r.db.WithContext(ctx).Select("service_desk_uuid", "last_modified_date").Find(&frs).Error; err != nil {
		return nil, err
	}
	frMap := make(map[string]*models.FiscalRegister, len(frs))
	for _, fr := range frs {
		if fr.ServiceDeskUUID != nil {
			frMap[*fr.ServiceDeskUUID] = fr
		}
	}
	return frMap, nil
}

func (r *frRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error) {
	var frs []models.FiscalRegister
	err := r.db.WithContext(ctx).
		Where("rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&frs).Error
	return frs, err
}
