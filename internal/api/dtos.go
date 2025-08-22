package api

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
	Status  string `json:"status"`
	Comment string `json:"comment,omitempty"`
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

// AgentDataDTO определяет структуру данных, получаемых от агента.
type AgentDataDTO struct {
	ModelName       string `json:"modelName"`
	SerialNumber    string `json:"serialNumber"`
	RNM             string `json:"RNM"`
	INN             string `json:"INN"`
	FNSerial        string `json:"fn_serial"`
	DateTimeEnd     string `json:"dateTime_end"`
	FFDVersion      string `json:"ffdVersion"`
	Hostname        string `json:"hostname"`
	URLRms          string `json:"url_rms"`
	CRMID           string `json:"crmId"`
	TeamviewerID    string `json:"teamviewer_id"`
	AnydeskID       string `json:"anydesk_id"`
	LitemanagerID   string `json:"litemanager_id"`
	CurrentTime     string `json:"current_time"`
	AgentVersion    string `json:"agent_version"`
	InstalledDriver string `json:"installed_driver,omitempty"`

	AdditionalProperties map[string]interface{} `json:"-"`
}

// UnmarshalJSON для кастомной обработки JSON, чтобы собирать все неописанные поля.
func (a *AgentDataDTO) UnmarshalJSON(data []byte) error {
	type Alias AgentDataDTO
	alias := &struct{ *Alias }{Alias: (*Alias)(a)}
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
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
	delete(raw, "hostname")
	delete(raw, "url_rms")
	delete(raw, "crmId")
	delete(raw, "teamviewer_id")
	delete(raw, "anydesk_id")
	delete(raw, "litemanager_id")
	delete(raw, "current_time")
	delete(raw, "agent_version")
	delete(raw, "installed_driver")

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

// --- НОВЫЕ DTO для UI-ориентированного поиска ---

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
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	Address        *string `json:"address,omitempty"`
	ActiveContract *bool   `json:"active_contract,omitempty"`
	AdditionalInfo *string `json:"additional_info,omitempty"`
}

// FoundEntityDTO представляет одну найденную сущность внутри группы.
type FoundEntityDTO struct {
	EntityType string      `json:"entity_type"`
	Data       interface{} `json:"data"`
}

// --- DTO для данных внутри FoundEntityDTO ---

// ServerRichDTO содержит полный набор полей Сервера для UI.
type ServerRichDTO struct {
	UUID        string  `json:"uuid"`
	DeviceName  *string `json:"device_name,omitempty"`
	IP          *string `json:"ip,omitempty"`
	Status      *string `json:"status,omitempty"`
	Anydesk     *string `json:"anydesk,omitempty"`
	Teamviewer  *string `json:"teamviewer,omitempty"`
	RDP         *string `json:"rdp,omitempty"`
	Litemanager *string `json:"litemanager,omitempty"`
}

// WorkstationRichDTO содержит полный набор полей Рабочей станции для UI.
type WorkstationRichDTO struct {
	UUID        string  `json:"uuid"`
	DeviceName  *string `json:"device_name,omitempty"`
	Status      *string `json:"status,omitempty"`
	Anydesk     *string `json:"anydesk,omitempty"`
	Teamviewer  *string `json:"teamviewer,omitempty"`
	Litemanager *string `json:"litemanager,omitempty"`
}

// FiscalRegisterRichDTO содержит полный набор полей Фискального регистратора для UI.
type FiscalRegisterRichDTO struct {
	UUID               string     `json:"uuid"`
	RNKKT              *string    `json:"rn_kkt,omitempty"`
	ModelKKT           *string    `json:"model_kkt,omitempty"`
	FNExpireDate       *time.Time `json:"fn_expire_date,omitempty"`
	FNRegistrationDate *time.Time `json:"fn_registration_date,omitempty"`
	DriverVersion      *string    `json:"driver_version,omitempty"`
	FirmwareVersion    *string    `json:"firmware_version,omitempty"`
}
