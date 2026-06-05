package dtos

import "time"

type TelephonyBindPendingContextDTO struct {
	TicketID    string `json:"ticket_id" validate:"required"`
	ContactName string `json:"contact_name"`
}

type TelephonyBindCallDTO struct {
	TicketID    string `json:"ticket_id" validate:"required"`
	ContactName string `json:"contact_name"`
}

type TelephonySetTicketContactDTO struct {
	ContactType     string `json:"contact_type"`
	Phone           string `json:"phone"`
	Telegram        string `json:"telegram"`
	ContactName     string `json:"contact_name"`
	TicketContactID uint   `json:"ticket_contact_id"`
	IsPrimary       bool   `json:"is_primary"`
	Clear           bool   `json:"clear"`
}

type TelephonyContactDTO struct {
	ID              uint    `json:"id"`
	PhoneNormalized string  `json:"phone_normalized"`
	PhoneDisplay    string  `json:"phone_display"`
	Name            *string `json:"name,omitempty"`
	BitrixContactID *string `json:"bitrix_contact_id,omitempty"`
}

type TicketContactDTO struct {
	ID                 uint                 `json:"id"`
	ContactType        string               `json:"contact_type"`
	TelephonyContactID *uint                `json:"telephony_contact_id,omitempty"`
	Value              string               `json:"value"`
	DisplayValue       string               `json:"display_value"`
	Name               string               `json:"name"`
	IsPrimary          bool                 `json:"is_primary"`
	PrimaryMode        string               `json:"primary_mode"`
	Source             string               `json:"source"`
	TelephonyContact   *TelephonyContactDTO `json:"telephony_contact,omitempty"`
}

type TelephonyCallDTO struct {
	ID              string               `json:"id"`
	ExternalCallID  string               `json:"external_call_id"`
	Direction       string               `json:"direction"`
	Status          string               `json:"status"`
	MissedStatus    *string              `json:"missed_status,omitempty"`
	ClientPhone     *string              `json:"client_phone,omitempty"`
	VATNumber       *string              `json:"vat_number,omitempty"`
	EmployeeLogin   *string              `json:"employee_login,omitempty"`
	EmployeeUserID  *uint                `json:"employee_user_id,omitempty"`
	EmployeeName    string               `json:"employee_name,omitempty"`
	EmployeeState   string               `json:"employee_state,omitempty"`
	GroupName       *string              `json:"group_name,omitempty"`
	StartedAt       *time.Time           `json:"started_at,omitzero"`
	AnsweredAt      *time.Time           `json:"answered_at,omitzero"`
	CompletedAt     *time.Time           `json:"completed_at,omitzero"`
	WaitSeconds     *int                 `json:"wait_seconds,omitempty"`
	DurationSeconds *int                 `json:"duration_seconds,omitempty"`
	RecordingURL    *string              `json:"recording_url,omitempty"`
	HasRecording    bool                 `json:"has_recording"`
	TicketID        *string              `json:"ticket_id,omitempty"`
	Contact         *TelephonyContactDTO `json:"contact,omitempty"`
}

type TelephonyPendingContextDTO struct {
	ID             string               `json:"id"`
	ExternalCallID string               `json:"external_call_id"`
	ClientPhone    string               `json:"client_phone"`
	ExpiresAt      time.Time            `json:"expires_at,omitzero"`
	Contact        *TelephonyContactDTO `json:"contact,omitempty"`
	Call           *TelephonyCallDTO    `json:"call,omitempty"`
}

type TelephonyContactCompanyDTO struct {
	CompanyID      string    `json:"company_id"`
	Title          string    `json:"title"`
	ParentTitle    string    `json:"parent_title,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at,omitzero"`
	ActiveContract *bool     `json:"active_contract,omitempty"`
}

type TelephonyCallListResponseDTO struct {
	Items []TelephonyCallDTO `json:"items"`
	Total int64              `json:"total"`
}

type TelephonyLineEmployeeDTO struct {
	UserID       *uint   `json:"user_id,omitempty"`
	Login        string  `json:"login"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Provider     string  `json:"provider"`
	ProviderExt  *string `json:"provider_ext,omitempty"`
	ProviderLine *string `json:"provider_line,omitempty"`
}

type TelephonyLineDTO struct {
	Color           string                     `json:"color"`
	OnLineCount     int                        `json:"on_line_count"`
	MissedOpenCount int                        `json:"missed_open_count"`
	Employees       []TelephonyLineEmployeeDTO `json:"employees"`
}
