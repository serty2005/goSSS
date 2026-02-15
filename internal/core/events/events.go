// Package events определяет типы событий и их payload для событийно-ориентированной архитектуры.
// События используются для асинхронного взаимодействия между компонентами системы через EventBus.
//
// Паттерн использования:
//   - Издатель: публикует событие через eventBus.Publish(eventName, payload)
//   - Подписчик: регистрирует обработчик через eventBus.Subscribe(eventName, handler)
//
// Все события именуются в формате: <домен>.<сущность>.<действие>
// Например: "agent.data.received", "servicedesk.entity.updated"
package events

import (
	"etalon-server/internal/domain/tickets"
	api "etalon-server/internal/transport/http/dtos"
	"time"
)

// Константы типов событий системы.
// Каждая константа определяет уникальный идентификатор события для шины событий.
const (
	// ContractsStatusRecalculated — событие завершения синхронизации контрактов.
	// Публикуется: ContractGateway после обновления статусов контрактов.
	// Подписчики: компоненты, зависящие от статуса контрактов (например, расчёт SLA).
	// Payload: ContractsStatusPayload.
	ContractsStatusRecalculated = "contracts.status.recalculated"

	// ServiceDeskEntityUpdated — событие обновления сущности в ServiceDesk (Naumen).
	// Публикуется: ServiceDeskSyncWorker при получении обновлений из внешней системы.
	// Подписчики: репозитории для обновления локальных данных.
	// Payload: ServiceDeskEntityPayload.
	ServiceDeskEntityUpdated = "servicedesk.entity.updated"

	// ServiceDeskEntityDeleted — событие удаления сущности в ServiceDesk (Naumen).
	// Публикуется: ServiceDeskSyncWorker при обнаружении удалённой сущности.
	// Подписчики: репозитории для мягкого удаления локальных данных.
	// Payload: ServiceDeskEntityDeletePayload.
	ServiceDeskEntityDeleted = "servicedesk.entity.deleted"

	// AgentDataReceived — событие получения данных от агента.
	// Публикуется: AgentFTPGateway (из JSON-файлов) или HTTP-хендлер (прямая отправка).
	// Подписчики: Orchestrator для обработки данных агента.
	// Payload: AgentDataPayload.
	AgentDataReceived = "agent.data.received"

	// AgentObservationRequested — событие запроса на применение наблюдения агента.
	// Публикуется: Orchestrator после валидации данных AgentDataReceived.
	// Подписчики: Engine для создания/обновления доменных сущностей.
	// Payload: AgentObservationPayload.
	AgentObservationRequested = "agent.observation.requested"

	// DuplicatesFound — событие обнаружения дубликатов оборудования.
	// Публикуется: DuplicatesWorker при сканировании БД.
	// Подписчики: UI для отображения предупреждений, Reconciliation для создания задач.
	// Payload: DuplicatesFoundPayload.
	DuplicatesFound = "duplicates.found"

	// ServerPollingSucceeded — событие успешного опроса сервера.
	// Публикуется: ServerPollingGateway при успешном ответе от сервера.
	// Подписчики: репозитории для обновления статуса сервера.
	// Payload: ServerPollingSucceededPayload.
	ServerPollingSucceeded = "server.polling.succeeded"

	// ServerPollingFailed — событие неудачного опроса сервера.
	// Публикуется: ServerPollingGateway при ошибке опроса.
	// Подписчики: репозитории для обновления статуса, алертинг.
	// Payload: ServerPollingFailedPayload.
	ServerPollingFailed = "server.polling.failed"

	// ServerPollingRequested — событие запроса ручного опроса сервера.
	// Публикуется: HTTP-хендлер при запросе пользователя.
	// Подписчики: ServerPollingGateway для выполнения опроса.
	// Payload: ServerPollingRequestedPayload.
	ServerPollingRequested = "server.polling.requested"

	// ServiceDeskCreateRequested — событие запроса создания сущности в ServiceDesk.
	// Публикуется: HTTP-хендлеры при создании новых сущностей.
	// Подписчики: ServiceDeskWorker для асинхронной отправки в Naumen.
	// Payload: ServiceDeskModificationPayload.
	ServiceDeskCreateRequested = "servicedesk.entity.create.requested"

	// ServiceDeskUpdateRequested — событие запроса обновления сущности в ServiceDesk.
	// Публикуется: HTTP-хендлеры при изменении сущностей.
	// Подписчики: ServiceDeskWorker для асинхронной отправки в Naumen.
	// Payload: ServiceDeskModificationPayload.
	ServiceDeskUpdateRequested = "servicedesk.entity.update.requested"

	// FiscalRegisterDiscrepancyFound — событие обнаружения расхождения данных ФР.
	// Публикуется: DiscrepancyWorker при сравнении локальных данных с ServiceDesk.
	// Подписчики: Reconciliation для создания задач на согласование.
	// Payload: FiscalRegisterDiscrepancyPayload.
	FiscalRegisterDiscrepancyFound = "discrepancy.fiscal_register.found"

	// TicketUpdated — событие обновления тикета в ServiceDesk.
	// Публикуется: TicketSyncWorker при обнаружении изменений.
	// Подписчики: UI через WebSocket для обновления в реальном времени.
	// Payload: ID тикета.
	TicketUpdated = "ticket.updated"
	// BitrixTicketSyncRequested публикуется при изменении тикета, требующем синхронизации в Bitrix24.
	// Payload: BitrixSyncEntityPayload.
	BitrixTicketSyncRequested = "bitrix.ticket.sync.requested"
	// BitrixCommentSyncRequested публикуется при добавлении публичного комментария в тикет.
	// Payload: BitrixSyncEntityPayload.
	BitrixCommentSyncRequested = "bitrix.comment.sync.requested"
)

// BitrixSyncEntityPayload — унифицированная сущность для событий исходящей синхронизации Bitrix24.
// Для событий тикета заполняются TicketID и Reason.
// Для событий комментария дополнительно заполняются Comment и EtalonUserID.
type BitrixSyncEntityPayload struct {
	TicketID     string
	Reason       string
	Comment      *tickets.TicketComment
	EtalonUserID *uint
}

// TicketUpdatedPayload — полезная нагрузка события TicketUpdated.
// Используется фронтендом для реактивного обновления карточек тикетов и уведомлений.
type TicketUpdatedPayload struct {
	TicketID   string    `json:"ticket_id"`
	Action     string    `json:"action"`
	Source     string    `json:"source"`
	Message    string    `json:"message,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
}

// ServiceDeskEntityPayload — полезная нагрузка для события ServiceDeskEntityUpdated.
// Содержит полные данные сущности, полученные из ServiceDesk (Naumen).
type ServiceDeskEntityPayload struct {
	// EntityType — внутренний тип сущности Etalon.
	// Значения: "Company", "Server", "Workstation", "Fiscal".
	EntityType string

	// ServiceDeskUUID — идентификатор сущности во внешней системе ServiceDesk (Naumen).
	ServiceDeskUUID string

	// Data — полные данные сущности.
	// Тип interface{} позволяет принимать как map[string]interface{} (legacy JSON),
	// так и типизированные структуры (*server.Server и т.д.).
	Data interface{}
}

// ServiceDeskEntityDeletePayload — полезная нагрузка для события ServiceDeskEntityDeleted.
// Содержит минимальные данные для идентификации удалённой сущности.
type ServiceDeskEntityDeletePayload struct {
	// EntityType — внутренний тип сущности Etalon.
	EntityType string

	// ServiceDeskUUID — идентификатор удалённой сущности во внешней системе.
	ServiceDeskUUID string
}

// ContractsStatusPayload — полезная нагрузка для события ContractsStatusRecalculated.
// Содержит карту статусов контрактов для всех компаний.
type ContractsStatusPayload struct {
	// CompanyActiveContract — карта активностей контрактов по компаниям.
	// Ключ: внутренний UUID компании.
	// Значение: true — активный контракт, false — неактивный/отсутствует.
	CompanyActiveContract map[string]bool
}

// DuplicatesFoundPayload — полезная нагрузка для события DuplicatesFound.
// Описывает группу дубликатов, обнаруженных по одному полю.
type DuplicatesFoundPayload struct {
	// EntityType — тип оборудования с дубликатами.
	// Значения: "Server", "Workstation", "FiscalRegister".
	EntityType string

	// Field — поле, по которому обнаружены дубликаты.
	// Значения: "ip", "anydesk", "serial_number", "hostname" и др.
	Field string

	// Value — значение поля, которое повторяется у нескольких сущностей.
	Value string

	// InternalIDs — список внутренних UUID всех сущностей в группе дубликатов.
	// Используется для отображения в UI и создания задач на разрешение.
	InternalIDs []string
}

// ServerPollingSucceededPayload — полезная нагрузка для события ServerPollingSucceeded.
// Содержит результаты успешного опроса сервера.
type ServerPollingSucceededPayload struct {
	// ServerUUID — внутренний UUID сервера.
	ServerUUID string

	// RequestID — идентификатор запроса для трассировки в логах.
	RequestID string

	// ServerName — имя сервера для логирования и отображения.
	ServerName string

	// ServerEdition — редакция ОС сервера (например, "Standard", "Datacenter").
	ServerEdition string

	// ServerVersion — версия ОС сервера.
	ServerVersion string

	// NewStatus — новый статус сервера после опроса.
	// Значения: "online", "offline", "archived", "undefined".
	NewStatus string

	// LastPolledAt — время последнего успешного опроса.
	LastPolledAt time.Time
}

// ServerPollingFailedPayload — полезная нагрузка для события ServerPollingFailed.
// Содержит информацию об ошибке опроса сервера.
type ServerPollingFailedPayload struct {
	// ServerUUID — внутренний UUID сервера.
	ServerUUID string

	// RequestID — идентификатор запроса для трассировки в логах.
	RequestID string

	// NewStatus — статус сервера после неудачного опроса.
	// Значения: "offline", "archived", "undefined".
	NewStatus string

	// ErrorMessage — текст ошибки опроса.
	ErrorMessage string

	// LastPolledAt — время попытки опроса.
	LastPolledAt time.Time
}

// ServerPollingRequestedPayload — полезная нагрузка для события ServerPollingRequested.
// Инициирует ручной опрос конкретного сервера.
type ServerPollingRequestedPayload struct {
	// ServerUUID — внутренний UUID сервера для опроса.
	ServerUUID string
}

// ServiceDeskModificationPayload — полезная нагрузка для событий создания/обновления в ServiceDesk.
// Используется для асинхронной синхронизации данных с внешней системой Naumen.
type ServiceDeskModificationPayload struct {
	// TaskID — идентификатор задачи для отслеживания статуса.
	TaskID uint `json:"task_id"`

	// EntityType — тип сущности для создания/обновления.
	// Значения: "Company", "Server", "Workstation", "Fiscal".
	EntityType string `json:"entity_type"`

	// EntityUUID — внутренний UUID сущности (пусто для создания новой).
	EntityUUID string `json:"entity_uuid,omitempty"`

	// TriggeredByUserID — UUID пользователя, инициировавшего операцию.
	TriggeredByUserID string `json:"triggered_by_user_id"`

	// PayloadForSD — данные для отправки в ServiceDesk в формате ключ-значение.
	// Формат соответствует API Naumen.
	PayloadForSD map[string]interface{} `json:"payload_for_sd"`
}

// DiscrepancyDetail описывает расхождение по одному полю между Etalon и ServiceDesk.
// Используется для выявления рассинхронизации данных.
type DiscrepancyDetail struct {
	// EtalonValue — значение поля в локальной БД Etalon.
	EtalonValue interface{} `json:"etalon_value"`

	// ServiceDeskValue — значение поля во внешней системе ServiceDesk (Naumen).
	ServiceDeskValue interface{} `json:"service_desk_value"`
}

// FiscalRegisterDiscrepancyPayload — полезная нагрузка для события FiscalRegisterDiscrepancyFound.
// Содержит все обнаруженные расхождения по фискальному регистратору.
type FiscalRegisterDiscrepancyPayload struct {
	// FRInternalUUID — внутренний UUID фискального регистратора в Etalon.
	FRInternalUUID string `json:"fr_internal_uuid"`

	// FRServiceDeskUUID — UUID фискального регистратора в ServiceDesk (Naumen).
	FRServiceDeskUUID string `json:"fr_service_desk_uuid"`

	// Discrepancies — карта расхождений по полям.
	// Ключ: имя поля (например, "serial_number", "fiscal_memory_number").
	// Значение: детали расхождения (значения в Etalon и ServiceDesk).
	Discrepancies map[string]DiscrepancyDetail `json:"discrepancies"`
}

// AgentDataPayload — полезная нагрузка для события AgentDataReceived.
// Содержит "сырые" данные, полученные от агента до обработки.
type AgentDataPayload struct {
	// TraceID — сквозной идентификатор трассировки
	TraceID string

	// Source — источник данных.
	// Для FTP: имя JSON-файла (например, "550e8400-e29b-41d4-a716-446655440000.json").
	// Для API: UUID агента.
	Source string

	// Data — данные агента, распарсенные из JSON.
	// Структура определяется агентом (sssruner).
	Data api.AgentDataDTO
}

// AgentObservationPayload — полезная нагрузка для события AgentObservationRequested.
// Содержит данные наблюдения, готовые к применению в доменной модели.
type AgentObservationPayload struct {
	// TraceID — сквозной идентификатор трассировки
	TraceID string

	// Source — источник данных для логирования.
	// Для API: UUID агента.
	// Для FTP: имя файла.
	Source string

	// Data — данные наблюдения агента.
	// Проходят предварительную валидацию в Orchestrator.
	Data api.AgentDataDTO
}
