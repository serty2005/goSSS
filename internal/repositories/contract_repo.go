// internal/repositories/contract_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// ContractRepo определяет интерфейс для работы с хранилищем контрактов.
type ContractRepo interface {
	Create(ctx context.Context, tx *gorm.DB, contract *models.Contract) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Contract, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Contract, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Contract, error)
	GetActiveContractUUIDsForCompany(ctx context.Context, companyUUID string) ([]string, error)
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

func (r *contractRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Contract{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Contract{})
	return res.RowsAffected > 0, res.Error
}

func (r *contractRepo) GetByUUID(ctx context.Context, uuid string) (*models.Contract, error) {
	var contract models.Contract
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&contract).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &contract, err
}

func (r *contractRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Contract, error) {
	var contract models.Contract
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&contract).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &contract, err
}

func (r *contractRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Contract, error) {
	var contracts []*models.Contract
	err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&contracts).Error
	if err != nil {
		return nil, err
	}
	contractMap := make(map[string]*models.Contract, len(contracts))
	for _, c := range contracts {
		if c.ServiceDeskUUID != nil {
			contractMap[*c.ServiceDeskUUID] = c
		}
	}
	return contractMap, nil
}

// GetActiveContractUUIDsForCompany находит все активные контракты для компании.
func (r *contractRepo) GetActiveContractUUIDsForCompany(ctx context.Context, companyUUID string) ([]string, error) {
	var contractUUIDs []string
	err := r.db.WithContext(ctx).Model(&models.Contract{}).
		Joins("JOIN company_contracts ON company_contracts.contract_service_desk_uuid = contracts.service_desk_uuid").
		Where("company_contracts.company_service_desk_uuid = ? AND contracts.state = ?", companyUUID, "active").
		Pluck("contracts.service_desk_uuid", &contractUUIDs).Error
	return contractUUIDs, err
}
