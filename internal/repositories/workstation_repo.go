package repositories

import (
	"context"
	"etalon-server/internal/models"

	"gorm.io/gorm"
)

// WorkstationRepo определяет интерфейс для работы с хранилищем рабочих станций.
type WorkstationRepo interface {
	Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Workstation, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error)
	FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error)
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

func (r *workstationRepo) Create(ctx context.Context, tx *gorm.DB, workstation *models.Workstation) error {
	return r.dbOrTx(tx).WithContext(ctx).Create(workstation).Error
}

func (r *workstationRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Model(&models.Workstation{}).Where("service_desk_uuid = ?", uuid).Updates(updateData)
	return res.RowsAffected > 0, res.Error
}

func (r *workstationRepo) GetByUUID(ctx context.Context, uuid string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Where("service_desk_uuid = ?", uuid).First(&workstation).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &workstation, err
}

func (r *workstationRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error) {
	var workstations []*models.Workstation
	if err := r.db.WithContext(ctx).Unscoped().Select("service_desk_uuid", "last_modified_date", "deleted_at").Find(&workstations).Error; err != nil {
		return nil, err
	}
	workstationMap := make(map[string]*models.Workstation, len(workstations))
	for _, ws := range workstations {
		if ws.ServiceDeskUUID != nil {
			workstationMap[*ws.ServiceDeskUUID] = ws
		}
	}
	return workstationMap, nil
}

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
	query := r.db.WithContext(ctx)
	hasCondition := false

	if tv != "" && tv != "None" {
		query = query.Or("teamviewer = ?", tv)
		hasCondition = true
	}
	if ad != "" && ad != "None" {
		query = query.Or("anydesk = ?", ad)
		hasCondition = true
	}
	if lm != "" && lm != "None" {
		query = query.Or("litemanager = ?", lm)
		hasCondition = true
	}

	if !hasCondition {
		return nil, nil
	}

	// Ищем самую свежую запись, если их несколько
	err := query.Order("last_modified_date DESC").First(&ws).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &ws, err
}
