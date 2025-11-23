package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/server"
	infraDB "etalon-server/internal/infra/db"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type serverRepo struct {
	db *gorm.DB
}

// NewServerRepo создает новый экземпляр репозитория.
func NewServerRepo(db *gorm.DB) server.Repository {
	return &serverRepo{db: db}
}

// dbOrTx возвращает DB из аргумента, из контекста или базовый.
// Приоритет: Аргумент tx -> Контекст (Transactor) -> Базовый db.
func (r *serverRepo) dbOrTx(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *serverRepo) Create(ctx context.Context, tx *gorm.DB, s *server.Server) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Create(s).Error
}

func (r *serverRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Model(&server.Server{}).Where("id = ?", internalID).Updates(updateData)
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *serverRepo) Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error) {
	res := r.dbOrTx(ctx, tx).WithContext(ctx).Where("id = ?", internalID).Delete(&server.Server{})
	if res.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(res.Error, &pgErr) && pgErr.Code == "23505" {
			return false, domain.ErrAlreadyExists
		}
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (r *serverRepo) GetByID(ctx context.Context, internalID string) (*server.Server, error) {
	var s server.Server
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Preload("AdditionalOwners").Where("id = ?", internalID).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *serverRepo) GetByIDUnscoped(ctx context.Context, internalID string) (*server.Server, error) {
	var s server.Server
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Preload("AdditionalOwners").Where("id = ?", internalID).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

func (r *serverRepo) GetAllIDsAndDates(ctx context.Context) (map[string]*server.Server, error) {
	var servers []*server.Server
	// Используем Select для оптимизации
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Unscoped().Select("id", "last_modified_date", "deleted_at").Find(&servers).Error
	if err != nil {
		return nil, err
	}
	serverMap := make(map[string]*server.Server, len(servers))
	for _, s := range servers {
		serverMap[s.ID] = s
	}
	return serverMap, nil
}

func (r *serverRepo) Search(ctx context.Context, term string, limit, offset int) ([]server.Server, error) {
	var servers []server.Server
	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("device_name ILIKE ? OR ip ILIKE ? OR unique_id ILIKE ? OR description ILIKE ? OR server_name ILIKE ?",
			"%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%", "%"+term+"%").
		Limit(limit).Offset(offset).Find(&servers).Error
	return servers, err
}

func (r *serverRepo) FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]server.Server, error) {
	var servers []server.Server
	threshold := time.Now().Add(-interval)

	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("ip IS NOT NULL AND ip != ''").
		Where("status NOT IN (?, ?)", "archived", "locked").
		Where("last_polled_at IS NULL OR last_polled_at < ?", threshold).
		Limit(limit).
		Order("last_polled_at ASC NULLS FIRST").
		Find(&servers).Error
	return servers, err
}

func (r *serverRepo) FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*server.Server, error) {
	var s server.Server
	db := r.dbOrTx(ctx, nil).WithContext(ctx).Preload("AdditionalOwners")

	// Приоритет поиска по CRM ID
	if crmid != "" {
		err := db.Where("crm_id = ? AND status != ?", crmid, "locked").First(&s).Error
		if err == nil {
			return &s, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}

	// Поиск по IP
	if ip != "" {
		err := db.Where("ip = ? AND status != ?", ip, "locked").First(&s).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, domain.ErrNotFound
			}
			return nil, err
		}
		return &s, nil
	}

	return nil, nil
}

func (r *serverRepo) FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]server.Server, error) {
	if len(ownerIDs) == 0 {
		return nil, nil
	}
	var servers []server.Server
	err := r.dbOrTx(ctx, nil).WithContext(ctx).Where("owner_id IN ?", ownerIDs).Find(&servers).Error
	return servers, err
}

// LockByOwner блокирует серверы владельца.
func (r *serverRepo) LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&server.Server{}).
		Where("owner_id = ? AND status != ?", ownerID, "locked").
		Updates(map[string]interface{}{
			"status_before_lock": gorm.Expr("status"),
			"status":             "locked",
		}).Error
}

// UnlockByOwner разблокирует серверы владельца.
func (r *serverRepo) UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error {
	return r.dbOrTx(ctx, tx).WithContext(ctx).Model(&server.Server{}).
		Where("owner_id = ? AND status = ? AND status_before_lock IS NOT NULL", ownerID, "locked").
		Updates(map[string]interface{}{
			"status":             gorm.Expr("status_before_lock"),
			"status_before_lock": gorm.Expr("NULL"),
		}).Error
}
