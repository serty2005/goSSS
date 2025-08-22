package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// CompanyRepo определяет интерфейс для работы с хранилищем компаний.
type CompanyRepo interface {
	Create(ctx context.Context, tx *gorm.DB, company *models.Company) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Company, error)
	GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error)
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

func (r *companyRepo) Create(ctx context.Context, tx *gorm.DB, company *models.Company) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(company).Error
}

func (r *companyRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Company{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *companyRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Company{})
	return res.RowsAffected > 0, res.Error
}

func (r *companyRepo) GetByUUID(ctx context.Context, uuid string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

// GetByUUIDs находит компании по списку их ServiceDesk UUID.
func (r *companyRepo) GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error) {
	if len(uuids) == 0 {
		return nil, nil
	}
	var companies []models.Company
	err := r.db.WithContext(ctx).Where("service_desk_uuid IN ?", uuids).Find(&companies).Error
	return companies, err
}

func (r *companyRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error) {
	var company models.Company
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&company).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &company, err
}

func (r *companyRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	var companies []*models.Company
	err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&companies).Error
	if err != nil {
		return nil, err
	}
	companyMap := make(map[string]*models.Company, len(companies))
	for _, c := range companies {
		if c.ServiceDeskUUID != nil {
			companyMap[*c.ServiceDeskUUID] = c
		}
	}
	return companyMap, nil
}

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
