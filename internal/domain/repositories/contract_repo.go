// internal/repositories/contract_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"

	"gorm.io/gorm"
)

// ContractRepo определяет интерфейс для работы с хранилищем контрактов.
type ContractRepo interface {
	Create(ctx context.Context, tx *gorm.DB, contract *models.Contract) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*models.Contract, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*models.Contract, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*models.Contract, error)
	GetActiveContractIDsForCompany(ctx context.Context, companyID string) ([]string, error)
}

type contractRepo struct {
	db *gorm.DB
}

// NewContractRepo создает новый экземпляр репозитория.
func NewContractRepo(db *gorm.DB) ContractRepo {
	return &contractRepo{db: db}
}

func (r *contractRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *contractRepo) Create(ctx context.Context, tx *gorm.DB, contract *models.Contract) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(contract).Error
}

func (r *contractRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Contract{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("id = ?", internalID).Delete(&models.Contract{})
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) GetByID(ctx context.Context, internalID string) (*models.Contract, error) {
	var contract models.Contract
	err := r.db.WithContext(ctx).Where("id = ?", internalID).First(&contract).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &contract, err
}

func (r *contractRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*models.Contract, error) {
	var contract models.Contract
	err := r.db.WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&contract).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &contract, err
}

func (r *contractRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*models.Contract, error) {
	var contracts []*models.Contract
	err := r.db.WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&contracts).Error
	if err != nil {
		return nil, err
	}
	contractMap := make(map[string]*models.Contract, len(contracts))
	for _, c := range contracts {
		contractMap[c.ID] = c
	}
	return contractMap, nil
}

// GetActiveContractIDsForCompany находит все активные контракты для компании по ее внутреннему ID.
func (r *contractRepo) GetActiveContractIDsForCompany(ctx context.Context, companyID string) ([]string, error) {
	var contractIDs []string
	err := r.db.WithContext(ctx).Model(&models.Contract{}).
		Joins("JOIN company_contracts ON company_contracts.contract_id = contracts.id").
		Where("company_contracts.company_id = ? AND contracts.state = ?", companyID, "active").
		Pluck("contracts.id", &contractIDs).Error
	return contractIDs, err
}
