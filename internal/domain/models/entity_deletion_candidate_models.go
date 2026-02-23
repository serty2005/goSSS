package models

import (
	"time"

	"gorm.io/datatypes"
)

const (
	EntityDeletionCandidateStatusPending   = "PENDING"
	EntityDeletionCandidateStatusConfirmed = "CONFIRMED"
	EntityDeletionCandidateStatusCancelled = "CANCELLED"
)

const (
	EntityDeletionSourceManual          = "manual"
	EntityDeletionSourceDuplicateWorker = "duplicate_worker"
)

type EntityDeletionCandidate struct {
	ID uint `gorm:"primaryKey" json:"id"`

	EntityType string `gorm:"type:varchar(32);index:idx_entity_delete_candidate_entity,priority:1;not null" json:"entity_type"`
	EntityID   string `gorm:"type:text;index:idx_entity_delete_candidate_entity,priority:2;not null" json:"entity_id"`

	EntityDisplayName *string `gorm:"type:text" json:"entity_display_name"`
	Status            string  `gorm:"type:varchar(32);index;not null" json:"status"`

	Reason  *string `gorm:"type:text" json:"reason"`
	Source  string  `gorm:"type:varchar(32);index;not null" json:"source"`
	Comment *string `gorm:"type:text" json:"comment"`

	RequestedByUserID *string    `gorm:"type:text;index" json:"requested_by_user_id"`
	RequestedAt       time.Time  `gorm:"index;not null" json:"requested_at"`
	ConfirmedByUserID *string    `gorm:"type:text;index" json:"confirmed_by_user_id"`
	ConfirmedAt       *time.Time `gorm:"index" json:"confirmed_at"`

	DuplicateOfEntityID *string        `gorm:"type:text" json:"duplicate_of_entity_id"`
	DuplicateField      *string        `gorm:"type:varchar(64)" json:"duplicate_field"`
	DuplicateValue      *string        `gorm:"type:text" json:"duplicate_value"`
	Meta                datatypes.JSON `gorm:"type:jsonb" json:"meta"`

	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
}
