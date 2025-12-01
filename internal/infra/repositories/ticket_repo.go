package repositories

import (
	"context"
	"errors"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/tickets"
	"fmt"

	"gorm.io/gorm"
)

type ticketRepo struct {
	db *gorm.DB
}

// NewTicketRepo создает экземпляр репозитория заявок.
func NewTicketRepo(db *gorm.DB) tickets.TicketRepository {
	return &ticketRepo{db: db}
}

// Upsert теперь работает иначе. Он ожидает, что Gateway сначала найдет ID через таблицу связей.
// Если ticket.ID пустой -> Create. Если заполнен -> Save.
func (r *ticketRepo) Upsert(ctx context.Context, ticket *tickets.Ticket) error {
	if ticket.MetaClass == "" {
		ticket.MetaClass = "serviceCall$serviceCall"
	}

	// Проверяем, передан ли ID.
	if ticket.ID != "" {
		// Обновление существующей записи
		// Используем Select для обновления только нужных полей, чтобы не затереть ID
		return r.db.WithContext(ctx).Model(ticket).Select(
			"Number", "Status", "Subject", "LastComment",
			"RequestDate", "LastModifiedDate", "CompanyID", "ContractID",
		).Updates(ticket).Error
	}

	// Создание новой записи (ID сгенерируется в BeforeCreate)
	return r.db.WithContext(ctx).Create(ticket).Error
}

// GetByID возвращает заявку по внутреннему ID.
func (r *ticketRepo) GetByID(ctx context.Context, id string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	// Добавляем выборку companies.title как CompanyName
	err := r.db.WithContext(ctx).Table("tickets").
		Select("tickets.*, links.service_desk_uuid, companies.title as company_name").
		Joins("LEFT JOIN external_system_links links ON links.internal_id = tickets.id AND links.system_name = ?", "naumen").
		Joins("LEFT JOIN companies ON companies.id = tickets.company_id").
		Where("tickets.id = ?", id).
		Scan(&ticket).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, fmt.Errorf("db: get ticket by id: %w", err)
	}
	return &ticket, nil
}

// GetByNumber возвращает заявку по номеру.
func (r *ticketRepo) GetByNumber(ctx context.Context, number int) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	// Ищем только в таблице tickets, так как связи может и не быть
	err := r.db.WithContext(ctx).
		Where("number = ?", number).
		First(&ticket).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("db: get ticket by number: %w", err)
	}
	return &ticket, nil
}

// GetByServiceDeskUUID возвращает заявку по внешнему UUID.
func (r *ticketRepo) GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*tickets.Ticket, error) {
	var ticket tickets.Ticket
	// Ищем через таблицу линков
	err := r.db.WithContext(ctx).Table("tickets").
		Select("tickets.*, links.service_desk_uuid").
		Joins("JOIN external_system_links links ON links.internal_id = tickets.id").
		Where("links.system_name = ? AND links.service_desk_uuid = ?", "naumen", sdUUID).
		First(&ticket).Error

	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &ticket, nil
}

// Find использует обновленный buildQuery
func (r *ticketRepo) Find(ctx context.Context, filter tickets.TicketFilter) ([]tickets.Ticket, error) {
	var items []tickets.Ticket
	query := r.buildQuery(ctx, filter)

	if filter.SortBy != "" {
		query = query.Order(filter.SortBy)
	} else {
		query = query.Order("tickets.request_date desc")
	}

	if filter.Limit > 0 {
		query = query.Limit(filter.Limit)
	}
	if filter.Offset > 0 {
		query = query.Offset(filter.Offset)
	}

	// Scan автоматически замапит service_desk_uuid из выборки в поле структуры (благодаря gorm:"-")
	// если имена колонок совпадают.
	err := query.Scan(&items).Error
	if err != nil {
		return nil, fmt.Errorf("db: find tickets: %w", err)
	}
	return items, nil
}

// Count возвращает количество записей по фильтру (для пагинации).
func (r *ticketRepo) Count(ctx context.Context, filter tickets.TicketFilter) (int64, error) {
	var count int64
	// buildQuery уже содержит таблицу tickets
	err := r.buildQuery(ctx, filter).Count(&count).Error
	if err != nil {
		return 0, fmt.Errorf("db: count tickets: %w", err)
	}
	return count, nil
}

func (r *ticketRepo) GetActive(ctx context.Context) ([]tickets.Ticket, error) {
	var activeTickets []tickets.Ticket
	finalStatuses := []string{tickets.StatusClosed, tickets.StatusResolved}

	// Здесь нужен SD UUID для проверки зомби, так что добавляем JOIN
	err := r.db.WithContext(ctx).Table("tickets").
		Select("tickets.*, links.service_desk_uuid").
		Joins("LEFT JOIN external_system_links links ON links.internal_id = tickets.id AND links.system_name = ?", "naumen").
		Where("tickets.status NOT IN ?", finalStatuses).
		Scan(&activeTickets).Error

	if err != nil {
		return nil, fmt.Errorf("db: get active tickets: %w", err)
	}
	return activeTickets, nil
}

// AssociateAsset - без изменений
func (r *ticketRepo) AssociateAsset(ctx context.Context, ticketID string, assetID string, assetType string) error {
	return r.db.WithContext(ctx).Model(&tickets.Ticket{}).
		Where("id = ?", ticketID).
		Updates(map[string]interface{}{
			"asset_id":   assetID,
			"asset_type": assetType,
		}).Error
}

// buildQuery обновлен для JOIN с таблицей связей
func (r *ticketRepo) buildQuery(ctx context.Context, filter tickets.TicketFilter) *gorm.DB {
	// Выбираем все поля тикета + service_desk_uuid из таблицы связей
	query := r.db.WithContext(ctx).Table("tickets").
		Select("tickets.*, links.service_desk_uuid").
		Joins("LEFT JOIN external_system_links links ON links.internal_id = tickets.id AND links.system_name = ?", "naumen")

	if filter.CompanyID != "" {
		query = query.Where("tickets.company_id = ?", filter.CompanyID)
	}
	if filter.AssetID != nil {
		query = query.Where("tickets.asset_id = ?", *filter.AssetID)
	}
	if filter.AssetType != nil {
		query = query.Where("tickets.asset_type = ?", *filter.AssetType)
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("tickets.status IN ?", filter.Statuses)
	}
	if filter.SearchQuery != "" {
		q := "%" + filter.SearchQuery + "%"
		query = query.Where("CAST(tickets.number AS TEXT) ILIKE ? OR tickets.status ILIKE ? OR tickets.subject ILIKE ?", q, q, q)
	}

	return query
}
