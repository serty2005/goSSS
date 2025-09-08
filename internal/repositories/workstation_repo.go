// internal/repositories/workstation_repo.go
package repositories

import (
	"context"
	"etalon-server/internal/models"
	"strings"

	"gorm.io/gorm"
)

// WorkstationRepo определяет интерфейс для работы с хранилищем рабочих станций.
type WorkstationRepo interface {
	Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*models.Workstation, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*models.Workstation, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error)
	FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.Workstation, error)
}

// workstationRepo реализует интерфейс WorkstationRepo.
type workstationRepo struct {
	db *gorm.DB
}

// NewWorkstationRepo создает новый экземпляр репозитория.
func NewWorkstationRepo(db *gorm.DB) WorkstationRepo {
	return &workstationRepo{db: db}
}

func (r *workstationRepo) dbOrTx(tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return r.db
}

// Create создает новую рабочую станцию в базе данных.
func (r *workstationRepo) Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(workstation).Error
}

// Update обновляет данные рабочей станции по ее внутреннему ID.
func (r *workstationRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Workstation{}).Where("id = ?", internalID).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

// Delete выполняет "мягкое удаление" рабочей станции по ее внутреннему ID.
func (r *workstationRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("id = ?", internalID).Delete(&models.Workstation{})
	return res.RowsAffected > 0, res.Error
}

// GetByID находит рабочую станцию по ее внутреннему ID.
func (r *workstationRepo) GetByID(ctx context.Context, internalID string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Where("id = ?", internalID).First(&workstation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &workstation, err
}

// GetByIDUnscoped находит рабочую станцию по внутреннему ID, включая "мягко удаленные".
func (r *workstationRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Unscoped().Where("id = ?", internalID).First(&workstation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &workstation, err
}

// GetAllIDsAndDates извлекает все внутренние ID, даты модификации и статусы удаления.
func (r *workstationRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error) {
	var workstations []*models.Workstation
	if err := r.db.WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&workstations).Error; err != nil {
		return nil, err
	}
	workstationMap := make(map[string]*models.Workstation, len(workstations))
	for _, ws := range workstations {
		workstationMap[ws.ID] = ws
	}
	return workstationMap, nil
}

// Search выполняет поиск рабочих станций по текстовому запросу.
func (r *workstationRepo) Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error) {
	var workstations []models.Workstation
	err := r.db.WithContext(ctx).
		Where("device_name ILIKE ? OR description ILIKE ?", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&workstations).Error
	return workstations, err
}

// FindByRemoteIDs ищет рабочую станцию по любому из ID удаленного доступа.
func (r *workstationRepo) FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error) {
	var ws models.Workstation
	query := r.db.WithContext(ctx).Where("status != ?", "locked")

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
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &ws, err
}

// FindByOwnerIDs находит все рабочие станции, принадлежащие указанным владельцам.
func (r *workstationRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]models.Workstation, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var workstations []models.Workstation
	err := r.db.WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&workstations).Error
	return workstations, err
}
