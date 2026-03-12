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
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ContractServicePointConflictDTO описывает актуальный конфликт дублей по точкам Bitrix24.
type ContractServicePointConflictDTO struct {
	ID                   string    `json:"id"`
	ConflictType         string    `json:"conflict_type"`
	ServicePointName     string    `json:"service_point_name"`
	ContractorID         *string   `json:"contractor_id,omitempty"`
	MessageID            *string   `json:"message_id,omitempty"`
	AttachmentHash       *string   `json:"attachment_hash,omitempty"`
	MatchedPointIDs      []int64   `json:"matched_point_ids,omitempty"`
	MappedPointIDs       []int64   `json:"mapped_point_ids,omitempty"`
	DeletionCandidateIDs []int64   `json:"deletion_candidate_ids,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ContractSyncStateDTO возвращает оператору текущее состояние почтовой синхронизации.
type ContractSyncStateDTO struct {
	LatestImport  *ContractMailImportDTO            `json:"latest_import,omitempty"`
	RecentImports []ContractMailImportDTO           `json:"recent_imports"`
	Conflicts     []ContractServicePointConflictDTO `json:"conflicts"`
}
