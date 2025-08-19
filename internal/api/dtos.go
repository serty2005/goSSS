package api

import (
	"encoding/json"
	"time"
)

// SearchResultDTO содержит результаты глобального поиска.
type SearchResultDTO struct {
	Companies       []CompanySearchResultDTO        `json:"companies"`
	Servers         []ServerSearchResultDTO         `json:"servers"`
	Workstations    []WorkstationSearchResultDTO    `json:"workstations"`
	FiscalRegisters []FiscalRegisterSearchResultDTO `json:"fiscal_registers"`
}

// CompanyCreateDTO - DTO для создания/обновления компании.
type CompanyCreateDTO struct {
	Title                 *string `json:"title"`
	Address               *string `json:"address"`
	AdditionalName        *string `json:"additional_name"`
	ParentServiceDeskUUID *string `json:"parent_uuid"`
}

type CompanySearchResultDTO struct {
	ServiceDeskUUID string  `json:"uuid"`
	Title           *string `json:"title,omitempty"`
	Address         *string `json:"address,omitempty"`
	AdditionalName  *string `json:"additional_name,omitempty"`
	ActiveContract  *bool   `json:"active_contract,omitempty"`
}

type ServerSearchResultDTO struct {
	ServiceDeskUUID      string  `json:"uuid"`
	DeviceName           *string `json:"device_name,omitempty"`
	IP                   *string `json:"ip,omitempty"`
	UniqueID             *string `json:"unique_id,omitempty"`
	Teamviewer           *string `json:"teamviewer,omitempty"`
	RDP                  *string `json:"rdp,omitempty"`
	Anydesk              *string `json:"anydesk,omitempty"`
	Litemanager          *string `json:"litemanager,omitempty"`
	OwnerServiceDeskUUID *string `json:"owner_id,omitempty"`
}

type WorkstationSearchResultDTO struct {
	ServiceDeskUUID      string  `json:"uuid"`
	DeviceName           *string `json:"device_name,omitempty"`
	Teamviewer           *string `json:"teamviewer,omitempty"`
	Anydesk              *string `json:"anydesk,omitempty"`
	Litemanager          *string `json:"litemanager,omitempty"`
	Description          *string `json:"description,omitempty"`
	OwnerServiceDeskUUID *string `json:"owner_id,omitempty"`
}

type FiscalRegisterSearchResultDTO struct {
	ServiceDeskUUID      string     `json:"uuid"`
	RNKKT                *string    `json:"rn_kkt,omitempty"`
	ModelKKT             *string    `json:"model_kkt,omitempty"`
	FNExpireDate         *time.Time `json:"fn_expire_date,omitempty"`
	FRSerialNumber       *string    `json:"fr_serial_number,omitempty"`
	FNNumber             *string    `json:"fn_number,omitempty"`
	LegalName            *string    `json:"legal_name,omitempty"`
	OwnerServiceDeskUUID *string    `json:"owner_id,omitempty"`
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

type AgentDataDTO struct {
	ModelName     string `json:"modelName"`
	SerialNumber  string `json:"serialNumber"`
	RNM           string `json:"RNM"`
	FNSerial      string `json:"fn_serial"`
	DateTimeEnd   string `json:"dateTime_end"`
	FFDVersion    string `json:"ffdVersion"`
	Hostname      string `json:"hostname"`
	URLRms        string `json:"url_rms"`
	CRMID         string `json:"crmId"`
	TeamviewerID  string `json:"teamviewer_id"`
	AnydeskID     string `json:"anydesk_id"`
	LitemanagerID string `json:"litemanager_id"`
	CurrentTime   string `json:"current_time"`
	AgentVersion  string `json:"agent_version"` // Версия самого агента

	// ИЗМЕНЕНИЕ: Добавляем поле для всех остальных "нестрогих" данных
	AdditionalProperties map[string]interface{} `json:"-"`
}

// UnmarshalJSON для кастомной обработки JSON
func (a *AgentDataDTO) UnmarshalJSON(data []byte) error {
	// Сначала десериализуем все поля в анонимную структуру,
	// чтобы избежать рекурсии вызова UnmarshalJSON.
	type Alias AgentDataDTO
	alias := &struct{ *Alias }{Alias: (*Alias)(a)}
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	// Теперь десериализуем все данные в мапу, чтобы получить дополнительные поля.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	// Удаляем из мапы те поля, которые уже были распарсены в основные поля структуры.
	delete(raw, "modelName")
	delete(raw, "serialNumber")
	delete(raw, "RNM")
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

	// Все, что осталось, - это дополнительные свойства.
	a.AdditionalProperties = raw

	return nil
}

// RegistrationRequestDTO - тело запроса для регистрации нового агента.
type RegistrationRequestDTO struct {
	// Уникальный ID, сгенерированный агентом.
	AgentUUID string `json:"agent_uuid"`
	// Имя хоста операционной системы.
	Hostname string `json:"hostname"`
	// Версия агента.
	AgentVersion string `json:"agent_version"`
	// Данные, собранные при первом запуске.
	InitialData AgentDataDTO `json:"initial_data"`
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
