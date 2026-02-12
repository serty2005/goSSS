package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/workstation"
	infraDB "etalon-server/internal/infra/db"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type workstationRepo struct {
	db *gorm.DB
}

func NewWorkstationRepo(db *gorm.DB) workstation.Repository {
	return &workstationRepo{db: db}
}

func (r *workstationRepo) dbOrTx(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *workstationRepo) Create(ctx context.Context, tx *gorm.DB, ws *workstation.Workstation) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Create(ws).Error
}

func (r *workstationRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	delete(updateData, "meta_class")
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&workstation.Workstation{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *workstationRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Where("id = ?", internalID).Delete(&workstation.Workstation{})
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *workstationRepo) GetByID(ctx context.Context, internalID string) (*workstation.Workstation, error) {
	var ws workstation.Workstation
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("id = ?", internalID).First(&ws).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &ws, nil
}

func (r *workstationRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*workstation.Workstation, error) {
	var ws workstation.Workstation
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&ws).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &ws, nil
}

func (r *workstationRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*workstation.Workstation, error) {
	var workstations []*workstation.Workstation
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&workstations).Error
	if err != nil {
		return nil, err
	}
	workstationMap := make(map[string]*workstation.Workstation, len(workstations))
	for _, ws := range workstations {
		workstationMap[ws.ID] = ws
	}
	return workstationMap, nil
}

func (r *workstationRepo) Search(ctx context.Context, term string, limit, offset int) ([]workstation.Workstation, error) {
	var workstations []workstation.Workstation
	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("id::text ILIKE ? OR device_name ILIKE ? OR description ILIKE ? OR anydesk ILIKE ? OR teamviewer ILIKE ? OR litemanager ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&workstations).Error
	return workstations, err
}

func (r *workstationRepo) FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*workstation.Workstation, error) {
	var ws workstation.Workstation
	// Используем dbOrTx без транзакции
	query := r.dbOrTx(ctx, nil).WithContext(ctx).Where("health_status != ?", "locked")

	var conditions []string
	var values []interface{}

	if tv != "" && tv != "None" {
		conditions = append(conditions, "teamviewer = ?")
		values = append(values, tv)
	}
	if ad != "" && ad != "None" {
		conditions = append(conditions, "anydesk = ?")
		values = append(values, ad)
	}
	if lm != "" && lm != "None" {
		conditions = append(conditions, "litemanager = ?")
		values = append(values, lm)
	}

	if len(conditions) == 0 {
		return nil, nil
	}

	query = query.Where(strings.Join(conditions, " OR "), values...)

	err := query.Order("updated_at DESC").First(&ws).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &ws, nil
}

// FindAllByRemoteIDs реализует поиск всех совпадений для детекции дубликатов.
func (r *workstationRepo) FindAllByRemoteIDs(ctx context.Context, tv, lm string) ([]workstation.Workstation, error) {
	var workstations []workstation.Workstation

	// Ищем только активные записи (не locked)
	query := r.dbOrTx(ctx, nil).WithContext(ctx).Where("health_status != ?", "locked")

	var conditions []string
	var values []interface{}

	if tv != "" && tv != "None" {
		conditions = append(conditions, "teamviewer = ?")
		values = append(values, tv)
	}
	if lm != "" && lm != "None" {
		conditions = append(conditions, "litemanager = ?")
		values = append(values, lm)
	}

	if len(conditions) == 0 {
		return nil, nil
	}

	// Используем OR, так как совпадение по любому из ID считается попаданием
	query = query.Where(strings.Join(conditions, " OR "), values...)

	err := query.Find(&workstations).Error
	return workstations, err
}

func (r *workstationRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]workstation.Workstation, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var workstations []workstation.Workstation
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&workstations).Error
	return workstations, err
}

func (r *workstationRepo) SetOwnerWithBinding(ctx context.Context, tx *gorm.DB, internalID string, ownerID string, bindingMode string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&workstation.Workstation{}).
		Where("id = ?", internalID).
		Updates(map[string]interface{}{"owner_id": ownerID, "owner_binding_mode": bindingMode})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *workstationRepo) LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	// У Workstation нет поля status (операционного), используем HealthStatus
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&workstation.Workstation{}).
		Where("owner_id = ? AND health_status != ?", ownerID, "locked").
		Updates(map[string]interface{}{
			"health_status_before_lock": gorm.Expr("health_status"),
			"health_status":             "locked",
		}).Error
}

func (r *workstationRepo) UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&workstation.Workstation{}).
		Where("owner_id = ? AND health_status = ? AND health_status_before_lock IS NOT NULL", ownerID, "locked").
		Updates(map[string]interface{}{
			"health_status":             gorm.Expr("health_status_before_lock"),
			"health_status_before_lock": gorm.Expr("NULL"),
		}).Error
}
