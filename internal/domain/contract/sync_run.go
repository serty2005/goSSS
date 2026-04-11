package contract

import (
	"etalon-server/internal/domain/common"
	"time"

	"gorm.io/datatypes"
)

const (
	ServicePointSyncRunModeManual    = "manual"
	ServicePointSyncRunModeAutomatic = "automatic"

	ServicePointSyncRunStatusSuccess = "success"
	ServicePointSyncRunStatusPartial = "partial"
	ServicePointSyncRunStatusFailed  = "failed"
	ServicePointSyncRunStatusSkipped = "skipped"

	ServicePointSyncRunActorUser   = "user"
	ServicePointSyncRunActorSystem = "system"
)

type ServicePointSyncRun struct {
	common.Base
	Mode          string         `json:"mode" gorm:"type:varchar(32);not null;index"`
	Status        string         `json:"status" gorm:"type:varchar(32);not null;index"`
	ActorType     string         `json:"actor_type" gorm:"type:varchar(32);not null;index"`
	ActorUserID   *uint          `json:"actor_user_id,omitempty" gorm:"index"`
	ActorName     *string        `json:"actor_name,omitempty" gorm:"type:text"`
	Note          *string        `json:"note,omitempty" gorm:"type:text"`
	StartedAt     time.Time      `json:"started_at" gorm:"index;not null"`
	CompletedAt   *time.Time     `json:"completed_at,omitzero" gorm:"index"`
	ReportRows    int            `json:"report_rows"`
	ToCreate      int            `json:"to_create"`
	ToUpdate      int            `json:"to_update"`
	ToDelete      int            `json:"to_delete"`
	BlockedRows   int            `json:"blocked_rows"`
	Processed     int            `json:"processed"`
	Created       int            `json:"created"`
	Updated       int            `json:"updated"`
	Deleted       int            `json:"deleted"`
	ActiveImports datatypes.JSON `json:"active_imports" gorm:"type:jsonb"`
	QueueItems    datatypes.JSON `json:"queue_items" gorm:"type:jsonb"`
	Errors        datatypes.JSON `json:"errors" gorm:"type:jsonb"`
	ErrorDetails  datatypes.JSON `json:"error_details" gorm:"type:jsonb"`
}

func (ServicePointSyncRun) TableName() string { return "contract_service_point_sync_runs" }

type ServicePointSyncRunImportSnapshot struct {
	ID             string     `json:"id,omitempty"`
	Source         string     `json:"source,omitempty"`
	MessageID      string     `json:"message_id,omitempty"`
	AttachmentName string     `json:"attachment_name,omitempty"`
	AttachmentHash string     `json:"attachment_hash,omitempty"`
	ReceivedAt     *time.Time `json:"received_at,omitzero"`
	ProcessedAt    *time.Time `json:"processed_at,omitzero"`
	Status         string     `json:"status,omitempty"`
	RowsCount      int        `json:"rows_count,omitempty"`
}

type ServicePointSyncRunQueueItemSnapshot struct {
	Key                 string                                 `json:"key,omitempty"`
	Action              string                                 `json:"action,omitempty"`
	ServicePointName    string                                 `json:"service_point_name,omitempty"`
	ServicePointCode    string                                 `json:"service_point_code,omitempty"`
	ContractorID        string                                 `json:"contractor_id,omitempty"`
	ContractorName      string                                 `json:"contractor_name,omitempty"`
	ContractType        string                                 `json:"contract_type,omitempty"`
	B24ElementID        *int64                                 `json:"b24_element_id,omitempty"`
	CurrentName         string                                 `json:"current_name,omitempty"`
	CurrentCode         string                                 `json:"current_code,omitempty"`
	CurrentContractType string                                 `json:"current_contract_type,omitempty"`
	ChangeSet           []ServicePointSyncRunFieldDiffSnapshot `json:"change_set,omitzero"`
	MatchedPointIDs     []int64                                `json:"matched_point_ids,omitzero"`
	FilledFields        int                                    `json:"filled_fields,omitempty"`
	IsMapped            bool                                   `json:"is_mapped,omitempty"`
	Reason              string                                 `json:"reason,omitempty"`
}

type ServicePointSyncRunFieldDiffSnapshot struct {
	Field        string `json:"field,omitempty"`
	Label        string `json:"label,omitempty"`
	CurrentValue string `json:"current_value,omitempty"`
	NextValue    string `json:"next_value,omitempty"`
}

type ServicePointSyncRunErrorDetailSnapshot struct {
	Key              string `json:"key,omitempty"`
	Action           string `json:"action,omitempty"`
	ServicePointName string `json:"service_point_name,omitempty"`
	ServicePointCode string `json:"service_point_code,omitempty"`
	B24ElementID     *int64 `json:"b24_element_id,omitempty"`
	Message          string `json:"message,omitempty"`
}
