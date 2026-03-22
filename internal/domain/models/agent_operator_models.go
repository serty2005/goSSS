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

// PublishedAgentAdapter хранит опубликованный server-side manifest адаптера.
// Оператор выбирает только adapter_id, а полный manifest агент получает из этой модели.
type PublishedAgentAdapter struct {
	ID uint `gorm:"primaryKey" json:"id"`

	AdapterID string `gorm:"type:varchar(128);uniqueIndex;not null" json:"adapter_id"`
	Title     string `gorm:"type:text;not null" json:"title"`

	Description string `gorm:"type:text" json:"description"`
	Published   bool   `gorm:"not null;default:false;index" json:"published"`

	Version         string `gorm:"type:varchar(64)" json:"version"`
	AdapterType     string `gorm:"type:varchar(128)" json:"adapter_type"`
	TargetOS        string `gorm:"type:varchar(32)" json:"target_os"`
	TargetArch      string `gorm:"type:varchar(32)" json:"target_arch"`
	ProtocolVersion string `gorm:"type:varchar(32)" json:"protocol_version"`
	DownloadURL     string `gorm:"type:text" json:"download_url"`
	SHA256          string `gorm:"type:varchar(128)" json:"sha256"`
	FileName        string `gorm:"type:text" json:"file_name"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
