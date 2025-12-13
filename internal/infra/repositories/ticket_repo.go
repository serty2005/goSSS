package repositories

import (
	"context"
	"errors"

	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/tickets"

	"gorm.io/gorm"
)

type ticketRepo struct {
	db *gorm.DB
}

func NewTicketRepo(db *gorm.DB) tickets.TicketRepository {
	return &ticketRepo{db: db}
}

func (r *ticketRepo) Create(ctx context.Context, ticket *tickets.Ticket) error {
	return r.db.WithContext(ctx).Create(ticket).Error
}

func (r *ticketRepo) Update(ctx context.Context, ticket *tickets.Ticket) error {
	// Обновляем все поля модели
	return r.db.WithContext(ctx).Save(ticket).Error
}

func (r *ticketRepo) GetByID(ctx context.Context, id string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	// Preload связей: Исполнитель, Репортер
	err := r.db.WithContext(ctx).
		Preload("Assignee").
		Preload("Reporter").
		Where("id = ?", id).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &ticket, nil
}

func (r *ticketRepo) GetByNumber(ctx context.Context, number int) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Preload("Assignee").
		Where("number = ?", number).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &ticket, err
}

func (r *ticketRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	err := r.db.WithContext(ctx).
		Where("service_desk_uuid = ?", sdUUID).
		First(&ticket).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil // Возвращаем nil, если не найдено (для логики синхронизации)
	}
	return &ticket, err
}

func (r *ticketRepo) Find(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, error) {
	var items []tickets.Ticket
	query := r.buildQuery(ctx, filter)

	if filter.SortBy != "" {
		query = query.Order(filter.SortBy)
	} else {
		query = query.Order("created_at desc")
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	err := query.Preload("Assignee").Find(&items).Error
	return items, err
}

func (r *ticketRepo) AssociateAsset(ctx context.Context, ticketID, assetID, assetType string) error {
	return r.db.WithContext(ctx).Model(&tickets.Ticket{}).Where("id = ?", ticketID).Updates(map[string]interface{}{
		"asset_id":   assetID,
		"asset_type": assetType,
	}).Error
}

func (r *ticketRepo) Count(ctx context.Context, filter tickets.TicketFilter) (int64, error) {
	var count int64
	err := r.buildQuery(ctx, filter).Count(&count).Error
	return count, err
}

func (r *ticketRepo) buildQuery(ctx context.Context, filter tickets.TicketFilter) *gorm.DB {
	query := r.db.WithContext(ctx).Model(&tickets.Ticket{})

	if filter.CompanyID != "" {
		query = query.Where("company_id = ?", filter.CompanyID)
	}
	if filter.AssetID != nil {
		query = query.Where("asset_id = ?", *filter.AssetID)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("status IN ?", filter.Statuses)
	}
	if filter.AssigneeID != nil {
		query = query.Where("assignee_id = ?", *filter.AssigneeID)
	}
	if filter.ReporterID != nil {
		query = query.Where("reporter_id = ?", *filter.ReporterID)
	}
	if filter.SearchQuery != "" {
		q := "%" + filter.SearchQuery + "%"
		// Поиск по номеру (преобразуем в текст) или теме
		query = query.Where("CAST(number AS TEXT) ILIKE ? OR subject ILIKE ?", q, q)
	}

	return query
}

// --- History & Attachments ---

func (r *ticketRepo) AddHistory(ctx context.Context, history *tickets.TicketHistory) error {
	return r.db.WithContext(ctx).Create(history).Error
}

func (r *ticketRepo) GetHistory(ctx context.Context, ticketID string) ([]tickets.TicketHistory, error) {
	var history []tickets.TicketHistory
	err := r.db.WithContext(ctx).Where("ticket_id = ?", ticketID).Order("created_at desc").Find(&history).Error
	return history, err
}

func (r *ticketRepo) AddAttachment(ctx context.Context, attachment *tickets.Attachment) error {
	return r.db.WithContext(ctx).Create(attachment).Error
}

func (r *ticketRepo) GetAttachments(ctx context.Context, ticketID string) ([]tickets.Attachment, error) {
	var attachments []tickets.Attachment
	err := r.db.WithContext(ctx).
		Where("entity_id = ? AND entity_type = ?", ticketID, "Ticket").
		Find(&attachments).Error
	return attachments, err
}
