package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	infraDB "etalon-server/internal/infra/db"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
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
	delete(updateData, "meta_class")
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&fiscal.FiscalRegister{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *frRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Where("id = ?", internalID).Delete(&fiscal.FiscalRegister{})
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *frRepo) GetByID(ctx context.Context, internalID string) (*fiscal.FiscalRegister, error) {
	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("id = ?", internalID).First(&fr).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &fr, nil
}

func (r *frRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*fiscal.FiscalRegister, error) {
	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&fr).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &fr, nil
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
		Where("id::text ILIKE ? OR rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ? OR model_kkt ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&frs).Error
	return frs, err
}

func (r *frRepo) List(ctx context.Context, limit, offset int) ([]fiscal.FiscalRegister, int64, error) {
	var total int64
	if err := r.dbOrTx(ctx, nil).WithContext(ctx).Model(&fiscal.FiscalRegister{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var frs []fiscal.FiscalRegister
	if err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Limit(limit).
		Offset(offset).
		Order("updated_at DESC").
		Find(&frs).Error; err != nil {
		return nil, 0, err
	}

	return frs, total, nil
}

func (r *frRepo) SearchWithTotal(ctx context.Context, term string, limit, offset int) ([]fiscal.FiscalRegister, int64, error) {
	pattern := "%" + term + "%"
	base := r.dbOrTx(ctx, nil).WithContext(ctx).
		Model(&fiscal.FiscalRegister{}).
		Where("id::text ILIKE ? OR rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ? OR model_kkt ILIKE ?",
			pattern, pattern, pattern, pattern, pattern, pattern)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var frs []fiscal.FiscalRegister
	if err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("id::text ILIKE ? OR rn_kkt ILIKE ? OR fr_serial_number ILIKE ? OR fn_number ILIKE ? OR legal_name ILIKE ? OR model_kkt ILIKE ?",
			pattern, pattern, pattern, pattern, pattern, pattern).
		Limit(limit).
		Offset(offset).
		Order("updated_at DESC").
		Find(&frs).Error; err != nil {
		return nil, 0, err
	}

	return frs, total, nil
}

func (r *frRepo) FindBySerialNumber(ctx context.Context, sn string) (*fiscal.FiscalRegister, error) {
	if sn == "" {
		return nil, nil
	}
	norm := strings.ToUpper(strings.TrimSpace(sn))
	norm = strings.ReplaceAll(norm, " ", "")

	var fr fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("fr_serial_normalized = ? OR fr_serial_number = ?", norm, strings.TrimSpace(sn)).
		Order("updated_at DESC").First(&fr).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &fr, nil
}

func (r *frRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]fiscal.FiscalRegister, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var frs []fiscal.FiscalRegister
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&frs).Error
	return frs, err
}

func (r *frRepo) SetOwnerWithBinding(ctx context.Context, tx *gorm.DB, internalID string, ownerID string, bindingMode string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&fiscal.FiscalRegister{}).
		Where("id = ?", internalID).
		Updates(map[string]interface{}{"owner_id": ownerID, "owner_binding_mode": bindingMode})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
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
