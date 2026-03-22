package models

import (
	"time"
)

// AgentCOMSignatureRule хранит подтверждённое серверное правило для COM-сигнатуры.
// Правило используется при формировании рекомендаций профиля машины и adapter_manifests.
type AgentCOMSignatureRule struct {
	ID uint `gorm:"primaryKey" json:"id"`

	SignatureKey string `gorm:"type:text;uniqueIndex;not null" json:"signature_key"`
	DeviceType   string `gorm:"type:varchar(64);index;not null" json:"device_type"`
	Label        string `gorm:"type:text" json:"label"`
	Confidence   string `gorm:"type:varchar(32)" json:"confidence"`
	ProfileHint  string `gorm:"type:varchar(64);index" json:"profile_hint"`

	SuggestedAdapter string `gorm:"type:varchar(128);index" json:"suggested_adapter"`
	Source           string `gorm:"type:varchar(64)" json:"source"`
	Notes            string `gorm:"type:text" json:"notes"`

	CreatedBy string `gorm:"type:text" json:"created_by"`
	UpdatedBy string `gorm:"type:text" json:"updated_by"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
