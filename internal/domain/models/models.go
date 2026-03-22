// internal/models/models.go
// Пакет models содержит основные доменные модели данных, используемые в системе Etalon-Server.
// Модели представляют структуры для хранения в БД и обмена данными между слоями приложения.
package models

import (
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"

	"gorm.io/datatypes"
)

// ExternalSystemLink хранит связь между внутренней сущностью Etalon и её представлением во внешней системе.
// Используется для синхронизации данных с ServiceDesk (Naumen), Zabbix и другими интеграциями.
//
// Пример использования:
//
//	link := ExternalSystemLink{
//	    InternalID:      "550e8400-e29b-41d4-a716-446655440000",
//	    SystemName:      "servicedesk",
//	    ServiceDeskUUID: "SD-12345",
//	    EntityType:      "Company",
//	}
type ExternalSystemLink struct {
	// InternalID — UUID внутренней сущности (из поля Base.ID).
	// Составной первичный ключ вместе с SystemName.
	InternalID string `gorm:"primaryKey;type:text"`

	// SystemName — название внешней системы.
	// Допустимые значения: "servicedesk", "zabbix", "iiko" и др.
	// Составной первичный ключ вместе с InternalID.
	SystemName string `gorm:"primaryKey;type:varchar(50)"`

	// ServiceDeskUUID — идентификатор сущности во внешней системе ServiceDesk (Naumen).
	// Уникальное значение, используется для поиска при синхронизации.
	ServiceDeskUUID string `gorm:"type:text;unique;not null"`

	// EntityType — тип внутренней сущности для полиморфной связи.
	// Допустимые значения: "Company", "Server", "Workstation", "Fiscal" и др.
	EntityType string `gorm:"type:varchar(50);not null"`

	// LastSyncedAt — время последней успешной синхронизации с внешней системой.
	LastSyncedAt time.Time
}

// Константы статусов агента.
// Определяют жизненный цикл агента от обнаружения до активной работы.
const (
	// StatusPendingRegistration — агент прислал bootstrap-регистрацию, но ещё не подтверждён оператором.
	// До подтверждения оператором сервер не должен выдавать ему токены.
	StatusPendingRegistration = "pending_registration"

	// StatusPendingOwner — агент обнаружен, ожидает привязки к владельцу (компании).
	// Начальный статус для новых агентов, полученных от FTP-гейтая.
	StatusPendingOwner = "pending_owner"

	// StatusPendingZabbix — агент привязан к владельцу, ожидает регистрации в Zabbix.
	// После успешной регистрации переходит в StatusActive.
	StatusPendingZabbix = "pending_zabbix_registration"

	// StatusActive — агент активно работает, регулярно отправляет данные.
	// Нормальный рабочий статус.
	StatusActive = "active"

	// StatusRegistrationFailed — ошибка при регистрации агента.
	// Требует ручного вмешательства или повторной попытки.
	StatusRegistrationFailed = "registration_failed"
)

// CompanyContract представляет связь "многие-ко-многим" между компаниями и договорами.
// Одна компания может иметь несколько договоров, один договор может охватывать несколько компаний.
type CompanyContract struct {
	// CompanyID — UUID компании (внешний ключ).
	CompanyID string `gorm:"primaryKey"`

	// Company — связанная компания.
	Company company.Company `gorm:"foreignKey:CompanyID"`

	// ContractID — UUID договора (внешний ключ).
	ContractID string `gorm:"primaryKey"`

	// Contract — связанный договор.
	Contract contract.Contract `gorm:"foreignKey:ContractID"`
}

// EquipmentStatusLog хранит историю изменений статусов оборудования.
// Используется для аудита и отслеживания жизненного цикла серверов, рабочих станций и ФР.
// Записи создаются автоматически при изменении HealthStatus через репозитории.
type EquipmentStatusLog struct {
	// ID — автоинкрементный первичный ключ.
	ID uint `gorm:"primarykey"`

	// EntityType — тип оборудования: "Server", "Workstation", "Fiscal".
	EntityType string `gorm:"type:varchar(50);index"`

	// EntityID — UUID оборудования.
	EntityID string `gorm:"type:text;index"`

	// OldHealthStatus — предыдущий статус здоровья.
	// Значения: "healthy", "warning", "critical", "unknown".
	OldHealthStatus string `gorm:"type:varchar(50)"`

	// NewHealthStatus — новый статус здоровья.
	NewHealthStatus string `gorm:"type:varchar(50)"`

	// Details — дополнительные данные об изменении в формате JSON.
	// Может содержать причину изменения, источник данных и т.д.
	Details datatypes.JSON `gorm:"type:jsonb"`

	// ChangedBy — инициатор изменения: "system", "agent", "manual" или UUID пользователя.
	ChangedBy string `gorm:"type:varchar(50)"`

	// Timestamp — время изменения статуса.
	Timestamp time.Time `gorm:"index"`
}

// Agent представляет запись об агенте оборудования в общей модели ServiceDesk.
// В системе сосуществуют legacy-агенты с file-based доставкой и новый active-agent контур `sssruner`.
// Для нового контура сюда сохраняются bootstrap-регистрация, heartbeat и диагностические snapshot-ы.
type Agent struct {
	// UUID — уникальный идентификатор агента.
	// Для active-agent генерируется при первом запуске и сохраняется локально у агента.
	UUID string `gorm:"primaryKey;type:text"`

	// Type — тип агента.
	// Наполнение зависит от контура поставки данных и версии агента.
	Type string `gorm:"type:varchar(50);not null"`

	// OwnerID — UUID компании-владельца оборудования.
	// Устанавливается при привязке агента через AcceptancePage.
	OwnerID string `gorm:"type:text;index"`

	// WorkstationID — UUID рабочей станции (опционально).
	// Заполняется после создания/привязки оборудования в БД.
	WorkstationID *string `gorm:"type:text;index"`

	// LastObservedAt — время последнего получения данных от агента.
	// Обновляется при обработке payload агента, включая heartbeat нового active-agent.
	LastObservedAt *time.Time

	// Config — конфигурация агента в формате JSON.
	// Содержит настройки, передаваемые агенту (частота сбора данных, endpoint'ы и т.д.).
	Config datatypes.JSON `gorm:"type:jsonb"`

	// LastHeartbeat — время последнего "сердцебиения" от агента.
	// Используется для мониторинга активности агента.
	LastHeartbeat time.Time

	// LastRegistrationAt — время последней bootstrap-регистрации агента.
	// Обновляется как при успешной регистрации, так и при диагностируемых отказах.
	LastRegistrationAt *time.Time

	// LastRegistrationStatus — итог последней попытки регистрации.
	// Значения: success, pending_approval, unauthorized, invalid_request, failed.
	LastRegistrationStatus string `gorm:"type:varchar(32);index"`

	// LastRegistrationError — текст последней ошибки регистрации.
	// Пустой при успешной регистрации.
	LastRegistrationError string `gorm:"type:text"`

	// MachineFingerprint — последний fingerprint машины, присланный агентом при регистрации.
	MachineFingerprint string `gorm:"type:text"`

	// RegistrationSystemInfo — последний system_info из registration payload.
	RegistrationSystemInfo datatypes.JSON `gorm:"type:jsonb"`

	// RegistrationPayload — последнее полное тело registration request.
	RegistrationPayload datatypes.JSON `gorm:"type:jsonb"`

	// RegistrationApprovedAt — время подтверждения регистрации оператором.
	// Пока значение пустое, сервер не должен выдавать токены bootstrap-регистрации.
	RegistrationApprovedAt *time.Time

	// RegistrationApprovedBy — идентификатор пользователя, подтвердившего регистрацию.
	RegistrationApprovedBy string `gorm:"type:text"`

	// LatestInventorySnapshot — последний inventory snapshot из heartbeat.
	LatestInventorySnapshot datatypes.JSON `gorm:"type:jsonb"`

	// LatestAdapterStatuses — последний срез adapter_statuses из heartbeat.
	LatestAdapterStatuses datatypes.JSON `gorm:"type:jsonb"`

	// LastMeaningfulHeartbeatAt — время последнего heartbeat, признанного значимым.
	// Обновляется только когда нормализованный fingerprint изменился
	// или когда после регистрации пришёл первый heartbeat.
	LastMeaningfulHeartbeatAt *time.Time

	// LastMeaningfulObservedAt — observed_at последнего значимого heartbeat.
	// Используется для диагностики и защиты от повторной публикации одинакового состояния.
	LastMeaningfulObservedAt *time.Time

	// LastMeaningfulHeartbeatFingerprint — fingerprint нормализованного состояния heartbeat.
	// Хранится на уровне Agent, чтобы чистые heartbeat без изменений не создавали observation.
	LastMeaningfulHeartbeatFingerprint string `gorm:"type:char(64);index"`

	// LastMeaningfulHeartbeatState — нормализованное состояние последнего значимого heartbeat.
	// Содержит только поля, влияющие на доменную обработку и рекомендации адаптеров.
	LastMeaningfulHeartbeatState datatypes.JSON `gorm:"type:jsonb"`

	// Version — версия агента (sssruner).
	// Используется для отслеживания обновлений.
	Version string `gorm:"type:varchar(50)"`

	// Hostname — сетевое имя оборудования.
	// Берётся из данных агента, используется для идентификации.
	Hostname string `gorm:"type:text"`

	// ZabbixHostname — имя хоста в системе мониторинга Zabbix.
	// Устанавливается при регистрации в Zabbix.
	ZabbixHostname string `gorm:"type:text"`

	// Status — текущий статус агента.
	// Значения: StatusPendingRegistration, StatusPendingOwner, StatusPendingZabbix, StatusActive, StatusRegistrationFailed.
	Status string `gorm:"type:varchar(50);index"`

	// CreatedAt — время создания записи (первое обнаружение агента).
	CreatedAt time.Time

	// UpdatedAt — время последнего обновления записи.
	UpdatedAt time.Time
}

// AgentSessionToken хранит серверные сессионные токены нового агента (sssruner).
// Старые пассивные агенты (submit_json) эту модель не используют.
type AgentSessionToken struct {
	ID uint `gorm:"primaryKey"`

	AgentUUID string `gorm:"type:text;index;not null"`
	TokenType string `gorm:"type:varchar(20);index;not null"` // access | refresh
	TokenHash string `gorm:"type:char(64);uniqueIndex;not null"`

	ExpiresAt  time.Time  `gorm:"index;not null"`
	LastUsedAt *time.Time `gorm:"index"`
	RevokedAt  *time.Time `gorm:"index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// AgentFile отслеживает состояние обработанных JSON-файлов от агентов.
// Используется FTP-гейтвеем для инкрементальной обработки — только изменённые файлы.
// Позволяет избежать повторной обработки одних и тех же данных.
type AgentFile struct {
	// FileName — имя файла на FTP-сервере (первичный ключ).
	// Формат: обычно UUID агента с расширением .json.
	FileName string `gorm:"primaryKey;type:text"`

	// LastProcessedModTime — время модификации файла при последней обработке.
	// Сравнивается с mtime файла на FTP для определения изменений.
	LastProcessedModTime time.Time `gorm:"not null"`

	// LastProcessedFileSize — размер файла при последней обработке.
	// Дополнительный критерий для определения изменений.
	LastProcessedFileSize int64 `gorm:"not null"`

	// PayloadHash — SHA256 хеш содержимого файла.
	// Используется для идемпотентности: если хеш не изменился, файл не обрабатывается.
	// Вычисляется от сырого содержимого JSON-файла.
	PayloadHash string `gorm:"type:char(64);index"`

	// LastSeenFRSerial — последний замеченный серийный номер фискального регистратора.
	// Используется для отслеживания связи "файл -> ФР".
	LastSeenFRSerial *string `gorm:"type:text;index"`

	// LastSeenRMSUrl — последний замеченный URL RMS (Iiko).
	// Используется для отслеживания связи "файл -> ресторан Iiko".
	LastSeenRMSUrl *string `gorm:"type:text"`

	// LastCheckedModTime — время последней проверки модификации файла на FTP через MDTM.
	// Используется для оптимизации синхронизации: позволяет проверить изменения без скачивания.
	// Если время на FTP совпадает с этим значением, файл не нужно скачивать.
	LastCheckedModTime *time.Time `gorm:"type:timestamp"`

	// CreatedAt — время первой обработки файла.
	CreatedAt time.Time

	// UpdatedAt — время последней обработки файла.
	UpdatedAt time.Time
}

// AgentCommand представляет техническую команду для удалённого выполнения агентом (sssruner).
// Не путать с ReconciliationTask — задачи для людей, а AgentCommand для программного агента.
//
// Порядок выполнения:
// 1. Команда создаётся со статусом "new"
// 2. Агент опрашивает сервер, получает команду, статус меняется на "sent"
// 3. После выполнения статус меняется на "completed" или "failed"
type AgentCommand struct {
	// ID — автоинкрементный первичный ключ.
	ID uint `gorm:"primaryKey"`

	// AgentUUID — UUID агента-получателя команды.
	AgentUUID string `gorm:"type:text;index;not null"`

	// Type — тип команды.
	// Допустимые значения:
	//   - "update_config": обновить конфигурацию агента
	//   - "inventory": выполнить инвентаризацию оборудования
	//   - "exec": выполнить произвольную команду
	Type string `gorm:"type:varchar(50);not null"`

	// Payload — аргументы команды в формате JSON.
	// Содержимое зависит от типа команды.
	Payload datatypes.JSON `gorm:"type:jsonb"`

	// Status — текущий статус выполнения команды.
	// Значения: "new" (новая), "sent" (отправлена), "completed" (выполнена), "failed" (ошибка).
	Status string `gorm:"type:varchar(20);default:'new';index"`

	// CreatedAt — время создания команды.
	CreatedAt time.Time

	// SentAt — время отправки команды агенту (статус "sent").
	SentAt *time.Time
}

// ReconciliationTask представляет задачу согласования данных для ручной обработки оператором.
// Создаётся автоматически при обнаружении расхождений между данными агента и существующими записями.
// Не путать с AgentCommand — задачи для людей, а AgentCommand для программного агента.
//
// Примеры задач:
//   - Новый агент без привязки к компании
//   - Обнаружен новый ФР, требующий регистрации
//   - Изменение критических параметров оборудования
type ReconciliationTask struct {
	// ID — автоинкрементный первичный ключ.
	ID uint `gorm:"primarykey"`

	// TaskType — тип задачи согласования.
	// Значения: "new_agent", "new_fiscal", "owner_change", "config_change" и др.
	TaskType string `gorm:"type:varchar(50);not null;index"`

	// EntityType — тип связанной сущности (опционально).
	// Значения: "Agent", "Server", "Workstation", "Fiscal".
	EntityType string `gorm:"type:varchar(50)"`

	// EntityUUID — UUID связанной сущности (опционально).
	EntityUUID string `gorm:"type:text"`

	// Details — детальная информация о задаче в формате JSON.
	// Содержит данные, необходимые оператору для принятия решения.
	Details datatypes.JSON `gorm:"type:jsonb"`

	// Status — текущий статус задачи.
	// Значения: "new" (новая), "in_progress" (в работе), "resolved" (решена), "cancelled" (отменена).
	Status string `gorm:"type:varchar(50);default:'new';index"`

	// Comment — комментарий оператора при разрешении задачи.
	Comment string `gorm:"type:text"`

	// CreatedAt — время создания задачи.
	CreatedAt time.Time

	// UpdatedAt — время последнего обновления задачи.
	UpdatedAt time.Time
}
