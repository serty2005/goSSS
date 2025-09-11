// internal/repositories/company_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/domain/models"

	"gorm.io/gorm"
)

// CompanyRepo определяет интерфейс для работы с хранилищем компаний.
type CompanyRepo interface {
	Create(ctx context.Context, tx *gorm.DB, company *models.Company) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*models.Company, error)
	GetByIDs(ctx context.Context, internalIDs []string) ([]models.Company, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*models.Company, error)
	GetAllParentIDs(ctx context.Context, childID string) ([]string, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*models.Company, error)
	Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error)
}

// companyRepo реализует интерфейс CompanyRepo.
type companyRepo struct {
	db *gorm.DB
}

// NewCompanyRepo создает новый экземпляр репозитория компаний.
func NewCompanyRepo(db *gorm.DB) CompanyRepo {
	return &companyRepo{db: db}
}

// dbOrTx возвращает переданную транзакцию или основное подключение к БД.
func (r *companyRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create создает новую компанию в базе данных.
func (r *companyRepo) Create(ctx context.Context, tx *gorm.DB, company *models.Company) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(company).Error
}

// Update обновляет данные компании по ее внутреннему ID.
func (r *companyRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Company{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" компании по ее внутреннему ID.
func (r *companyRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("id = ?", internalID).Delete(&models.Company{})
	return res.RowsAffected > 0, res.Error
}

// GetByID находит компанию по ее внутреннему ID.
func (r *companyRepo) GetByID(ctx context.Context, internalID string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Where("id = ?", internalID).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

// GetByIDs находит компании по списку их внутренних ID.
func (r *companyRepo) GetByIDs(ctx context.Context, internalIDs []string) ([]models.Company, error) {
	if len(internalIDs) == 0 {
		return nil, nil
	}
	var companies []models.Company
	err := r.db.WithContext(ctx).Where("id IN ?", internalIDs).Find(&companies).Error
	return companies, err
}

// GetByIDUnscoped находит компанию по внутреннему ID, включая "мягко удаленные".
func (r *companyRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

// GetAllParentIDs находит все родительские внутренние ID для дочерней компании.
func (r *companyRepo) GetAllParentIDs(ctx context.Context, childID string) ([]string, error) {
	var parentIDs []string
	currentID := childID

	// Защита от бесконечного цикла, максимум 10 уровней вложенности
	for i := 0; i < 10; i++ {
		var company models.Company
		err := r.db.WithContext(ctx).
			Select("parent_id").
			Where("id = ?", currentID).
			First(&company).Error

		if err == gorm.ErrRecordNotFound {
			break // Дошли до корня или компания не найдена
		}
		if err != nil {
			return nil, err
		}

		if company.ParentID != nil && *company.ParentID != "" {
			parentIDs = append(parentIDs, *company.ParentID)
			currentID = *company.ParentID
		} else {
			break // Нет родителя
		}
	}
	return parentIDs, nil
}

// GetAllIDsAndDates извлекает все внутренние ID, даты модификации и статусы удаления.
func (r *companyRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	var companies []*models.Company
	err := r.db.WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&companies).Error
	if err != nil {
		return nil, err
	}
	companyMap := make(map[string]*models.Company, len(companies))
	for _, c := range companies {
		companyMap[c.ID] = c
	}
	return companyMap, nil
}

// Search выполняет поиск компаний по текстовому запросу.
func (r *companyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error) {
	var companies []models.Company
	query := r.db.WithContext(ctx).
		Where("title ILIKE ? OR address ILIKE ? OR additional_name ILIKE ?", "%"+term+"%", "%"+term+"%", "%"+term+"%")

	if !showInactive {
		query = query.Where("active_contract = ?", true)
	}

	err := query.Limit(limit).Offset(offset).Find(&companies).Error
	return companies, err
}
