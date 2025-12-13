package common

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Base содержит общие поля для всех сущностей (CMDB и Tickets).
// MetaClass удален, так как мы переходим к собственной схеме типов.
type Base struct {
	ID            string         `gorm:"primaryKey;type:text"`
	Attributes    datatypes.JSON `gorm:"type:jsonb"` // Хранилище произвольных атрибутов от внешних систем
	LastUpdatedBy string         `gorm:"type:varchar(50);default:'unknown'"`
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
