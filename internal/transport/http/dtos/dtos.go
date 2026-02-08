package dtos

import (
	"encoding/json"
	"time"
)

// --- DTO для CRUD, Задач и других операций ---

// CompanyCreateDTO - DTO для создания/обновления компании.
type CompanyCreateDTO struct {
	Title                 *string `json:"title"`
	Address               *string `json:"address"`
	AdditionalName        *string `json:"additional_name"`
	ParentServiceDeskUUID *string `json:"parent_uuid"`
}

// ResolveTaskRequestDTO - тело запроса для решения задачи.
type ResolveTaskRequestDTO struct {
	Status            string                 `json:"status"` // 'resolved', 'rejected', 'pending_sd_action' и т.д.
	Comment           string                 `json:"comment,omitempty"`
	ResolutionPayload map[string]interface{} `json:"resolution_payload,omitempty"` // Данные для выполнения действия
}

// DuplicateGroupDTO представляет группу дубликатов для ответа API.
type DuplicateGroupDTO struct {
	Field      string        `json:"field"`
	Value      string        `json:"value"`
	EntityType string        `json:"entity_type"`
	MainRecord interface{}   `json:"main_record"`
	Duplicates []interface{} `json:"duplicates"`
}

// ErrorResponseDTO стандартизированный ответ с ошибкой.
type ErrorResponseDTO struct {
	Error string `json:"error"`
}

// --- DTO для взаимодействия с Агентами ---

// LicenseInfo описывает одну лицензию из данных агента.
type LicenseInfo struct {
	Name      string `json:"name"`
	DateFrom  string `json:"dateFrom"`
	DateUntil string `json:"dateUntil"`
}

// LicensesField - это специальный тип для поля 'licenses',
// который может быть либо строкой (старый формат), либо картой (новый формат).
type LicensesField struct {
	Structured map[string]LicenseInfo
	Legacy     string
}

// UnmarshalJSON для кастомной обработки поля 'licenses'.
func (lf *LicensesField) UnmarshalJSON(data []byte) error {
	// Сначала пытаемся распарсить как структурированный объект (карту).
	var structured map[string]LicenseInfo
	if err := json.Unmarshal(data, &structured); err == nil {
		lf.Structured = structured
		return nil
	}

	// Если не получилось, пытаемся распарсить как простую строку.
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		lf.Legacy = legacy
		return nil
	}

	// Если не удалось распарсить ни как карту, ни как строку, возвращаем ошибку.
	// Это может произойти, если в JSON, например, число или null.
	return json.Unmarshal(data, &struct{}{}) // Возвращаем оригинальную ошибку парсинга
}

// MarshalJSON для кастомной сериализации поля 'licenses' обратно в простой JSON.
func (lf LicensesField) MarshalJSON() ([]byte, error) {
	if lf.Structured != nil {
		return json.Marshal(lf.Structured)
	}
	// Если Structured - nil, сериализуем Legacy (даже если это пустая строка).
	return json.Marshal(lf.Legacy)
}

// AgentDataDTO определяет структуру данных, получаемых от агента.
type AgentDataDTO struct {
	ModelName        string        `json:"modelName"`
	SerialNumber     string        `json:"serialNumber"`
	RNM              string        `json:"RNM"`
	INN              string        `json:"INN"`
	FNSerial         string        `json:"fn_serial"`
	DateTimeEnd      string        `json:"dateTime_end"`
	FFDVersion       string        `json:"ffdVersion"`
	FNExecution      string        `json:"fnExecution"`
	OrganizationName string        `json:"organizationName"`
	DateTimeReg      string        `json:"datetime_reg"`
	Hostname         string        `json:"hostname"`
	URLRms           string        `json:"url_rms"`
	CRMID            string        `json:"crmId"`
	TeamviewerID     string        `json:"teamviewer_id"`
	AnydeskID        string        `json:"anydesk_id"`
	LitemanagerID    string        `json:"litemanager_id"`
	CurrentTime      string        `json:"current_time"`
	AgentVersion     string        `json:"agent_version"`
	InstalledDriver  string        `json:"installed_driver,omitempty"`
	BootVersion      string        `json:"bootVersion,omitempty"`      // Версия прошивки (для FRFirmware)
	Licenses         LicensesField `json:"licenses,omitempty"`         // Используем кастомный тип
	Address          string        `json:"address,omitempty"`          // Адрес фискального регистратора
	AttributeExcise  *string       `json:"attribute_excise,omitempty"` // Признак работы с акцизными товарами (строковый)
	AttributeMarked  *string       `json:"attribute_marked,omitempty"` // Признак работы с маркированными товарами (строковый)
	OFDName          string        `json:"ofd_name,omitempty"`         // Название оператора фискальных данных

	// Временные поля для парсинга строковых значений
	AttributeExciseStr string `json:"attribute_excise_str,omitempty"`
	AttributeMarkedStr string `json:"attribute_marked_str,omitempty"`

	AgentUUID string `json:"uuid,omitempty"`       // UUID, присылаемый самим агентом (из конфига или аргументов)
	AgentType string `json:"agent_type,omitempty"` // Тип агента: "getad", "sssruner", "workstation"

	AdditionalProperties map[string]interface{} `json:"-"`
}

// AgentTaskDTO описывает задачу, которую агент должен выполнить (для sssruner).
type AgentTaskDTO struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`              // Тип задачи: "inventory", "update_config", "exec_script"
	Payload   json.RawMessage `json:"payload,omitempty"` // Аргументы задачи
	CreatedAt time.Time       `json:"created_at"`
}

// AgentHeartbeatResponseDTO — ответ сервера на передачу данных/пинг.
type AgentHeartbeatResponseDTO struct {
	Status string         `json:"status"`          // "ok", "accepted"
	Tasks  []AgentTaskDTO `json:"tasks,omitempty"` // Список задач для выполнения (если агент sssruner)
}

// TicketListDTO - DTO для списка заявок (для таблицы на UI).
type TicketListDTO struct {
	ID                string    `json:"id"`                // Внутренний ID
	Number            int       `json:"number"`            // Номер заявки
	ServiceDeskUUID   string    `json:"service_desk_uuid"` // Внешний UUID
	Status            string    `json:"status"`            // Статус
	Subject           string    `json:"subject"`           // Описание/Тема
	Description       string    `json:"description"`       // ?Описание (HTML/Markdown)
	LastComment       string    `json:"last_comment"`      // Последний комментарий
	LastCommentAuthor string    `json:"last_comment_author,omitempty"`
	LastActivityDate  time.Time `json:"last_activity"` // Дата последнего изменения
	CreatedAt         time.Time `json:"created_at"`    // Дата создания
	CompanyID         string    `json:"company_id"`
	CompanyName       string    `json:"company_name"`
	ContractID        *string   `json:"contract_id,omitempty"`
	IsCommonContract  bool      `json:"is_common_contract,omitempty"`
	Assignee          *struct {
		ID       uint   `json:"id"`
		FullName string `json:"fullName"`
	} `json:"assignee,omitempty"`
}

// --- DTO для операций с тикетами ---

// TicketAssignDTO - запрос на назначение исполнителя.
type TicketAssignDTO struct {
	AssigneeID *uint `json:"assignee_id"` // null, чтобы снять исполнителя
}

// TicketStatusChangeDTO - запрос на смену статуса.
type TicketStatusChangeDTO struct {
	Status  string `json:"status" validate:"required"`
	Comment string `json:"comment"` // Опциональный комментарий при смене статуса
}

// TicketCreateInternalDTO - создание тикета вручную (через API).
type TicketCreateInternalDTO struct {
	Subject     string  `json:"subject" validate:"required"`
	Description string  `json:"description"`
	Priority    string  `json:"priority"` // low, medium, high, critical
	Type        string  `json:"type"`     // incident, service_request
	CompanyID   string  `json:"company_id" validate:"required"`
	AssetID     *string `json:"asset_id"`
	AssetType   *string `json:"asset_type"`
}

// UnmarshalJSON для кастомной обработки JSON, чтобы собирать все неописанные поля.
func (a *AgentDataDTO) UnmarshalJSON(data []byte) error {
	// Сначала парсим JSON в map для доступа к сырым значениям
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Обрабатываем поля attribute_excise и attribute_marked отдельно
	// Они могут приходить как строки или как boolean значения

	// Обрабатываем attribute_excise
	if val, exists := raw["attribute_excise"]; exists && val != nil {
		if strVal, ok := val.(string); ok {
			// Пришло как строка, сохраняем как строку
			a.AttributeExcise = &strVal
		} else if boolVal, ok := val.(bool); ok {
			// Пришло как boolean, преобразуем в строку
			strVal := "false"
			if boolVal {
				strVal = "true"
			}
			a.AttributeExcise = &strVal
		}
	}

	// Обрабатываем attribute_marked
	if val, exists := raw["attribute_marked"]; exists && val != nil {
		if strVal, ok := val.(string); ok {
			// Пришло как строка, сохраняем как строку
			a.AttributeMarked = &strVal
		} else if boolVal, ok := val.(bool); ok {
			// Пришло как boolean, преобразуем в строку
			strVal := "false"
			if boolVal {
				strVal = "true"
			}
			a.AttributeMarked = &strVal
		}
	}

	// Удаляем обработанные поля из raw мапы
	delete(raw, "attribute_excise")
	delete(raw, "attribute_marked")

	// Создаем временную структуру без проблемных полей для парсинга остальных данных
	type Alias AgentDataDTO
	alias := &struct{ *Alias }{Alias: (*Alias)(a)}

	// Подготавливаем данные без проблемных полей
	tempData, err := json.Marshal(raw)
	if err != nil {
		return err
	}

	// Парсим остальные поля
	if err := json.Unmarshal(tempData, alias); err != nil {
		return err
	}

	// Удаляем из мапы те поля, которые уже были распарсены в основные поля структуры.
	delete(raw, "modelName")
	delete(raw, "serialNumber")
	delete(raw, "RNM")
	delete(raw, "INN")
	delete(raw, "fn_serial")
	delete(raw, "dateTime_end")
	delete(raw, "ffdVersion")
	delete(raw, "fnExecution")
	delete(raw, "organizationName")
	delete(raw, "datetime_reg")
	delete(raw, "hostname")
	delete(raw, "url_rms")
	delete(raw, "crmId")
	delete(raw, "teamviewer_id")
	delete(raw, "anydesk_id")
	delete(raw, "litemanager_id")
	delete(raw, "current_time")
	delete(raw, "agent_version")
	delete(raw, "installed_driver")
	delete(raw, "bootVersion")
	delete(raw, "licenses")
	delete(raw, "address")
	delete(raw, "ofd_name")
	delete(raw, "attribute_excise_str")
	delete(raw, "attribute_marked_str")

	a.AdditionalProperties = raw
	return nil
}

// RegistrationRequestDTO - тело запроса для регистрации нового агента.
type RegistrationRequestDTO struct {
	AgentUUID    string       `json:"agent_uuid"`
	Hostname     string       `json:"hostname"`
	AgentVersion string       `json:"agent_version"`
	InitialData  AgentDataDTO `json:"initial_data"`
}

// AgentConfigDTO - структура конфигурации, отправляемая агенту.
type AgentConfigDTO struct {
	EtalonServerURL string            `json:"etalon_server_url"`
	Mode            string            `json:"mode"`
	Intervals       IntervalsDTO      `json:"intervals"`
	Zabbix          ZabbixConfigDTO   `json:"zabbix"`
	Workstation     WorkstationCfgDTO `json:"workstation,omitempty"`
}

type IntervalsDTO struct {
	Heartbeat        int `json:"heartbeat_seconds"`
	ConfigCheck      int `json:"config_check_seconds"`
	WorkstationCheck int `json:"workstation_check_seconds"`
	UpdateCheck      int `json:"update_check_seconds"`
}

type ZabbixConfigDTO struct {
	ServerHost string `json:"server_host"`
	ServerPort int    `json:"server_port"`
	Hostname   string `json:"hostname"`
}

type WorkstationCfgDTO struct {
	PrimaryJSONPath   string `json:"primary_json_path"`
	CashServerLogPath string `json:"cash_server_log_path"`
}

// --- DTO для аутентификации и пользователей ---

// LoginRequestDTO - тело запроса для входа в систему.
type LoginRequestDTO struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// UserDTO - DTO для отображения информации о пользователе.
type UserDTO struct {
	ID       uint     `json:"id"`
	Username string   `json:"username"`
	FullName string   `json:"fullName"`
	Roles    []string `json:"roles"`
}

// LoginResponseDTO - тело ответа при успешном входе.
type LoginResponseDTO struct {
	AccessToken string  `json:"access_token"`
	User        UserDTO `json:"user"`
}

// UserCreateDTO - DTO для создания нового пользователя.
type UserCreateDTO struct {
	Username string   `json:"username" validate:"required"`
	Password string   `json:"password" validate:"required,min=6"`
	FullName string   `json:"fullName" validate:"required"`
	Roles    []string `json:"roles" validate:"required"`
}

// UserUpdateDTO - DTO для обновления пользователя.
type UserUpdateDTO struct {
	Password *string  `json:"password,omitempty" validate:"omitempty,min=6"`
	FullName *string  `json:"fullName,omitempty"`
	Roles    []string `json:"roles,omitempty"`
}

// --- DTO для UI-ориентированного поиска ---

// FinalSearchResponseDTO - корневой объект для нового ответа поиска.
type FinalSearchResponseDTO struct {
	SearchResults []SearchGroupDTO `json:"search_results"`
}

// SearchGroupDTO представляет одну группу в результатах поиска (сущности, сгруппированные по владельцу).
type SearchGroupDTO struct {
	Owner         OwnerFullDTO     `json:"owner"`
	FoundEntities []FoundEntityDTO `json:"found_entities"`
}

// OwnerFullDTO содержит расширенную информацию о компании-владельце.
type OwnerFullDTO struct {
	UUID            string      `json:"uuid"`
	ServiceDeskUUID *string     `json:"external_uuid,omitempty"`
	Name            string      `json:"name"`
	Address         *string     `json:"address,omitempty"`
	ActiveContract  *bool       `json:"active_contract,omitempty"`
	AdditionalInfo  *string     `json:"additional_info,omitempty"`
	ParentInfo      *ParentInfo `json:"parent_info,omitempty"`
}

// ParentInfo содержит краткую информацию о родительской компании.
type ParentInfo struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// FoundEntityDTO представляет одну найденную сущность внутри группы.
type FoundEntityDTO struct {
	EntityType string      `json:"entity_type"`
	Data       interface{} `json:"data"`
}

// --- DTO для данных внутри FoundEntityDTO ---

// ServerRichDTO содержит полный набор полей Сервера для UI.
type ServerRichDTO struct {
	UUID              string      `json:"uuid"`
	ServiceDeskUUID   *string     `json:"external_uuid,omitempty"`
	DeviceName        *string     `json:"device_name,omitempty"`
	IP                *string     `json:"ip,omitempty"`
	OperationalStatus string      `json:"operational_status,omitempty"`
	HealthStatus      string      `json:"health_status,omitempty"`
	StatusDetails     interface{} `json:"status_details,omitempty"`
	Anydesk           *string     `json:"anydesk,omitempty"`
	Teamviewer        *string     `json:"teamviewer,omitempty"`
	RDP               *string     `json:"rdp,omitempty"`
	Litemanager       *string     `json:"litemanager,omitempty"`
	UniqueID          *string     `json:"unique_id,omitempty"`
	CRMid             *string     `json:"crm_id,omitempty"`
	PartnersLink      *string     `json:"partners_link,omitempty"`
	ServerName        *string     `json:"server_name,omitempty"`
	ServerVersion     *string     `json:"server_version,omitempty"`
	ServerEdition     *string     `json:"server_edition,omitempty"`
	LastPolledAt      *time.Time  `json:"last_polled_at,omitempty"`
}

// WorkstationRichDTO содержит полный набор полей Рабочей станции для UI.
type WorkstationRichDTO struct {
	UUID            string      `json:"uuid"`
	ServiceDeskUUID *string     `json:"external_uuid,omitempty"`
	DeviceName      *string     `json:"device_name,omitempty"`
	HealthStatus    string      `json:"health_status,omitempty"`
	StatusDetails   interface{} `json:"status_details,omitempty"`
	Anydesk         *string     `json:"anydesk,omitempty"`
	Teamviewer      *string     `json:"teamviewer,omitempty"`
	Litemanager     *string     `json:"litemanager,omitempty"`
}

// FiscalRegisterRichDTO содержит полный набор полей Фискального регистратора для UI.
type FiscalRegisterRichDTO struct {
	UUID               string      `json:"uuid"`
	ServiceDeskUUID    *string     `json:"external_uuid,omitempty"`
	HealthStatus       string      `json:"health_status,omitempty"`
	StatusDetails      interface{} `json:"status_details,omitempty"`
	RNKKT              *string     `json:"rn_kkt,omitempty"`
	ModelKKT           *string     `json:"model_kkt,omitempty"`
	SerialNumber       *string     `json:"serial_number,omitempty"`
	FNNumber           *string     `json:"fn_number,omitempty"`
	FNRegistrationDate *time.Time  `json:"fn_registration_date,omitempty"`
	FNExpireDate       *time.Time  `json:"fn_expire_date,omitempty"`
	DriverVersion      *string     `json:"driver_version,omitempty"`
	FRFirmware         *string     `json:"fr_firmware,omitempty"`
	FRDownloader       *string     `json:"fr_downloader,omitempty"`
	OrganizationName   *string     `json:"organization_name,omitempty"`
	INN                *string     `json:"inn,omitempty"`
}

// TaskDTO - DTO для отображения задачи в UI.
type TaskDTO struct {
	ID         uint        `json:"id"`
	TaskType   string      `json:"task_type"`
	EntityType string      `json:"entity_type"`
	EntityUUID string      `json:"entity_uuid"` // Может быть внутренним ID или другим идентификатором (СН)
	Details    interface{} `json:"details"`
	Status     string      `json:"status"`
	Comment    string      `json:"comment"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// CreateEntityInSDRequestDTO - тело запроса для создания сущности в ServiceDesk по задаче.
type CreateEntityInSDRequestDTO struct {
	EntityType string `json:"entity_type" validate:"required"` // 'FiscalRegister', 'Workstation', 'Server'
}

// AcceptedResponseDTO - стандартный ответ для асинхронных операций.
type AcceptedResponseDTO struct {
	Message string `json:"message"`
	TaskID  uint   `json:"task_id"`
}

// === DTO для создания сущностей ===

// ServerCreateDTO - DTO для создания сервера
type ServerCreateDTO struct {
	UniqueID      *string `json:"unique_id"`
	CRMid         *string `json:"crm_id"`
	Teamviewer    *string `json:"teamviewer"`
	RDP           *string `json:"rdp"`
	Anydesk       *string `json:"anydesk"`
	IP            *string `json:"ip"`
	DeviceName    *string `json:"device_name"`
	ServerName    *string `json:"server_name"`
	ServerVersion *string `json:"server_version"`
	Description   *string `json:"description"`
	OwnerID       *string `json:"owner_id"`
}

// WorkstationCreateDTO - DTO для создания рабочей станции
type WorkstationCreateDTO struct {
	Teamviewer  *string `json:"teamviewer"`
	Anydesk     *string `json:"anydesk"`
	Litemanager *string `json:"litemanager"`
	DeviceName  *string `json:"device_name"`
	Description *string `json:"description"`
	OwnerID     *string `json:"owner_id"`
}

// FiscalRegisterCreateDTO - DTO для создания фискального регистратора
type FiscalRegisterCreateDTO struct {
	ModelKKT       *string `json:"model_kkt"`
	RNKKT          *string `json:"rn_kkt"`
	INN            *string `json:"inn"`
	FRSerialNumber *string `json:"fr_serial_number"`
	FNNumber       *string `json:"fn_number"`
	FRDownloader   *string `json:"fr_downloader"`
	FRFirmware     *string `json:"fr_firmware"`
	DriverVersion  *string `json:"driver_version"`
	OwnerID        *string `json:"owner_id"`
}

// ContractCreateDTO - DTO для создания контракта
type ContractCreateDTO struct {
	State          *string                `json:"state"`
	StateStartTime *time.Time             `json:"state_start_time"`
	Services       map[string]interface{} `json:"services"`
	Recipients     map[string]interface{} `json:"recipients"`
	ServiceLevel   int                    `json:"service_level"`
	CompanyIDs     []string               `json:"company_ids"`
}

// === DTO для пагинации ===

// PaginationParams - параметры пагинации для запросов
type PaginationParams struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// PaginatedResponse - универсальный пагинированный ответ (используется для разделения Data/Meta).
type PaginatedResponse struct {
	Data    interface{} `json:"data"`
	Total   int64       `json:"total"`
	Limit   int         `json:"limit"`
	Offset  int         `json:"offset"`
	HasNext bool        `json:"has_next"`
	HasPrev bool        `json:"has_prev"`
}

// FilterParams - параметры фильтрации
type FilterParams struct {
	Status     *string `json:"status,omitempty"`
	OwnerID    *string `json:"owner_id,omitempty"`
	Search     *string `json:"search,omitempty"`
	DateFrom   *string `json:"date_from,omitempty"`
	DateTo     *string `json:"date_to,omitempty"`
	EntityType *string `json:"entity_type,omitempty"`
}

// SortParams - параметры сортировки
type SortParams struct {
	Field     string `json:"field,omitempty"`
	Direction string `json:"direction,omitempty"` // "asc" или "desc"
}
