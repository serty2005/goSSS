package dtos

import "time"

// ContractMailImportDTO описывает один прогон почтового импорта контрактов.
type ContractMailImportDTO struct {
	ID             string     `json:"id"`
	Source         string     `json:"source,omitempty"`
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

type ContractSyncBlockedItemDTO struct {
	Key              string  `json:"key"`
	ServicePointName string  `json:"service_point_name,omitempty"`
	ServicePointCode string  `json:"service_point_code,omitempty"`
	ContractorID     string  `json:"contractor_id,omitempty"`
	ContractorName   string  `json:"contractor_name,omitempty"`
	Reason           string  `json:"reason"`
	ResolutionHint   string  `json:"resolution_hint,omitempty"`
	MatchedPointIDs  []int64 `json:"matched_point_ids,omitempty"`
}

type ContractSyncRunSummaryDTO struct {
	ID            string                  `json:"id"`
	Status        string                  `json:"status"`
	Mode          string                  `json:"mode"`
	ActorType     string                  `json:"actor_type"`
	ActorUserID   *uint                   `json:"actor_user_id,omitempty"`
	ActorName     string                  `json:"actor_name,omitempty"`
	Note          string                  `json:"note,omitempty"`
	StartedAt     time.Time               `json:"started_at"`
	CompletedAt   *time.Time              `json:"completed_at,omitzero"`
	ReportRows    int                     `json:"report_rows"`
	ToCreate      int                     `json:"to_create"`
	ToUpdate      int                     `json:"to_update"`
	ToDelete      int                     `json:"to_delete"`
	BlockedRows   int                     `json:"blocked_rows"`
	Processed     int                     `json:"processed"`
	Created       int                     `json:"created"`
	Updated       int                     `json:"updated"`
	Deleted       int                     `json:"deleted"`
	ActiveImports []ContractMailImportDTO `json:"active_imports,omitzero"`
}

type ContractSyncRunDetailsDTO struct {
	ContractSyncRunSummaryDTO
	QueueItems   []ContractSyncQueueItemDTO    `json:"queue_items,omitzero"`
	Errors       []string                      `json:"errors,omitzero"`
	ErrorDetails []ContractSyncExecuteErrorDTO `json:"error_details,omitzero"`
}

type ContractSyncAutoExecutionDTO struct {
	Enabled           bool   `json:"enabled"`
	IntervalMinutes   int    `json:"interval_minutes"`
	AppliesCreates    bool   `json:"applies_creates"`
	AppliesUpdates    bool   `json:"applies_updates"`
	AppliesDeletes    bool   `json:"applies_deletes"`
	TriggerLabel      string `json:"trigger_label,omitempty"`
	SafetyDescription string `json:"safety_description,omitempty"`
}

// ContractSyncStateDTO возвращает оператору текущее состояние почтовой синхронизации.
type ContractSyncStateDTO struct {
	LatestImport        *ContractMailImportDTO        `json:"latest_import,omitempty"`
	ActiveReportImport  *ContractMailImportDTO        `json:"active_report_import,omitempty"`
	ActiveReportImports []ContractMailImportDTO       `json:"active_report_imports,omitzero"`
	RecentImports       []ContractMailImportDTO       `json:"recent_imports"`
	RecentRuns          []ContractSyncRunSummaryDTO   `json:"recent_runs,omitzero"`
	AutoSync            *ContractSyncAutoExecutionDTO `json:"auto_sync,omitempty"`
	ReportRows          int                           `json:"report_rows"`
	ToCreate            int                           `json:"to_create"`
	ToUpdate            int                           `json:"to_update"`
	ToDelete            int                           `json:"to_delete"`
	BlockedRows         int                           `json:"blocked_rows"`
	BlockedItems        []ContractSyncBlockedItemDTO  `json:"blocked_items,omitempty"`
	UpsertItems         []ContractSyncQueueItemDTO    `json:"upsert_items"`
	DeleteItems         []ContractSyncQueueItemDTO    `json:"delete_items"`
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
