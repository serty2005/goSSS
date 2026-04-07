package telephony

import "time"

const (
	ProviderMegafonVATS = "megafon_vats"
)

const (
	IncomingEventCommandEvent   = "event"
	IncomingEventCommandHistory = "history"
)

const (
	IncomingEventStatusNew        = "new"
	IncomingEventStatusQueued     = "queued"
	IncomingEventStatusProcessing = "processing"
	IncomingEventStatusDone       = "done"
	IncomingEventStatusFailed     = "failed"
	IncomingEventStatusIgnored    = "ignored"
)

const (
	PendingContextStatusNew       = "new"
	PendingContextStatusBound     = "bound"
	PendingContextStatusExpired   = "expired"
	PendingContextStatusDismissed = "dismissed"
)

const (
	CallArtifactTypeRecording = "recording"
)

type ProviderEmployee struct {
	Provider      string    `json:"provider" gorm:"primaryKey;type:varchar(50)"`
	EmployeeLogin string    `json:"employee_login" gorm:"primaryKey;type:varchar(255)"`
	EmployeeName  string    `json:"employee_name" gorm:"type:text"`
	Ext           *string   `json:"ext,omitempty" gorm:"type:varchar(64)"`
	Telnum        *string   `json:"telnum,omitempty" gorm:"type:varchar(64)"`
	Status        *string   `json:"status,omitempty" gorm:"type:varchar(64);index"`
	RawJSON       string    `json:"raw_json" gorm:"type:text"`
	LastSeenAt    time.Time `json:"last_seen_at" gorm:"not null;index"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (ProviderEmployee) TableName() string {
	return "telephony_provider_employees"
}

type IncomingEvent struct {
	ID             string     `json:"id" gorm:"primaryKey;type:uuid"`
	Provider       string     `json:"provider" gorm:"type:varchar(50);not null;index:idx_telephony_incoming_lookup"`
	Cmd            string     `json:"cmd" gorm:"type:varchar(32);not null;index:idx_telephony_incoming_lookup"`
	EventName      string     `json:"event_name" gorm:"type:varchar(64);not null;default:'';index:idx_telephony_incoming_lookup"`
	ExternalCallID string     `json:"external_call_id" gorm:"type:varchar(128);not null;index:idx_telephony_incoming_lookup"`
	PayloadRaw     string     `json:"payload_raw" gorm:"type:text;not null"`
	PayloadHash    string     `json:"payload_hash" gorm:"type:char(64);uniqueIndex;not null"`
	Status         string     `json:"status" gorm:"type:varchar(20);not null;default:'new';index:idx_telephony_incoming_status_received"`
	Attempts       int        `json:"attempts" gorm:"not null;default:0"`
	LastError      *string    `json:"last_error,omitempty" gorm:"type:text"`
	ReceivedAt     time.Time  `json:"received_at" gorm:"not null;autoCreateTime;index:idx_telephony_incoming_status_received"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (IncomingEvent) TableName() string {
	return "telephony_incoming_events"
}

type Call struct {
	ID              string     `json:"id" gorm:"primaryKey;type:uuid"`
	Provider        string     `json:"provider" gorm:"type:varchar(50);not null;uniqueIndex:idx_telephony_calls_provider_external,priority:1"`
	ExternalCallID  string     `json:"external_call_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_telephony_calls_provider_external,priority:2"`
	Direction       string     `json:"direction" gorm:"type:varchar(32);not null;default:'';index"`
	Status          string     `json:"status" gorm:"type:varchar(32);not null;default:'';index"`
	MissedStatus    *string    `json:"missed_status,omitempty" gorm:"type:varchar(64);index"`
	ClientPhone     *string    `json:"client_phone,omitempty" gorm:"type:varchar(64);index"`
	VATNumber       *string    `json:"vat_number,omitempty" gorm:"column:vat_number;type:varchar(64);index"`
	EmployeeLogin   *string    `json:"employee_login,omitempty" gorm:"type:varchar(255);index"`
	EmployeeUserID  *uint      `json:"employee_user_id,omitempty" gorm:"index"`
	GroupName       *string    `json:"group_name,omitempty" gorm:"type:text"`
	StartedAt       *time.Time `json:"started_at,omitempty" gorm:"index"`
	AnsweredAt      *time.Time `json:"answered_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty" gorm:"index"`
	WaitSeconds     *int       `json:"wait_seconds,omitempty"`
	DurationSeconds *int       `json:"duration_seconds,omitempty"`
	RecordingURL    *string    `json:"recording_url,omitempty" gorm:"type:text"`
	HasRecording    bool       `json:"has_recording" gorm:"not null;default:false"`
	LastEventType   *string    `json:"last_event_type,omitempty" gorm:"type:varchar(64)"`
	RawSnapshot     string     `json:"raw_snapshot" gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (Call) TableName() string {
	return "telephony_calls"
}

type CallHistorySyncWindow struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	Provider      string    `json:"provider" gorm:"type:varchar(50);not null;index:idx_telephony_call_history_scope_range,priority:1"`
	EmployeeLogin *string   `json:"employee_login,omitempty" gorm:"type:varchar(255);index:idx_telephony_call_history_scope_range,priority:2"`
	StartedFrom   time.Time `json:"started_from" gorm:"not null;index:idx_telephony_call_history_scope_range,priority:3"`
	StartedTo     time.Time `json:"started_to" gorm:"not null;index"`
	SyncedAt      time.Time `json:"synced_at" gorm:"not null;index"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (CallHistorySyncWindow) TableName() string {
	return "telephony_call_history_sync_windows"
}

type CallEvent struct {
	ID                  uint      `json:"id" gorm:"primaryKey"`
	TelephonyCallID     string    `json:"telephony_call_id" gorm:"type:uuid;not null;index"`
	EventType           string    `json:"event_type" gorm:"type:varchar(64);not null;index"`
	ExternalCallID      string    `json:"external_call_id" gorm:"type:varchar(128);not null;index"`
	SecondCallID        *string   `json:"second_call_id,omitempty" gorm:"type:varchar(128);index"`
	IncomingPayloadHash string    `json:"incoming_payload_hash" gorm:"type:char(64);uniqueIndex;not null"`
	PayloadRaw          string    `json:"payload_raw" gorm:"type:text;not null"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (CallEvent) TableName() string {
	return "telephony_call_events"
}

type CallArtifact struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	TelephonyCallID string    `json:"telephony_call_id" gorm:"type:uuid;not null;uniqueIndex:idx_telephony_call_artifacts_call_type,priority:1"`
	ArtifactType    string    `json:"artifact_type" gorm:"type:varchar(64);not null;uniqueIndex:idx_telephony_call_artifacts_call_type,priority:2;index"`
	URL             *string   `json:"url,omitempty" gorm:"type:text"`
	StorageKey      *string   `json:"storage_key,omitempty" gorm:"type:text"`
	MimeType        *string   `json:"mime_type,omitempty" gorm:"type:varchar(128)"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (CallArtifact) TableName() string {
	return "telephony_call_artifacts"
}

type CallTicketLink struct {
	TelephonyCallID string    `json:"telephony_call_id" gorm:"primaryKey;type:uuid"`
	TicketID        string    `json:"ticket_id" gorm:"primaryKey;type:text"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (CallTicketLink) TableName() string {
	return "telephony_call_ticket_links"
}

type PendingContext struct {
	ID             string    `json:"id" gorm:"primaryKey;type:uuid"`
	EmployeeUserID uint      `json:"employee_user_id" gorm:"not null;index:idx_telephony_pending_user_status"`
	ExternalCallID string    `json:"external_call_id" gorm:"type:varchar(128);not null;uniqueIndex"`
	ClientPhone    string    `json:"client_phone" gorm:"type:varchar(64);not null;default:'';index"`
	Status         string    `json:"status" gorm:"type:varchar(32);not null;default:'new';index:idx_telephony_pending_user_status"`
	ExpiresAt      time.Time `json:"expires_at" gorm:"not null;index"`
	LinkedTicketID *string   `json:"linked_ticket_id,omitempty" gorm:"type:text"`
	DecisionReason *string   `json:"decision_reason,omitempty" gorm:"type:text"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (PendingContext) TableName() string {
	return "telephony_pending_contexts"
}

type Contact struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	PhoneNormalized string    `json:"phone_normalized" gorm:"type:varchar(64);uniqueIndex;not null"`
	PhoneDisplay    string    `json:"phone_display" gorm:"type:varchar(64);not null;default:''"`
	Name            *string   `json:"name,omitempty" gorm:"type:text"`
	BitrixContactID *string   `json:"bitrix_contact_id,omitempty" gorm:"type:varchar(128);index"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (Contact) TableName() string {
	return "contacts"
}

type ContactCompanyLink struct {
	ContactID  uint      `json:"contact_id" gorm:"primaryKey"`
	CompanyID  string    `json:"company_id" gorm:"primaryKey;type:text"`
	LastSeenAt time.Time `json:"last_seen_at" gorm:"not null;index"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (ContactCompanyLink) TableName() string {
	return "contact_company_links"
}
