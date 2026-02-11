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
	ID          uint           `gorm:"primaryKey"`
	Source      string         `gorm:"type:text;index"`
	ObservedAt  time.Time      `gorm:"index"`
	ServerKey   *string        `gorm:"type:text;index"`
	ServerCRMID *string        `gorm:"column:server_crm_id;type:text;index"`
	PayloadJSON datatypes.JSON `gorm:"type:jsonb"`
	PayloadHash string         `gorm:"type:char(64);uniqueIndex"`
	Status      string         `gorm:"type:varchar(32);index"`
	ErrorText   *string        `gorm:"type:text"`

	WorkstationID *string `gorm:"type:text;index"`
	CandidateID   *uint   `gorm:"index"`
	FRID          *string `gorm:"type:text;index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type Candidate struct {
	ID          uint           `gorm:"primaryKey"`
	ServerKey   *string        `gorm:"type:text;index"`
	ServerCRMID *string        `gorm:"column:server_crm_id;type:text;index"`
	ServerURL   *string        `gorm:"type:text"`
	Status      string         `gorm:"type:varchar(32);index"`
	TicketID    *uint          `gorm:"index"`
	Meta        datatypes.JSON `gorm:"type:jsonb"`

	ApprovedCompanyID *string `gorm:"type:text;index"`
	ApprovedServerID  *string `gorm:"type:text;index"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type CandidateStatusHistory struct {
	ID          uint      `gorm:"primaryKey"`
	CandidateID uint      `gorm:"index;not null"`
	FromStatus  *string   `gorm:"type:varchar(32)"`
	ToStatus    string    `gorm:"type:varchar(32);index"`
	Reason      *string   `gorm:"type:text"`
	CreatedAt   time.Time `gorm:"index"`
}

type CandidateWorkstationStaging struct {
	ID              uint      `gorm:"primaryKey"`
	CandidateID     uint      `gorm:"index;not null"`
	ObservationID   uint      `gorm:"index;not null"`
	ObservedAt      time.Time `gorm:"index"`
	Hostname        *string   `gorm:"type:text"`
	AgentUUID       *string   `gorm:"type:text;index"`
	WorkstationUUID *string   `gorm:"type:text"`
	TeamviewerID    *string   `gorm:"type:text;index"`
	LitemanagerID   *string   `gorm:"type:text;index"`
	AnydeskID       *string   `gorm:"type:text;index"`
	URLRms          *string   `gorm:"column:url_rms;type:text"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CandidateFiscalStaging struct {
	ID               uint      `gorm:"primaryKey"`
	CandidateID      uint      `gorm:"index;not null"`
	ObservationID    uint      `gorm:"index;not null"`
	ObservedAt       time.Time `gorm:"index"`
	SerialNumber     *string   `gorm:"column:serial_number;type:text"`
	SerialNormalized *string   `gorm:"column:serial_normalized;type:text;index"`
	RNKKT            *string   `gorm:"column:rn_kkt;type:text"`
	ModelName        *string   `gorm:"column:model_name;type:text"`
	INN              *string   `gorm:"column:inn;type:text"`
	FNNumber         *string   `gorm:"column:fn_number;type:text"`
	FNExpireDate     *time.Time
	OrganizationName *string `gorm:"type:text"`
	Address          *string `gorm:"type:text"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
