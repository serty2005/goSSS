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
	Update(ctx context.Context, tx *gorm.DB, uuid string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error)
	GetByUUID(ctx context.Context, uuid string) (*models.Workstation, error)
	GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Workstation, error)
	GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Workstation, error)
	Search(ctx context.Context, term string, limit, offset int) ([]models.Workstation, error)
	FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*models.Workstation, error)
	FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Workstation, error)
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

// Delete выполняет "мягкое удаление" рабочей станции по ее ServiceDesk UUID.
func (r *workstationRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	res := r.dbOrTx(tx).WithContext(ctx).Where("service_desk_uuid = ?", uuid).Delete(&models.Workstation{})
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

func (r *workstationRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Workstation, error) {
	var workstation models.Workstation
	err := r.db.WithContext(ctx).Unscoped().Where("service_desk_uuid = ?", uuid).First(&workstation).Error
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

	// Динамически строим запрос, добавляя условия только для валидных ID
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

	// Если ни одного валидного ID не предоставлено, ничего не ищем
	if len(conditions) == 0 {
		return nil, nil
	}

	// Объединяем условия через OR
	query = query.Where(strings.Join(conditions, " OR "), values...)

	// Ищем самую свежую запись, если их несколько
	err := query.Order("last_modified_date DESC").First(&ws).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &ws, err
}

// FindByOwnerUUIDs находит все рабочие станции, принадлежащие указанным владельцам.
func (r *workstationRepo) FindByOwnerUUIDs(ctx context.Context, ownerUUIDs []string) ([]models.Workstation, error) {
	if len(ownerUUIDs) == 0 {
		return nil, nil
	}
	var workstations []models.Workstation
	err := r.db.WithContext(ctx).Where("owner_service_desk_uuid IN ?", ownerUUIDs).Find(&workstations).Error
	return workstations, err
}
