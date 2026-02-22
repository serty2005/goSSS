package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/server"
	infraDB "etalon-server/internal/infra/db"
	"time"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

type serverRepo struct {
	db *gorm.DB
}

func NewServerRepo(db *gorm.DB) server.Repository {
	return &serverRepo{db: db}
}

func (r *serverRepo) listBaseQuery(ctx context.Context) *gorm.DB {
	return r.dbOrTx(ctx, nil).WithContext(ctx).
		Model(&server.Server{}).
		Joins("LEFT JOIN companies owner_comp ON owner_comp.id = servers.owner_id").
		Joins("LEFT JOIN companies owner_parent ON owner_parent.id = owner_comp.parent_id")
}

func (r *serverRepo) applyCompanyFilter(query *gorm.DB, companyIDs []string) *gorm.DB {
	cleanIDs := make([]string, 0, len(companyIDs))
	for _, id := range companyIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			cleanIDs = append(cleanIDs, trimmed)
		}
	}
	if len(cleanIDs) == 0 {
		return query
	}
	return query.Where("(servers.owner_id IN ? OR owner_comp.parent_id IN ?)", cleanIDs, cleanIDs)
}

func (r *serverRepo) applyServerListSelect(query *gorm.DB) *gorm.DB {
	return query.Select("servers.*, owner_comp.title AS owner_title, owner_comp.parent_id AS owner_parent_id, owner_parent.title AS owner_parent_title")
}

func (r *serverRepo) dbOrTx(ctx context.Context, tx *gorm.DB) *gorm.DB {
	if tx != nil {
		return tx
	}
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *serverRepo) Create(ctx context.Context, tx *gorm.DB, s *server.Server) error {
	// MetaClass удален
	return r.dbOrTx(ctx, tx).WithContext(ctx).Create(s).Error
}

func (r *serverRepo) Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error) {
	delete(updateData, "meta_class") // Защита
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

func (r *serverRepo) Search(ctx context.Context, term string, limit, offset int, companyIDs []string) ([]server.Server, error) {
	var servers []server.Server
	pattern := "%" + term + "%"
	query := r.listBaseQuery(ctx)
	query = r.applyCompanyFilter(query, companyIDs)
	query = query.Where("servers.id::text ILIKE ? OR servers.device_name ILIKE ? OR servers.ip ILIKE ? OR servers.unique_id ILIKE ? OR servers.description ILIKE ? OR servers.server_name ILIKE ? OR servers.crm_id ILIKE ?",
		pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	err := r.applyServerListSelect(query).
		Limit(limit).
		Offset(offset).
		Order("servers.updated_at DESC").
		Find(&servers).Error
	return servers, err
}

func (r *serverRepo) List(ctx context.Context, limit, offset int, companyIDs []string) ([]server.Server, int64, error) {
	var total int64
	query := r.listBaseQuery(ctx)
	query = r.applyCompanyFilter(query, companyIDs)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var servers []server.Server
	if err := r.applyServerListSelect(r.applyCompanyFilter(r.listBaseQuery(ctx), companyIDs)).
		Limit(limit).
		Offset(offset).
		Order("servers.updated_at DESC").
		Find(&servers).Error; err != nil {
		return nil, 0, err
	}

	return servers, total, nil
}

func (r *serverRepo) SearchWithTotal(ctx context.Context, term string, limit, offset int, companyIDs []string) ([]server.Server, int64, error) {
	pattern := "%" + term + "%"
	base := r.listBaseQuery(ctx)
	base = r.applyCompanyFilter(base, companyIDs)
	base = base.Where("servers.id::text ILIKE ? OR servers.device_name ILIKE ? OR servers.ip ILIKE ? OR servers.unique_id ILIKE ? OR servers.description ILIKE ? OR servers.server_name ILIKE ? OR servers.crm_id ILIKE ?",
			pattern, pattern, pattern, pattern, pattern, pattern, pattern)

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var servers []server.Server
	if err := r.applyServerListSelect(base.Session(&gorm.Session{})).
		Limit(limit).
		Offset(offset).
		Order("servers.updated_at DESC").
		Find(&servers).Error; err != nil {
		return nil, 0, err
	}

	return servers, total, nil
}

func (r *serverRepo) FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]server.Server, error) {
	var servers []server.Server
	threshold := time.Now().Add(-interval)

	err := r.dbOrTx(ctx, nil).WithContext(ctx).
		Where("ip IS NOT NULL AND ip != ''").
		Where("LOWER(ip) NOT LIKE ? AND LOWER(ip) NOT LIKE ?", "%iikoweb%", "%syrve.app%").
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

func (r *serverRepo) AddAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	conn := r.dbOrTx(ctx, nil).WithContext(ctx)
	var srv server.Server
	if err := conn.Where("id = ?", serverID).First(&srv).Error; err != nil {
		return err
	}
	var comp company.Company
	if err := conn.Where("id = ?", companyID).First(&comp).Error; err != nil {
		return err
	}
	return conn.Model(&srv).Association("AdditionalOwners").Append(&comp)
}

func (r *serverRepo) RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	conn := r.dbOrTx(ctx, nil).WithContext(ctx)
	var srv server.Server
	if err := conn.Where("id = ?", serverID).First(&srv).Error; err != nil {
		return err
	}
	var comp company.Company
	if err := conn.Where("id = ?", companyID).First(&comp).Error; err != nil {
		return err
	}
	return conn.Model(&srv).Association("AdditionalOwners").Delete(&comp)
}
