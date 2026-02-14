package repositories

import (
	"context"
	"etalon-server/internal/domain/bitrix"
	infraDB "etalon-server/internal/infra/db"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type bitrixRepo struct {
	db *gorm.DB
}

func NewBitrixRepo(db *gorm.DB) bitrix.Repository {
	return &bitrixRepo{db: db}
}

func (r *bitrixRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *bitrixRepo) UpsertDealLink(ctx context.Context, link *bitrix.DealLink) error {
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ticket_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"b24_deal_id", "last_sync_at", "updated_at"}),
	}).Create(link).Error
}

func (r *bitrixRepo) GetDealLinkByTicketID(ctx context.Context, ticketID string) (*bitrix.DealLink, error) {
	var item bitrix.DealLink
	err := r.getDB(ctx).WithContext(ctx).Where("ticket_id = ?", ticketID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) GetDealLinkByDealID(ctx context.Context, dealID int64) (*bitrix.DealLink, error) {
	var item bitrix.DealLink
	err := r.getDB(ctx).WithContext(ctx).Where("b24_deal_id = ?", dealID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) DeleteDealLinkByTicketID(ctx context.Context, ticketID string) error {
	return r.getDB(ctx).WithContext(ctx).Where("ticket_id = ?", ticketID).Delete(&bitrix.DealLink{}).Error
}

func (r *bitrixRepo) UpsertCommentLink(ctx context.Context, link *bitrix.CommentLink) error {
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "etalon_comment_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"b24_comment_id", "ticket_id", "direction", "updated_at"}),
	}).Create(link).Error
}

func (r *bitrixRepo) GetCommentLinkByEtalonID(ctx context.Context, etalonCommentID string) (*bitrix.CommentLink, error) {
	var item bitrix.CommentLink
	err := r.getDB(ctx).WithContext(ctx).Where("etalon_comment_id = ?", etalonCommentID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) GetCommentLinkByB24ID(ctx context.Context, b24CommentID int64) (*bitrix.CommentLink, error) {
	var item bitrix.CommentLink
	err := r.getDB(ctx).WithContext(ctx).Where("b24_comment_id = ?", b24CommentID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) UpsertUserMap(ctx context.Context, item *bitrix.UserMap) error {
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "etalon_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"b24_user_id", "updated_at"}),
	}).Create(item).Error
}

func (r *bitrixRepo) GetUserMapByEtalonID(ctx context.Context, etalonUserID uint) (*bitrix.UserMap, error) {
	var item bitrix.UserMap
	err := r.getDB(ctx).WithContext(ctx).Where("etalon_user_id = ?", etalonUserID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) GetUserMapByB24ID(ctx context.Context, b24UserID int64) (*bitrix.UserMap, error) {
	var item bitrix.UserMap
	err := r.getDB(ctx).WithContext(ctx).Where("b24_user_id = ?", b24UserID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) ReplaceServicePoints(ctx context.Context, points []bitrix.ServicePoint) error {
	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(points) == 0 {
			return tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&bitrix.ServicePoint{}).Error
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "b24_element_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"address",
				"raw_json",
				"updated_at",
			}),
		}).CreateInBatches(points, 200).Error; err != nil {
			return err
		}

		ids := make([]int64, 0, len(points))
		for _, point := range points {
			ids = append(ids, point.B24ElementID)
		}

		return tx.Where("b24_element_id NOT IN ?", ids).Delete(&bitrix.ServicePoint{}).Error
	})
}

func (r *bitrixRepo) ListServicePoints(ctx context.Context) ([]bitrix.ServicePoint, error) {
	var items []bitrix.ServicePoint
	err := r.getDB(ctx).WithContext(ctx).Order("name asc").Find(&items).Error
	return items, err
}

func (r *bitrixRepo) SearchServicePoints(ctx context.Context, term string, limit, offset int, randomWhenEmpty bool) ([]bitrix.ServicePoint, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := r.getDB(ctx).WithContext(ctx).Model(&bitrix.ServicePoint{})
	normalizedTerm := strings.TrimSpace(term)
	if normalizedTerm != "" {
		mask := "%" + normalizedTerm + "%"
		query = query.Where("name ILIKE ? OR one_c_code ILIKE ? OR address ILIKE ?", mask, mask, mask)
		query = query.Order("name asc")
	} else if randomWhenEmpty {
		query = query.Order("RANDOM()")
	} else {
		query = query.Order("name asc")
	}

	var items []bitrix.ServicePoint
	err := query.Limit(limit).Offset(offset).Find(&items).Error
	return items, err
}

func (r *bitrixRepo) ReplaceUserCache(ctx context.Context, users []bitrix.UserCache) error {
	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&bitrix.UserCache{}).Error; err != nil {
			return err
		}
		if len(users) == 0 {
			return nil
		}
		return tx.CreateInBatches(users, 200).Error
	})
}

func (r *bitrixRepo) ListUserCache(ctx context.Context) ([]bitrix.UserCache, error) {
	var items []bitrix.UserCache
	err := r.getDB(ctx).WithContext(ctx).Order("name asc").Find(&items).Error
	return items, err
}

func (r *bitrixRepo) UpdateServicePointOneCData(ctx context.Context, b24ElementID int64, oneCCode string, contractOn *bool) error {
	normalizedCode := strings.TrimSpace(oneCCode)
	if normalizedCode == "" {
		return nil
	}

	updates := map[string]interface{}{
		"one_c_code":        normalizedCode,
		"one_c_contract_on": contractOn,
		"updated_at":        time.Now(),
	}

	return r.getDB(ctx).WithContext(ctx).
		Model(&bitrix.ServicePoint{}).
		Where("b24_element_id = ?", b24ElementID).
		Updates(updates).Error
}

func (r *bitrixRepo) GetServicePointByID(ctx context.Context, b24ElementID int64) (*bitrix.ServicePoint, error) {
	var item bitrix.ServicePoint
	err := r.getDB(ctx).WithContext(ctx).Where("b24_element_id = ?", b24ElementID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) ListServicePointsByIDs(ctx context.Context, ids []int64) ([]bitrix.ServicePoint, error) {
	if len(ids) == 0 {
		return []bitrix.ServicePoint{}, nil
	}
	var items []bitrix.ServicePoint
	err := r.getDB(ctx).WithContext(ctx).Where("b24_element_id IN ?", ids).Find(&items).Error
	return items, err
}

func (r *bitrixRepo) UpsertCompanyServicePointMapping(ctx context.Context, item *bitrix.CompanyServicePointMapping) error {
	if item == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "company_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"bitrix_service_point_id", "updated_at"}),
	}).Create(item).Error
}

func (r *bitrixRepo) GetCompanyServicePointMappingByCompanyID(ctx context.Context, companyID string) (*bitrix.CompanyServicePointMapping, error) {
	var item bitrix.CompanyServicePointMapping
	err := r.getDB(ctx).WithContext(ctx).Where("company_id = ?", companyID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) GetCompanyServicePointMappingByPointID(ctx context.Context, bitrixServicePointID int64) (*bitrix.CompanyServicePointMapping, error) {
	var item bitrix.CompanyServicePointMapping
	err := r.getDB(ctx).WithContext(ctx).Where("bitrix_service_point_id = ?", bitrixServicePointID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *bitrixRepo) ListCompanyServicePointMappingsByCompanyIDs(ctx context.Context, companyIDs []string) ([]bitrix.CompanyServicePointMapping, error) {
	if len(companyIDs) == 0 {
		return []bitrix.CompanyServicePointMapping{}, nil
	}
	var items []bitrix.CompanyServicePointMapping
	err := r.getDB(ctx).WithContext(ctx).Where("company_id IN ?", companyIDs).Find(&items).Error
	return items, err
}

func (r *bitrixRepo) DeleteCompanyServicePointMappingByCompanyID(ctx context.Context, companyID string) error {
	return r.getDB(ctx).WithContext(ctx).Where("company_id = ?", companyID).Delete(&bitrix.CompanyServicePointMapping{}).Error
}

func (r *bitrixRepo) DeleteCompanyServicePointMappingByPointID(ctx context.Context, bitrixServicePointID int64) error {
	return r.getDB(ctx).WithContext(ctx).Where("bitrix_service_point_id = ?", bitrixServicePointID).Delete(&bitrix.CompanyServicePointMapping{}).Error
}
