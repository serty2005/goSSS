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
	ID            string         `json:"id" gorm:"primaryKey;type:text"`
	Attributes    datatypes.JSON `json:"attributes" gorm:"type:jsonb"` // Хранилище произвольных атрибутов от внешних систем
	LastUpdatedBy string         `json:"last_updated_by" gorm:"type:varchar(50);default:'unknown'"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `json:"deleted_at" gorm:"index"`
}

// BeforeCreate генерирует UUID перед созданием.
func (base *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if base.ID == "" {
		base.ID = uuid.New().String()
	}
	return
}
