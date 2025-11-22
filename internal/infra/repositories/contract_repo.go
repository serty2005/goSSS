package repositories

import (
	"context"
	"errors"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	infraDB "etalon-server/internal/infra/db"

	"gorm.io/gorm"
)

type contractRepo struct {
	db *gorm.DB
}

// NewContractRepo создает новый экземпляр репозитория.
func NewContractRepo(db *gorm.DB) contract.Repository {
	return &contractRepo{db: db}
}

// getDB извлекает транзакцию из контекста или возвращает базовый DB.
func (r *contractRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *contractRepo) Create(ctx context.Context, c *contract.Contract) error {
	return r.getDB(ctx).WithContext(ctx).Create(c).Error
}

func (r *contractRepo) Update(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.getDB(ctx).WithContext(ctx).Model(&contract.Contract{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) Delete(ctx context.Context, internalID string) (bool, error) {
	res := r.getDB(ctx).WithContext(ctx).Where("id = ?", internalID).Delete(&contract.Contract{})
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) GetByID(ctx context.Context, internalID string) (*contract.Contract, error) {
	var c contract.Contract
	err := r.getDB(ctx).WithContext(ctx).Preload("Companies").Where("id = ?", internalID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
}

// GetByServiceDeskUUID находит контракт по внешнему UUID (через таблицу связей).
func (r *contractRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*contract.Contract, error) {
	var c contract.Contract
	// Используем JOIN с таблицей external_system_links
	err := r.getDB(ctx).WithContext(ctx).
		Joins("JOIN external_system_links l ON l.internal_id = contracts.id").
		Where("l.system_name = ? AND l.service_desk_uuid = ? AND l.entity_type = ?", "naumen", sdUUID, "Contract").
		First(&c).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &c, err
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
		Pluck("contracts.id", &contractIDs).Error
	return contractIDs, err
}
