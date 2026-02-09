package integration

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/workstation"
	"time"
)

// EntitySummary представляет краткую информацию о сущности для быстрой сверки.
type EntitySummary struct {
	ExternalID string
	UpdatedAt  time.Time
}

// CommonProvider определяет базовые методы для любого провайдера.
type CommonProvider interface {
	// SystemName возвращает уникальное имя системы (например, "naumen", "zabbix").
	// Это имя используется в таблице external_system_links.
	SystemName() string
}

// InventoryProvider - методы для получения инвентарных данных.
type InventoryProvider interface {
	CommonProvider

	// Summaries (для инкрементальной синхронизации)
	GetCompanySummaries(ctx context.Context) ([]EntitySummary, error)
	GetServerSummaries(ctx context.Context) ([]EntitySummary, error)
	GetWorkstationSummaries(ctx context.Context) ([]EntitySummary, error)
	GetFiscalRegisterSummaries(ctx context.Context) ([]EntitySummary, error)

	// Single Entity (для получения деталей)
	GetCompany(ctx context.Context, externalID string) (*company.Company, error)
	GetServer(ctx context.Context, externalID string) (*server.Server, error)
	GetWorkstation(ctx context.Context, externalID string) (*workstation.Workstation, error)
	GetFiscalRegister(ctx context.Context, externalID string) (*fiscal.FiscalRegister, error)

	// Bulk Fetch (для полных сверок, например FRUpdateFounder).
	// Возвращает Map: ExternalID -> Domain Model
	GetAllFiscalRegisters(ctx context.Context) (map[string]*fiscal.FiscalRegister, error)

	// --- Write (Create/Update) ---
	// Методы возвращают ExternalID созданной сущности
	CreateFiscalRegister(ctx context.Context, fr *fiscal.FiscalRegister) (string, error)
	UpdateFiscalRegister(ctx context.Context, externalID string, fr *fiscal.FiscalRegister) error
}

// ContractProvider определяет методы для работы с контрактами.
type ContractProvider interface {
	CommonProvider
	// GetContracts возвращает Map: ExternalID -> Contract Model
	GetContracts(ctx context.Context) (map[string]*contract.Contract, error)
	// GetCompanyContractStates возвращает карту статусов контрактов для компаний (ExternalID -> Active).
	GetCompanyContractStates(ctx context.Context) (map[string]bool, error)
}

// TicketProvider определяет методы для работы с заявками.
type TicketProvider interface {
	CommonProvider
	// GetTickets возвращает Map: ExternalID -> Ticket Model
	GetTickets(ctx context.Context, statuses []string) (map[string]*tickets.Ticket, error)

	// GetComments возвращает комментарии тикета из внешней системы.
	GetComments(ctx context.Context, ticketExternalID string) ([]*tickets.Comment, error)
	// GetCommentsBySources возвращает комментарии, сгруппированные по source UUID.
	GetCommentsBySources(ctx context.Context, sourceUUIDs []string) (map[string][]*tickets.Comment, error)

	// GetFilesBySource возвращает файлы, связанные с source UUID (например, тикетом).
	GetFilesBySource(ctx context.Context, sourceUUID string) ([]RemoteFile, error)
	// GetFilesBySources возвращает файлы, сгруппированные по source UUID.
	GetFilesBySources(ctx context.Context, sourceUUIDs []string) (map[string][]RemoteFile, error)

	// DownloadFile скачивает файл по его внешнему UUID.
	DownloadFile(ctx context.Context, fileUUID string) ([]byte, string, error)

	// CreateTicket создает заявку во внешней системе и возвращает её ID.
	CreateTicket(ctx context.Context, ticket *tickets.Ticket) (string, error)
	// UpdateTicket обновляет заявку во внешней системе.
	UpdateTicket(ctx context.Context, externalID string, data map[string]interface{}) error
}

// RemoteFile метаданные файла во внешней системе.
type RemoteFile struct {
	UUID     string
	Name     string
	MimeType string
	Size     int64
}
