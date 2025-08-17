package api

import "time"

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

// SyncRequestDTO тело запроса для запуска синхронизации.
type SyncRequestDTO struct {
	Full bool `json:"full"`
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
