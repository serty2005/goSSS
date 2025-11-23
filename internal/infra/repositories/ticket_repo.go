package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/tickets"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ticketRepo struct {
	db *gorm.DB
}

// NewTicketRepo создает экземпляр репозитория заявок.
func NewTicketRepo(db *gorm.DB) tickets.TicketRepository {
	return &ticketRepo{db: db}
}

// Upsert создает или обновляет заявку по ServiceDeskUUID.
func (r *ticketRepo) Upsert(ctx context.Context, ticket *tickets.Ticket) error {
	// Настраиваем MetaClass, если он пустой (требование Base)
	if ticket.MetaClass == "" {
		ticket.MetaClass = "serviceCall$serviceCall"
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "service_desk_uuid"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"number",
			"status",
			"request_date",
			"last_modified_date",
			"company_id",
			"contract_id",
			"updated_at", // GORM обновит это поле автоматически, но явное указание безопасно
		}),
	}).Create(ticket).Error
}

// GetByID возвращает заявку по внутреннему ID.
func (r *ticketRepo) GetByID(ctx context.Context, id string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).First(&ticket, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("db: get ticket by id: %w", err)
	}
	return &ticket, nil
}

// GetByServiceDeskUUID возвращает заявку по внешнему UUID.
func (r *ticketRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).First(&ticket, "service_desk_uuid = ?", sdUUID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("db: get ticket by sd uuid: %w", err)
	}
	return &ticket, nil
}

// Find ищет заявки по фильтру с пагинацией и сортировкой.
func (r *ticketRepo) Find(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, error) {
	var items []tickets.Ticket
	query := r.buildQuery(ctx, filter)

	if filter.SortBy != "" {
		query = query.Order(filter.SortBy)
	} else {
		query = query.Order("request_date desc") // Сортировка по умолчанию
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	err := query.Find(&items).Error
	if err != nil {
		return nil, fmt.Errorf("db: find tickets: %w", err)
	}
	return items, nil
}

// Count возвращает количество записей по фильтру (для пагинации).
func (r *ticketRepo) Count(ctx context.Context, filter tickets.TicketFilter) (int64, error) {
	var count int64
	err := r.buildQuery(ctx, filter).Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("db: count tickets: %w", err)
	}
	return count, nil
}

// AssociateAsset обновляет привязку заявки к оборудованию.
func (r *ticketRepo) AssociateAsset(ctx context.Context, ticketID string, assetID string, assetType string) error {
	return r.db.WithContext(ctx).Model(&tickets.Ticket{}).
		Where("id = ?", ticketID).
		Updates(map[string]interface{}{
			"asset_id":   assetID,
			"asset_type": assetType,
		}).Error
}

func (r *ticketRepo) GetActive(ctx context.Context) ([]tickets.Ticket, error) {
	var activeTickets []tickets.Ticket
	// Статусы, которые считаются "конечными"
	finalStatuses := []string{tickets.StatusClosed, tickets.StatusResolved}

	err := r.db.WithContext(ctx).
		Where("status NOT IN ?", finalStatuses).
		Find(&activeTickets).Error

	if err != nil {
		return nil, fmt.Errorf("db: get active tickets: %w", err)
	}
	return activeTickets, nil
}

// buildQuery строит базовый запрос GORM на основе фильтра.
func (r *ticketRepo) buildQuery(ctx context.Context, filter tickets.TicketFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&tickets.Ticket{})

	if filter.CompanyID != "" {
		query = query.Where("company_id = ?", filter.CompanyID)
	}

	if filter.AssetID != nil {
		query = query.Where("asset_id = ?", *filter.AssetID)
	}

	if filter.AssetType != nil {
		query = query.Where("asset_type = ?", *filter.AssetType)
	}

	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}

	return query
}
