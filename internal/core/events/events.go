package events

import (
	api "etalon-server/internal/transport/http/dtos"
	"time"
)

// Константы для типов событий.
const (
	// ContractsStatusRecalculated событие возникает, когда шлюз контрактов завершил синхронизацию и пересчет статусов.
	ContractsStatusRecalculated = "contracts.status.recalculated"

	// ServiceDeskEntityUpdated событие возникает, когда из ServiceDesk получены обновленные данные о сущности.
	ServiceDeskEntityUpdated = "servicedesk.entity.updated"

	// ServiceDeskEntityDeleted событие возникает, когда сущность была удалена в ServiceDesk.
	ServiceDeskEntityDeleted = "servicedesk.entity.deleted"

	// AgentDataReceived событие возникает, когда от агента (по API или через FTP) получены данные.
	AgentDataReceived = "agent.data.received"

	// DuplicatesFound событие возникает, когда воркер поиска обнаружил дубликаты.
	DuplicatesFound = "duplicates.found"

	// ServerPollingSucceeded событие возникает при успешном опросе статуса сервера.
	ServerPollingSucceeded = "server.polling.succeeded"
	// ServerPollingFailed событие возникает при неудачном опросе статуса сервера.
	ServerPollingFailed = "server.polling.failed"
	// ServerPollingRequested событие для ручного запуска опроса одного сервера.
	ServerPollingRequested = "server.polling.requested"

	// ServiceDeskCreateRequested событие для асинхронного создания сущности в ServiceDesk.
	ServiceDeskCreateRequested = "servicedesk.entity.create.requested"
	// ServiceDeskUpdateRequested событие для асинхронного обновления сущности в ServiceDesk.
	ServiceDeskUpdateRequested = "servicedesk.entity.update.requested"
	// FiscalRegisterDiscrepancyFound событие возникает, когда воркер обнаружил расхождение данных ФР.
	FiscalRegisterDiscrepancyFound = "discrepancy.fiscal_register.found"

	// TicketUpdated событие возникает, когда воркер обнаружил обновление тикета.
	TicketUpdated = "ticket.updated"
)

// ServiceDeskEntityPayload - полезная нагрузка для события ServiceDeskEntityUpdated.
// Содержит сырые данные из внешней системы.
type ServiceDeskEntityPayload struct {
	EntityType      string                 // Внутренний тип сущности: "Company", "Server"
	ServiceDeskUUID string                 // UUID сущности во внешней системе
	Data            map[string]interface{} // Полные данные сущности, полученные от внешней системы
}

// ServiceDeskEntityDeletePayload - полезная нагрузка для события ServiceDeskEntityDeleted.
type ServiceDeskEntityDeletePayload struct {
	EntityType      string // Внутренний тип сущности
	ServiceDeskUUID string // UUID удаленной сущности во внешней системе
}

// ContractsStatusPayload - полезная нагрузка для события ContractsStatusRecalculated.
type ContractsStatusPayload struct {
	// Карта, где ключ - ВНУТРЕННИЙ ID компании, а значение - флаг, активен ли у нее контракт.
	CompanyActiveContract map[string]bool
}

// DuplicatesFoundPayload - полезная нагрузка для события DuplicatesFound.
type DuplicatesFoundPayload struct {
	EntityType  string   // 'Server', 'Workstation', 'FiscalRegister'
	Field       string   // Поле, по которому найдены дубликаты ('ip', 'anydesk', и т.д.)
	Value       string   // Значение поля, которое дублируется
	InternalIDs []string // Список ВНУТРЕННИХ UUID всех сущностей в группе дубликатов
}

// ServerPollingSucceededPayload - полезная нагрузка для успешного опроса.
type ServerPollingSucceededPayload struct {
	ServerUUID    string
	RequestID     string // Идентификатор запроса для связывания логов
	ServerName    string
	ServerEdition string
	ServerVersion string
	NewStatus     string
	LastPolledAt  time.Time
}

// ServerPollingFailedPayload - полезная нагрузка для неудачного опроса.
type ServerPollingFailedPayload struct {
	ServerUUID   string
	RequestID    string // Идентификатор запроса для связывания логов
	NewStatus    string // 'offline', 'archived' или 'undefined'
	ErrorMessage string
	LastPolledAt time.Time
}

// ServerPollingRequestedPayload - полезная нагрузка для ручного запуска.
type ServerPollingRequestedPayload struct {
	ServerUUID string
}

// ServiceDeskModificationPayload - общая полезная нагрузка для событий создания/обновления в ServiceDesk.
type ServiceDeskModificationPayload struct {
	TaskID            uint                   `json:"task_id"`
	EntityType        string                 `json:"entity_type"`
	EntityUUID        string                 `json:"entity_uuid,omitempty"` // Пусто для создания
	TriggeredByUserID string                 `json:"triggered_by_user_id"`
	PayloadForSD      map[string]interface{} `json:"payload_for_sd"` // Данные для отправки в SD
}

// DiscrepancyDetail описывает расхождение по одному полю.
type DiscrepancyDetail struct {
	EtalonValue      interface{} `json:"etalon_value"`
	ServiceDeskValue interface{} `json:"service_desk_value"`
}

// FiscalRegisterDiscrepancyPayload - полезная нагрузка для события FiscalRegisterDiscrepancyFound.
type FiscalRegisterDiscrepancyPayload struct {
	FRInternalUUID    string                       `json:"fr_internal_uuid"`
	FRServiceDeskUUID string                       `json:"fr_service_desk_uuid"`
	Discrepancies     map[string]DiscrepancyDetail `json:"discrepancies"` // Карта: имя поля -> детали расхождения
}

// AgentDataPayload - полезная нагрузка для события AgentDataReceived.
type AgentDataPayload struct {
	Source string           // Источник данных: имя файла для FTP или UUID агента для API
	Data   api.AgentDataDTO // Сами данные, полученные от агента
}
