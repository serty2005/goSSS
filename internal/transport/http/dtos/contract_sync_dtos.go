package dtos

import "time"

// ContractMailImportDTO описывает один прогон почтового импорта контрактов.
type ContractMailImportDTO struct {
	ID             string     `json:"id"`
	MessageID      string     `json:"message_id"`
	AttachmentName string     `json:"attachment_name"`
	AttachmentHash string     `json:"attachment_hash"`
	ReceivedAt     *time.Time `json:"received_at,omitempty"`
	Status         string     `json:"status"`
	ErrorText      *string    `json:"error_text,omitempty"`
	ProcessedAt    *time.Time `json:"processed_at,omitempty"`
	RowsCount      int        `json:"rows_count"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ContractSyncQueueItemDTO struct {
	Key                 string                     `json:"key"`
	Action              string                     `json:"action"`
	ServicePointName    string                     `json:"service_point_name"`
	ServicePointCode    string                     `json:"service_point_code"`
	ContractorID        string                     `json:"contractor_id,omitempty"`
	ContractorName      string                     `json:"contractor_name,omitempty"`
	ContractType        string                     `json:"contract_type,omitempty"`
	B24ElementID        *int64                     `json:"b24_element_id,omitempty"`
	CurrentName         string                     `json:"current_name,omitempty"`
	CurrentCode         string                     `json:"current_code,omitempty"`
	CurrentContractType string                     `json:"current_contract_type,omitempty"`
	ChangeSet           []ContractSyncFieldDiffDTO `json:"change_set,omitempty"`
	MatchedPointIDs     []int64                    `json:"matched_point_ids,omitempty"`
	FilledFields        int                        `json:"filled_fields,omitempty"`
	IsMapped            bool                       `json:"is_mapped,omitempty"`
	Reason              string                     `json:"reason,omitempty"`
}

type ContractSyncFieldDiffDTO struct {
	Field        string `json:"field"`
	Label        string `json:"label"`
	CurrentValue string `json:"current_value,omitempty"`
	NextValue    string `json:"next_value,omitempty"`
}

// ContractSyncStateDTO возвращает оператору текущее состояние почтовой синхронизации.
type ContractSyncStateDTO struct {
	LatestImport       *ContractMailImportDTO     `json:"latest_import,omitempty"`
	ActiveReportImport *ContractMailImportDTO     `json:"active_report_import,omitempty"`
	RecentImports      []ContractMailImportDTO    `json:"recent_imports"`
	ReportRows         int                        `json:"report_rows"`
	ToCreate           int                        `json:"to_create"`
	ToUpdate           int                        `json:"to_update"`
	ToDelete           int                        `json:"to_delete"`
	BlockedRows        int                        `json:"blocked_rows"`
	UpsertItems        []ContractSyncQueueItemDTO `json:"upsert_items"`
	DeleteItems        []ContractSyncQueueItemDTO `json:"delete_items"`
}

type ContractSyncExecuteResultDTO struct {
	Processed    int                           `json:"processed"`
	Created      int                           `json:"created"`
	Updated      int                           `json:"updated"`
	Deleted      int                           `json:"deleted"`
	AppliedKeys  []string                      `json:"applied_keys,omitempty"`
	Errors       []string                      `json:"errors,omitempty"`
	ErrorDetails []ContractSyncExecuteErrorDTO `json:"error_details,omitempty"`
}

type ContractSyncExecuteErrorDTO struct {
	Key              string `json:"key"`
	Action           string `json:"action"`
	ServicePointName string `json:"service_point_name,omitempty"`
	ServicePointCode string `json:"service_point_code,omitempty"`
	B24ElementID     *int64 `json:"b24_element_id,omitempty"`
	Message          string `json:"message"`
}
