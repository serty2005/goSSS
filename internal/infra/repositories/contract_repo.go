package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	infraDB "etalon-server/internal/infra/db"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type contractRepo struct {
	db *gorm.DB
}

func NewContractRepo(db *gorm.DB) contract.Repository {
	return &contractRepo{db: db}
}

func (r *contractRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *contractRepo) Create(ctx context.Context, c *contract.Contract) error {
	// MetaClass удален
	return r.getDB(ctx).WithContext(ctx).Create(c).Error
}

func (r *contractRepo) Update(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error) {
	delete(updateData, "meta_class")
	res := r.getDB(ctx).WithContext(ctx).Model(&contract.Contract{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *contractRepo) Restore(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error) {
	delete(updateData, "meta_class")
	updateData["deleted_at"] = nil
	res := r.getDB(ctx).WithContext(ctx).Unscoped().Model(&contract.Contract{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *contractRepo) Delete(ctx context.Context, internalID string) (bool, error) {
	res := r.getDB(ctx).WithContext(ctx).Where("id = ?", internalID).Delete(&contract.Contract{})
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *contractRepo) GetByID(ctx context.Context, internalID string) (*contract.Contract, error) {
	var c contract.Contract
	err := r.getDB(ctx).WithContext(ctx).Preload("Companies").Where("id = ?", internalID).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *contractRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*contract.Contract, error) {
	var c contract.Contract
	err := r.getDB(ctx).WithContext(ctx).Unscoped().Preload("Companies").Where("id = ?", internalID).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// GetByServiceDeskUUID находит контракт по внешнему UUID (через таблицу связей).
func (r *contractRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*contract.Contract, error) {
	var c contract.Contract
	// Используем JOIN с таблицей external_system_links
	err := r.getDB(ctx).WithContext(ctx).
		Joins("JOIN external_system_links l ON l.internal_id = contracts.id").
		Where("l.system_name = ? AND l.service_desk_uuid = ? AND l.entity_type = ?", "naumen", sdUUID, "Contract").
		First(&c).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *contractRepo) ListByLastUpdatedBy(ctx context.Context, lastUpdatedBy string) ([]contract.Contract, error) {
	var items []contract.Contract
	err := r.getDB(ctx).WithContext(ctx).
		Preload("Companies").
		Where("last_updated_by = ?", lastUpdatedBy).
		Find(&items).Error
	return items, err
}

// ReplaceCompanyLinks обновляет связи Many-to-Many для контракта.
// companyIDs - список внутренних UUID компаний.
func (r *contractRepo) ReplaceCompanyLinks(ctx context.Context, c *contract.Contract, companyIDs []string) error {
	db := r.getDB(ctx)

	// Создаем срез структур Company только с заполненными ID.
	// Этого достаточно для GORM, чтобы понять, какие записи связывать.
	companies := make([]company.Company, len(companyIDs))
	for i, id := range companyIDs {
		companies[i] = company.Company{
			Base: common.Base{ID: id},
		}
	}

	// Association("Companies").Replace(...) заменяет ВСЕ текущие связи на новые.
	// GORM принимает слайс структур и корректно обновляет join-таблицу company_contracts.
	return db.Model(c).Association("Companies").Replace(companies)
}

// Helper для преобразования ID в структуры для GORM ассоциаций
func (r *contractRepo) idsToCompanies(ids []string) []map[string]interface{} {
	// GORM позволяет использовать map для primary keys в ассоциациях
	res := make([]map[string]interface{}, len(ids))
	for i, id := range ids {
		res[i] = map[string]interface{}{"id": id}
	}
	return res
}

func (r *contractRepo) GetActiveContractIDsForCompany(ctx context.Context, companyID string) ([]string, error) {
	var contractIDs []string
	err := r.getDB(ctx).WithContext(ctx).Table("contracts").
		Joins("JOIN company_contracts ON company_contracts.contract_id = contracts.id").
		Where("company_contracts.company_id = ? AND contracts.state = ?", companyID, "active").
		Order("(contracts.id = ('mail-contract:' || company_contracts.company_id)) DESC").
		Order("contracts.updated_at DESC").
		Pluck("contracts.id", &contractIDs).Error
	return contractIDs, err
}

func (r *contractRepo) DeactivateActiveContractsExcept(ctx context.Context, companyID string, keepContractID string) error {
	db := r.getDB(ctx).WithContext(ctx)
	linkedContracts := db.Table("company_contracts").
		Select("contract_id").
		Where("company_id = ?", companyID)
	return db.Model(&contract.Contract{}).
		Where("id IN (?) AND id <> ? AND state = ?", linkedContracts, keepContractID, "active").
		Update("state", "inactive").Error
}

func (r *contractRepo) ListForCompany(ctx context.Context, companyID string) ([]contract.Contract, error) {
	items := make([]contract.Contract, 0)
	err := r.getDB(ctx).WithContext(ctx).
		Joins("JOIN company_contracts ON company_contracts.contract_id = contracts.id").
		Where("company_contracts.company_id = ?", companyID).
		Preload("Companies").
		Order("(contracts.id = ('mail-contract:' || company_contracts.company_id)) DESC").
		Order("(contracts.state = 'active') DESC").
		Order("COALESCE(contracts.last_modified_date, contracts.updated_at, contracts.created_at) DESC").
		Find(&items).Error
	return items, err
}

func (r *contractRepo) GetMailImportByAttachmentHash(ctx context.Context, attachmentHash string) (*contract.MailImport, error) {
	var item contract.MailImport
	err := r.getDB(ctx).WithContext(ctx).
		Where("attachment_hash = ?", attachmentHash).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (r *contractRepo) UpsertMailImport(ctx context.Context, item *contract.MailImport) error {
	if item == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "attachment_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"message_id",
			"attachment_name",
			"received_at",
			"status",
			"error_text",
			"processed_at",
			"report_rows",
			"updated_at",
			"last_updated_by",
		}),
	}).Create(item).Error
}

func (r *contractRepo) ListMailImports(ctx context.Context, limit int) ([]contract.MailImport, error) {
	if limit <= 0 {
		limit = 20
	}

	items := make([]contract.MailImport, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Order("processed_at DESC NULLS LAST").
		Order("received_at DESC NULLS LAST").
		Order("created_at DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *contractRepo) CreateServicePointSyncRun(ctx context.Context, item *contract.ServicePointSyncRun) error {
	if item == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Create(item).Error
}

func (r *contractRepo) ListServicePointSyncRuns(ctx context.Context, limit int) ([]contract.ServicePointSyncRun, error) {
	if limit <= 0 {
		limit = 20
	}

	items := make([]contract.ServicePointSyncRun, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Order("started_at DESC").
		Order("created_at DESC").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *contractRepo) GetServicePointSyncRunByID(ctx context.Context, id string) (*contract.ServicePointSyncRun, error) {
	var item contract.ServicePointSyncRun
	err := r.getDB(ctx).WithContext(ctx).Where("id = ?", id).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (r *contractRepo) ListServicePointSyncConflicts(ctx context.Context) ([]contract.ServicePointSyncConflict, error) {
	items := make([]contract.ServicePointSyncConflict, 0, 32)
	err := r.getDB(ctx).WithContext(ctx).
		Order("service_point_name ASC").
		Order("updated_at DESC").
		Find(&items).Error
	return items, err
}

func (r *contractRepo) ReplaceServicePointSyncConflicts(ctx context.Context, conflicts []contract.ServicePointSyncConflict) error {
	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&contract.ServicePointSyncConflict{}).Error; err != nil {
			return err
		}
		if len(conflicts) == 0 {
			return nil
		}
		now := time.Now()
		for i := range conflicts {
			conflicts[i].UpdatedAt = now
			if conflicts[i].CreatedAt.IsZero() {
				conflicts[i].CreatedAt = now
			}
		}
		return tx.CreateInBatches(conflicts, 200).Error
	})
}
