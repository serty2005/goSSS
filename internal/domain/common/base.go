package common

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base содержит общие поля для всех сущностей.
type Base struct {
	ID            string `gorm:"primaryKey;type:text"`
	MetaClass     string `gorm:"type:text"`
	LastUpdatedBy string `gorm:"type:varchar(50);default:'unknown'"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate генерирует UUID перед созданием.
func (base *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if base.ID == "" {
		base.ID = uuid.New().String()
	}
	return
}
