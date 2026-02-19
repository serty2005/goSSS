package models

import "time"

const (
	CompanyOwnerModeNormal     = "normal"
	CompanyOwnerModeNetworkHub = "network_hub"
)

const (
	OwnerBindingModeAuto   = "auto"
	OwnerBindingModeManual = "manual"
)

const (
	NetworkCandidateStatusNew       = "NEW"
	NetworkCandidateStatusInReview  = "IN_REVIEW"
	NetworkCandidateStatusApproved  = "APPROVED"
	NetworkCandidateStatusRejected  = "REJECTED"
	NetworkCandidateStatusCancelled = "CANCELLED"
)

const (
	NetworkCandidateGroupStatusActive      = "ACTIVE"
	NetworkCandidateGroupStatusTransferred = "TRANSFERRED"
)

const (
	OwnerChangeSourceManualResolution = "manual_resolution"
	OwnerChangeSourceManualUpdate     = "manual_update"
	OwnerChangeSourceCreated          = "created"
	OwnerChangeSourceNetworkAuto      = "network_auto"
	OwnerChangeSourceNetworkAutoWS    = "network_auto_ws"
	OwnerChangeSourceNetworkAutoFR    = "network_auto_fr"
	OwnerChangeSourceNetworkAutoBoth  = "network_auto_both"
	OwnerChangeSourceNetworkConflict  = "network_conflict"
	OwnerChangeSourceCandidateApprove = "candidate_approve"
	OwnerChangeSourceAgentDataUpdate  = "agent_data_update"
	OwnerChangeSourceConnCopyRemoteID = "connection_copy_remote_id"
	OwnerChangeSourceConnCopyServerIP = "connection_copy_server_address"
)

type OwnerChangeHistory struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	EntityType      string    `gorm:"type:varchar(32);index:idx_owner_change_entity,priority:1;not null" json:"entity_type"`
	EntityID        string    `gorm:"type:text;index:idx_owner_change_entity,priority:2;not null" json:"entity_id"`
	FromOwnerID     *string   `gorm:"type:text" json:"from_owner_id"`
	ToOwnerID       string    `gorm:"type:text;not null" json:"to_owner_id"`
	ChangeSource    string    `gorm:"type:varchar(32);not null" json:"change_source"`
	Comment         *string   `gorm:"type:text" json:"comment"`
	ChangedByUserID *string   `gorm:"type:text" json:"changed_by_user_id"`
	AgentUUID       *string   `gorm:"type:text;index" json:"agent_uuid"`
	ObservationID   *uint     `gorm:"index" json:"observation_id"`
	CreatedAt       time.Time `gorm:"index" json:"created_at"`
}

type NetworkCandidate struct {
	ID           uint    `gorm:"primaryKey" json:"id"`
	Status       string  `gorm:"type:varchar(32);index;not null" json:"status"`
	HubCompanyID string  `gorm:"type:text;index;not null" json:"hub_company_id"`
	ServerID     string  `gorm:"type:text;index;not null" json:"server_id"`
	ServerKey    *string `gorm:"type:text;index" json:"server_key"`
	ServerCRMID  *string `gorm:"column:server_crm_id;type:text;index" json:"server_crm_id"`
	ServerURL    *string `gorm:"type:text" json:"server_url"`
	// ConflictInfo содержит описание конфликта владельцев.
	// Заполняется когда РС найдена у одной дочерней компании, а ФР у другой.
	ConflictInfo *string `gorm:"type:text" json:"conflict_info"`
	// WSOwnerCandidate содержит ID компании-кандидата по РС (при конфликте).
	WSOwnerCandidate *string `gorm:"type:text" json:"ws_owner_candidate"`
	// FROwnerCandidate содержит ID компании-кандидата по ФР (при конфликте).
	FROwnerCandidate *string   `gorm:"type:text" json:"fr_owner_candidate"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type NetworkCandidateGroup struct {
	ID            uint      `gorm:"primaryKey" json:"id"`
	CandidateID   uint      `gorm:"index;not null" json:"candidate_id"`
	ObservationID uint      `gorm:"index:idx_network_candidate_observation,priority:1;not null" json:"observation_id"`
	Status        string    `gorm:"type:varchar(32);index;not null" json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NetworkCandidateWSStaging struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	GroupID         uint      `gorm:"index;not null" json:"group_id"`
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

type NetworkCandidateFRStaging struct {
	ID               uint       `gorm:"primaryKey" json:"id"`
	GroupID          uint       `gorm:"index;not null" json:"group_id"`
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
