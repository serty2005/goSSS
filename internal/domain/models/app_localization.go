package models

import (
	"time"

	"gorm.io/datatypes"
)

type AppLocalization struct {
	ID        uint           `gorm:"primarykey"`
	Key       string         `gorm:"type:varchar(100);uniqueIndex;not null"`
	Payload   datatypes.JSON `gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (AppLocalization) TableName() string {
	return "app_localizations"
}
