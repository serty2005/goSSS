package models

import (
	"time"

	"gorm.io/datatypes"
)

const (
	AgentRegistrationStatusSuccess         = "success"
	AgentRegistrationStatusPendingApproval = "pending_approval"
	AgentRegistrationStatusUnauthorized    = "unauthorized"
	AgentRegistrationStatusInvalidRequest  = "invalid_request"
	AgentRegistrationStatusFailed          = "failed"
)

// AgentRegistrationAttempt хранит историю bootstrap-регистрации нового агента.
// Одна запись соответствует одной HTTP-попытке обращения к /api/agents/register.
type AgentRegistrationAttempt struct {
	ID uint `gorm:"primaryKey" json:"id"`

	AgentUUID *string `gorm:"type:text;index" json:"agent_uuid"`

	Status    string  `gorm:"type:varchar(32);index;not null" json:"status"`
	ErrorText *string `gorm:"type:text" json:"error_text"`

	MachineFingerprint string         `gorm:"type:text" json:"machine_fingerprint"`
	SystemInfo         datatypes.JSON `gorm:"type:jsonb" json:"system_info"`
	Payload            datatypes.JSON `gorm:"type:jsonb" json:"payload"`
	RemoteAddr         string         `gorm:"type:text" json:"remote_addr"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
}
