package models

import (
	"time"

	"gorm.io/datatypes"
)

const (
	AgentObservationStatusProcessing   = "PROCESSING"
	AgentObservationStatusApplied      = "APPLIED"
	AgentObservationStatusStaged       = "STAGED"
	AgentObservationStatusIgnored      = "IGNORED"
	AgentObservationStatusIgnoredStale = "IGNORED_STALE"
	AgentObservationStatusError        = "ERROR"
)

const (
	CandidateStatusNew       = "NEW"
	CandidateStatusInReview  = "IN_REVIEW"
	CandidateStatusApproved  = "APPROVED"
	CandidateStatusRejected  = "REJECTED"
	CandidateStatusCancelled = "CANCELLED"
)

type AgentObservation struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Source      string         `gorm:"type:text;index" json:"source"`
	ObservedAt  time.Time      `gorm:"index" json:"observed_at"`
	ServerKey   *string        `gorm:"type:text;index" json:"server_key"`
	ServerCRMID *string        `gorm:"column:server_crm_id;type:text;index" json:"server_crm_id"`
	PayloadJSON datatypes.JSON `gorm:"type:jsonb" json:"payload_json"`
	PayloadHash string         `gorm:"type:char(64);uniqueIndex" json:"payload_hash"`
	Status      string         `gorm:"type:varchar(32);index" json:"status"`
	ErrorText   *string        `gorm:"type:text" json:"error_text"`

	WorkstationID      *string `gorm:"type:text;index" json:"workstation_id"`
	CandidateID        *uint   `gorm:"index" json:"candidate_id"`
	NetworkCandidateID *uint   `gorm:"index" json:"network_candidate_id"`
	FRID               *string `gorm:"type:text;index" json:"fr_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Candidate struct {
	ID               uint           `gorm:"primaryKey" json:"id"`
	ServerKey        *string        `gorm:"type:text;index" json:"server_key"`
	ServerCRMID      *string        `gorm:"column:server_crm_id;type:text;index" json:"server_crm_id"`
	ServerURL        *string        `gorm:"type:text" json:"server_url"`
	Status           string         `gorm:"type:varchar(32);index" json:"status"`
	TicketID         *uint          `gorm:"index" json:"ticket_id"`
	Meta             datatypes.JSON `gorm:"type:jsonb" json:"meta"`
	ExistingServerID *string        `gorm:"type:text;index" json:"existing_server_id"`

	ApprovedCompanyID *string `gorm:"type:text;index" json:"approved_company_id"`
	ApprovedServerID  *string `gorm:"type:text;index" json:"approved_server_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CandidateStatusHistory struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	CandidateID uint      `gorm:"index;not null" json:"candidate_id"`
	FromStatus  *string   `gorm:"type:varchar(32)" json:"from_status"`
	ToStatus    string    `gorm:"type:varchar(32);index" json:"to_status"`
	Reason      *string   `gorm:"type:text" json:"reason"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
}

type CandidateWorkstationStaging struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	CandidateID     uint      `gorm:"index;not null" json:"candidate_id"`
	ObservationID   uint      `gorm:"index;not null" json:"observation_id"`
	ObservedAt      time.Time `gorm:"index" json:"observed_at"`
	Hostname        *string   `gorm:"type:text" json:"hostname"`
	AgentUUID       *string   `gorm:"type:text;index" json:"agent_uuid"`
	WorkstationUUID *string   `gorm:"type:text" json:"workstation_uuid"`
	TeamviewerID    *string   `gorm:"type:text;index" json:"teamviewer_id"`
	LitemanagerID   *string   `gorm:"type:text;index" json:"litemanager_id"`
	AnydeskID       *string   `gorm:"type:text;index" json:"anydesk_id"`
	URLRms          *string   `gorm:"column:url_rms;type:text" json:"url_rms"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CandidateFiscalStaging struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	CandidateID      uint       `gorm:"index;not null" json:"candidate_id"`
	ObservationID    uint       `gorm:"index;not null" json:"observation_id"`
	ObservedAt       time.Time  `gorm:"index" json:"observed_at"`
	SerialNumber     *string    `gorm:"column:serial_number;type:text" json:"serial_number"`
	SerialNormalized *string    `gorm:"column:serial_normalized;type:text;index" json:"serial_normalized"`
	RNKKT            *string    `gorm:"column:rn_kkt;type:text" json:"rn_kkt"`
	ModelName        *string    `gorm:"column:model_name;type:text" json:"model_name"`
	INN              *string    `gorm:"column:inn;type:text" json:"inn"`
	FNNumber         *string    `gorm:"column:fn_number;type:text" json:"fn_number"`
	FNExpireDate     *time.Time `json:"fn_expire_date"`
	OrganizationName *string    `gorm:"type:text" json:"organization_name"`
	Address          *string    `gorm:"type:text" json:"address"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
