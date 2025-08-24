package events

import "time"

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
)

// ServiceDeskEntityPayload - полезная нагрузка для события ServiceDeskEntityUpdated.
type ServiceDeskEntityPayload struct {
	MetaClass string                 // Метакласс сущности, например, "ou$company"
	UUID      string                 // UUID сущности
	Data      map[string]interface{} // Полные данные сущности, полученные от ServiceDesk
}

// ServiceDeskEntityDeletePayload - полезная нагрузка для события ServiceDeskEntityDeleted.
type ServiceDeskEntityDeletePayload struct {
	MetaClass string // Метакласс сущности
	UUID      string // UUID удаленной сущности
}

// ContractsStatusPayload - полезная нагрузка для события ContractsStatusRecalculated.
type ContractsStatusPayload struct {
	// Карта, где ключ - UUID компании, а значение - флаг, активен ли у нее контракт.
	CompanyActiveContract map[string]bool
}

// DuplicatesFoundPayload - полезная нагрузка для события DuplicatesFound.
type DuplicatesFoundPayload struct {
	EntityType string   // 'Server', 'Workstation', 'FiscalRegister'
	Field      string   // Поле, по которому найдены дубликаты ('ip', 'anydesk', и т.д.)
	Value      string   // Значение поля, которое дублируется
	UUIDs      []string // Список ServiceDesk UUID всех сущностей в группе дубликатов
}
// ServerPollingSucceededPayload - полезная нагрузка для успешного опроса.
type ServerPollingSucceededPayload struct {
	ServerUUID     string
	ServerName     string
	ServerEdition  string
	ServerVersion  string
	NewStatus      string
	LastPolledAt   time.Time
}

// ServerPollingFailedPayload - полезная нагрузка для неудачного опроса.
type ServerPollingFailedPayload struct {
	ServerUUID   string
	NewStatus    string // 'offline' или 'archived'
	ErrorMessage string
	LastPolledAt time.Time
}

// ServerPollingRequestedPayload - полезная нагрузка для ручного запуска.
type ServerPollingRequestedPayload struct {
	ServerUUID string
}