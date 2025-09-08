// internal/repositories/fr_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// FiscalRegisterRepo определяет интерфейс для работы с хранилищем фискальных регистраторов.
type FiscalRegisterRepo interface {
	Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*models.FiscalRegister, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*models.FiscalRegister, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error)
	FindBySerialNumber(ctx context.Context, sn string) (*models.FiscalRegister, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.FiscalRegister, error)
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

// Create создает новый фискальный регистратор в базе данных.
func (r *frRepo) Create(ctx context.Context, tx *gorm.DB, fr *models.FiscalRegister) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(fr).Error
}

// Update обновляет данные фискального регистратора по его внутреннему ID.
func (r *frRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.FiscalRegister{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" фискального регистратора по его внутреннему ID.
func (r *frRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("id = ?", internalID).Delete(&models.FiscalRegister{})
	return res.RowsAffected > 0, res.Error
}

// GetByID находит фискальный регистратор по его внутреннему ID.
func (r *frRepo) GetByID(ctx context.Context, internalID string) (*models.FiscalRegister, error) {
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Where("id = ?", internalID).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

// GetByIDUnscoped находит запись по внутреннему ID, включая "мягко удаленные".
func (r *frRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*models.FiscalRegister, error) {
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

// GetAllIDsAndDates извлекает все внутренние ID, даты модификации и статусы удаления.
func (r *frRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*models.FiscalRegister, error) {
	var frs []*models.FiscalRegister
	if err := r.db.WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&frs).Error; err != nil {
		return nil, err
	}
	frMap := make(map[string]*models.FiscalRegister, len(frs))
	for _, fr := range frs {
		frMap[fr.ID] = fr
	}
	return frMap, nil
}

// Search выполняет поиск фискальных регистраторов по текстовому запросу.
func (r *frRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.FiscalRegister, error) {
	var frs []models.FiscalRegister
	err := r.db.WithContext(ctx).
		Where("rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&frs).Error
	return frs, err
}

// FindBySerialNumber ищет фискальный регистратор по серийному номеру.
func (r *frRepo) FindBySerialNumber(ctx context.Context, sn string) (*models.FiscalRegister, error) {
	if sn == "" {
		return nil, nil
	}
	var fr models.FiscalRegister
	err := r.db.WithContext(ctx).Where("fr_serial_number = ?", sn).Order("updated_at DESC").First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

// FindByOwnerIDs находит все фискальные регистраторы, принадлежащие указанным владельцам.
func (r *frRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.FiscalRegister, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var frs []models.FiscalRegister
	err := r.db.WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&frs).Error
	return frs, err
}
