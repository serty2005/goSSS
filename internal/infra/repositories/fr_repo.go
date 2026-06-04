package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/fiscal"
	infraDB "etalon-server/internal/infra/db"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type frRepo struct {
	db *gorm.DB
}

func NewFiscalRegisterRepo(db *gorm.DB) fiscal.Repository {
	return &frRepo{db: db}
}

func (r *frRepo) listBaseQuery(ctx context.Context) *gorm.DB {
	return r.dbOrTx(ctx, nil).WithContext(ctx).
		Model(&fiscal.FiscalRegister{}).
		Joins("LEFT JOIN companies owner_comp ON owner_comp.id = fiscal_registers.owner_id").
		Joins("LEFT JOIN companies owner_parent ON owner_parent.id = owner_comp.parent_id")
}

func (r *frRepo) applyFiscalListSelect(query *gorm.DB) *gorm.DB {
	return query.Select("fiscal_registers.*, owner_comp.title AS owner_title, owner_comp.parent_id AS owner_parent_id, owner_parent.title AS owner_parent_title")
}

func cleanStringSlice(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func (r *frRepo) applyCompanyFilter(query *gorm.DB, companyIDs []string) *gorm.DB {
	cleanIDs := cleanStringSlice(companyIDs)
	if len(cleanIDs) == 0 {
		return query
	}
	return query.Where(`
		fiscal_registers.owner_id IN (
			WITH RECURSIVE company_tree(id) AS (
				SELECT companies.id
				FROM companies
				WHERE companies.id IN ?
				UNION ALL
				SELECT child.id
				FROM companies child
				JOIN company_tree parent ON child.parent_id = parent.id
			)
			SELECT id FROM company_tree
		)
	`, cleanIDs)
}

func (r *frRepo) applyModelFilter(query *gorm.DB, models []string) *gorm.DB {
	cleanModels := cleanStringSlice(models)
	if len(cleanModels) == 0 {
		return query
	}
	return query.Where("fiscal_registers.model_kkt IN ?", cleanModels)
}

func (r *frRepo) applyFNExpireFilter(query *gorm.DB, from, to, minDate *time.Time) *gorm.DB {
	lowerBound := from
	if minDate != nil && (lowerBound == nil || lowerBound.Before(*minDate)) {
		lowerBound = minDate
	}
	if lowerBound != nil {
		query = query.Where("fiscal_registers.fn_expire_date >= ?", lowerBound)
	}
	if to != nil {
		query = query.Where("fiscal_registers.fn_expire_date <= ?", to)
	}
	return query
}

func (r *frRepo) applySearchFilter(query *gorm.DB, term string) *gorm.DB {
	term = strings.TrimSpace(term)
	if term == "" {
		return query
	}
	pattern := "%" + term + "%"
	return query.Where(`
		fiscal_registers.address ILIKE ?
		OR fiscal_registers.legal_name ILIKE ?
		OR fiscal_registers.fr_serial_number ILIKE ?
		OR fiscal_registers.rn_kkt ILIKE ?
		OR owner_comp.title ILIKE ?
		OR owner_parent.title ILIKE ?
		OR fiscal_registers.id::text ILIKE ?
		OR fiscal_registers.fn_number ILIKE ?
		OR fiscal_registers.model_kkt ILIKE ?
	`, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
}

func (r *frRepo) applyListFilters(query *gorm.DB, filter fiscal.ListFilter) *gorm.DB {
	query = r.applySearchFilter(query, filter.SearchQuery)
	query = r.applyCompanyFilter(query, filter.CompanyIDs)
	query = r.applyModelFilter(query, filter.Models)
	query = r.applyFNExpireFilter(query, filter.FNExpireFrom, filter.FNExpireTo, filter.FNExpireMin)
	return query
}

func (r *frRepo) applyListOrder(query *gorm.DB, filter fiscal.ListFilter) *gorm.DB {
	if strings.TrimSpace(filter.SortBy) != "fn_expire_date" {
		return query.Order("fiscal_registers.updated_at DESC")
	}

	direction := "ASC"
	switch strings.ToLower(strings.TrimSpace(filter.SortOrder)) {
	case "desc", "descend":
		direction = "DESC"
	}
	return query.Order("fiscal_registers.fn_expire_date " + direction + " NULLS LAST").Order("fiscal_registers.updated_at DESC")
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
	frs, _, err := r.List(ctx, fiscal.ListFilter{
		Limit:       limit,
		Offset:      offset,
		SearchQuery: term,
	})
	return frs, err
}

func (r *frRepo) List(ctx context.Context, filter fiscal.ListFilter) ([]fiscal.FiscalRegister, int64, error) {
	limit := filter.Limit
	offset := filter.Offset
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var total int64
	countQuery := r.applyListFilters(r.listBaseQuery(ctx), filter)
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var frs []fiscal.FiscalRegister
	listQuery := r.applyListFilters(r.listBaseQuery(ctx), filter)
	if err := r.applyListOrder(r.applyFiscalListSelect(listQuery), filter).
		Limit(limit).
		Offset(offset).
		Find(&frs).Error; err != nil {
		return nil, 0, err
	}

	return frs, total, nil
}

func (r *frRepo) ListModels(ctx context.Context) ([]string, error) {
	var models []string
	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Model(&fiscal.FiscalRegister{}).
		Where("model_kkt IS NOT NULL AND BTRIM(model_kkt) <> ''").
		Distinct("model_kkt").
		Order("model_kkt ASC").
		Pluck("model_kkt", &models).Error
	if err != nil {
		return nil, err
	}

	return models, nil
}

func (r *frRepo) SearchWithTotal(ctx context.Context, term string, limit, offset int) ([]fiscal.FiscalRegister, int64, error) {
	return r.List(ctx, fiscal.ListFilter{
		Limit:       limit,
		Offset:      offset,
		SearchQuery: term,
	})
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
