package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	infraDB "etalon-server/internal/infra/db"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type companyRepo struct {
	db *gorm.DB
}

func NewCompanyRepo(db *gorm.DB) company.Repository {
	return &companyRepo{db: db}
}

func (r *companyRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *companyRepo) Create(ctx context.Context, entity *company.Company) error {
	return r.getDB(ctx).WithContext(ctx).Create(entity).Error
}

func (r *companyRepo) Update(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error) {
	delete(updateData, "meta_class")

	res := r.getDB(ctx).WithContext(ctx).Model(&company.Company{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *companyRepo) Delete(ctx context.Context, internalID string) (bool, error) {
	res := r.getDB(ctx).WithContext(ctx).Where("id = ?", internalID).Delete(&company.Company{})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *companyRepo) GetByID(ctx context.Context, internalID string) (*company.Company, error) {
	var entity company.Company
	err := r.getDB(ctx).WithContext(ctx).
		Joins("LEFT JOIN companies parent ON parent.id = companies.parent_id").
		Select(`
			companies.*,
			parent.title AS parent_title,
			(
				SELECT c.id
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_id,
			(
				SELECT c.services->>0
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_type
		`).
		Where("companies.id = ?", internalID).
		First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &entity, nil
}

func (r *companyRepo) GetByIDs(ctx context.Context, internalIDs []string) ([]company.Company, error) {
	if len(internalIDs) == 0 {
		return nil, nil
	}
	var entities []company.Company
	err := r.getDB(ctx).WithContext(ctx).Where("id IN ?", internalIDs).Find(&entities).Error
	return entities, err
}

func (r *companyRepo) GetChildren(ctx context.Context, parentID string) ([]company.Company, error) {
	var entities []company.Company
	err := r.getDB(ctx).WithContext(ctx).Where("parent_id = ?", parentID).Find(&entities).Error
	return entities, err
}

func (r *companyRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*company.Company, error) {
	var entity company.Company
	err := r.getDB(ctx).WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&entity).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &entity, nil
}

func (r *companyRepo) GetAllParentIDs(ctx context.Context, childID string) ([]string, error) {
	var parentIDs []string
	currentID := childID
	db := r.getDB(ctx)

	for i := 0; i < 10; i++ {
		var entity company.Company
		err := db.WithContext(ctx).Select("parent_id").Where("id = ?", currentID).First(&entity).Error
		if err == gorm.ErrRecordNotFound {
			break
		}
		if err != nil {
			return nil, err
		}
		if entity.ParentID != nil && *entity.ParentID != "" {
			parentIDs = append(parentIDs, *entity.ParentID)
			currentID = *entity.ParentID
		} else {
			break
		}
	}
	return parentIDs, nil
}

func (r *companyRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*company.Company, error) {
	var entities []*company.Company
	err := r.getDB(ctx).WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&entities).Error
	if err != nil {
		return nil, err
	}
	res := make(map[string]*company.Company, len(entities))
	for _, c := range entities {
		res[c.ID] = c
	}
	return res, nil
}

func (r *companyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]company.Company, error) {
	var entities []company.Company
	query := r.getDB(ctx).WithContext(ctx).
		Joins("LEFT JOIN companies parent ON parent.id = companies.parent_id").
		Select(`
			companies.*,
			parent.title AS parent_title,
			(
				SELECT c.id
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_id,
			(
				SELECT c.services->>0
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_type
		`)
	query = applyCompanySearchTerm(query, term)
	if !showInactive {
		query = query.Where("companies.active_contract = ?", true)
	}
	err := query.
		Order("CASE WHEN companies.parent_id IS NULL OR companies.parent_id = '' THEN 0 ELSE 1 END ASC").
		Order("LOWER(COALESCE(companies.title, '')) ASC").
		Limit(limit).
		Offset(offset).
		Find(&entities).Error
	return entities, err
}

func (r *companyRepo) SearchWithTotal(ctx context.Context, term string, showInactive bool, limit, offset int) ([]company.Company, int64, error) {
	base := r.getDB(ctx).WithContext(ctx).
		Model(&company.Company{}).
		Joins("LEFT JOIN companies parent ON parent.id = companies.parent_id")
	base = applyCompanySearchTerm(base, term)
	if !showInactive {
		base = base.Where("companies.active_contract = ?", true)
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var entities []company.Company
	query := r.getDB(ctx).WithContext(ctx).
		Joins("LEFT JOIN companies parent ON parent.id = companies.parent_id").
		Select(`
			companies.*,
			parent.title AS parent_title,
			(
				SELECT c.id
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_id,
			(
				SELECT c.services->>0
				FROM contracts c
				JOIN company_contracts cc ON cc.contract_id = c.id
				WHERE cc.company_id = companies.id
				ORDER BY (c.state = 'active') DESC, c.updated_at DESC
				LIMIT 1
			) AS contract_type
		`)
	query = applyCompanySearchTerm(query, term)
	if !showInactive {
		query = query.Where("companies.active_contract = ?", true)
	}
	if err := query.
		Order("CASE WHEN companies.parent_id IS NULL OR companies.parent_id = '' THEN 0 ELSE 1 END ASC").
		Order("LOWER(COALESCE(companies.title, '')) ASC").
		Limit(limit).
		Offset(offset).
		Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

func applyCompanySearchTerm(query *gorm.DB, term string) *gorm.DB {
	tokens := splitSearchTokens(term)
	if len(tokens) == 0 {
		return query
	}

	for _, token := range tokens {
		pattern := "%" + token + "%"
		query = query.Where(
			"(companies.title ILIKE ? OR companies.address ILIKE ? OR companies.additional_name ILIKE ? OR parent.title ILIKE ?)",
			pattern, pattern, pattern, pattern,
		)
	}

	titleOnlyConditions := make([]string, 0, len(tokens))
	args := make([]interface{}, 0, len(tokens))
	for _, token := range tokens {
		titleOnlyConditions = append(titleOnlyConditions, "companies.title ILIKE ?")
		args = append(args, "%"+token+"%")
	}

	return query.Where(
		"(companies.parent_id IS NULL OR companies.parent_id = '' OR ("+strings.Join(titleOnlyConditions, " OR ")+"))",
		args...,
	)
}

func splitSearchTokens(term string) []string {
	return strings.Fields(strings.TrimSpace(term))
}
