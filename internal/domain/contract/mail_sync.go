package contract

import (
	"etalon-server/internal/domain/common"
	"time"

	"gorm.io/datatypes"
)

const (
	MailImportStatusProcessed = "processed"
	MailImportStatusFailed    = "failed"
)

type MailImport struct {
	common.Base
	MessageID      string         `json:"message_id" gorm:"type:text;index"`
	AttachmentName string         `json:"attachment_name" gorm:"type:text;not null"`
	AttachmentHash string         `json:"attachment_hash" gorm:"type:char(64);uniqueIndex;not null"`
	ReceivedAt     *time.Time     `json:"received_at"`
	Status         string         `json:"status" gorm:"type:varchar(32);not null;index"`
	ErrorText      *string        `json:"error_text" gorm:"type:text"`
	ProcessedAt    *time.Time     `json:"processed_at"`
	ReportRows     datatypes.JSON `json:"report_rows" gorm:"type:jsonb"`
}

func (MailImport) TableName() string { return "contract_mail_imports" }

type ServicePointSyncConflict struct {
	common.Base
	ConflictType     string         `json:"conflict_type" gorm:"type:varchar(32);not null;index"`
	ServicePointName string         `json:"service_point_name" gorm:"type:text;not null;index"`
	ContractorID     *string        `json:"contractor_id,omitempty" gorm:"type:varchar(128);index"`
	MessageID        *string        `json:"message_id,omitempty" gorm:"type:text;index"`
	AttachmentHash   *string        `json:"attachment_hash,omitempty" gorm:"type:char(64);index"`
	Details          datatypes.JSON `json:"details" gorm:"type:jsonb"`
}

func (ServicePointSyncConflict) TableName() string { return "contract_service_point_sync_conflicts" }

type DailyCompanyContractSnapshot struct {
	CompanyID        string
	ServicePointID   int64
	ServicePointName string
	ServicePointCode string
	ContractorID     string
	ContractType     string
	Active           bool
	StartDate        *time.Time
	EndDate          *time.Time
	ClientOrder      string
	SourceHash       string
}
