package repositories

import (
	"context"
	"etalon-server/internal/domain/fiscal" // <-- Новый импорт
	infraDB "etalon-server/internal/infra/db"

	"gorm.io/gorm"
)

type frRepo struct {
	db *gorm.DB
}

func NewFiscalRegisterRepo(db *gorm.DB) fiscal.Repository {
	return &frRepo{db: db}
}

func (r *frRepo) dbOrTx(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *frRepo) Create(ctx context.Context, tx *gorm.DB, fr *fiscal.FiscalRegister) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Create(fr).Error
}

func (r *frRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&fiscal.FiscalRegister{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *frRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Where("id = ?", internalID).Delete(&fiscal.FiscalRegister{})
	return res.RowsAffected > 0, res.Error
}

func (r *frRepo) GetByID(ctx context.Context, internalID string) (*fiscal.FiscalRegister, error) {
	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("id = ?", internalID).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

func (r *frRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*fiscal.FiscalRegister, error) {
	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

func (r *frRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*fiscal.FiscalRegister, error) {
	var frs []*fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&frs).Error
	if err != nil {
		return nil, err
	}
	frMap := make(map[string]*fiscal.FiscalRegister, len(frs))
	for _, fr := range frs {
		frMap[fr.ID] = fr
	}
	return frMap, nil
}

func (r *frRepo) Search(ctx context.Context, term string, limit, offset int) ([]fiscal.FiscalRegister, error) {
	var frs []fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&frs).Error
	return frs, err
}

func (r *frRepo) FindBySerialNumber(ctx context.Context, sn string) (*fiscal.FiscalRegister, error) {
	if sn == "" {
		return nil, nil
	}
	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("fr_serial_number = ?", sn).Order("updated_at DESC").First(&fr).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &fr, err
}

func (r *frRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]fiscal.FiscalRegister, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var frs []fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&frs).Error
	return frs, err
}

func (r *frRepo) LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&fiscal.FiscalRegister{}).
		Where("owner_id = ? AND health_status != ?", ownerID, "locked").
		Updates(map[string]interface{}{
			"health_status_before_lock": gorm.Expr("health_status"),
			"health_status":             "locked",
		}).Error
}

func (r *frRepo) UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&fiscal.FiscalRegister{}).
		Where("owner_id = ? AND health_status = ? AND health_status_before_lock IS NOT NULL", ownerID, "locked").
		Updates(map[string]interface{}{
			"health_status":             gorm.Expr("health_status_before_lock"),
			"health_status_before_lock": gorm.Expr("NULL"),
		}).Error
}
