package tickets

import (
	"context"
)

// TicketFilter содержит параметры для поиска заявок.
type TicketFilter struct {
	CompanyID string   // Фильтр по компании
	AssetID   *string  // Фильтр по конкретному оборудованию
	AssetType *string  // Фильтр по типу оборудования
	Statuses  []string // Список статусов для выборки
	Limit     int
	Offset    int
	SortBy    string // Поле для сортировки (например, "request_date desc")
}

// TicketRepository определяет методы для работы с хранилищем заявок.
type TicketRepository interface {
	// Upsert создает заявку, если её нет (по ServiceDeskUUID), или обновляет существующую.
	// Возвращает обновленную модель.
	Upsert(ctx context.Context, ticket *Ticket) error

	// GetByID возвращает заявку по внутреннему ID.
	GetByID(ctx context.Context, id string) (*Ticket, error)

	// GetByServiceDeskUUID возвращает заявку по внешнему UUID (ServiceDesk).
	GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*Ticket, error)

	// Find ищет заявки по фильтру.
	Find(ctx context.Context, filter TicketFilter) ([]Ticket, error)

	// Count возвращает количество заявок, соответствующих фильтру (для пагинации).
	Count(ctx context.Context, filter TicketFilter) (int64, error)

	// AssociateAsset привязывает заявку к оборудованию (обновляет AssetID/AssetType).
	AssociateAsset(ctx context.Context, ticketID string, assetID string, assetType string) error
}
