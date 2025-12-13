package tickets

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Attachment представляет файл, прикрепленный к сущности (обычно к Тикету).
type Attachment struct {
	ID         string `json:"id" gorm:"primaryKey;type:text"`
	EntityID   string `json:"entity_id" gorm:"type:text;index;not null"` // ID родительской сущности
	EntityType string `json:"entity_type" gorm:"type:varchar(50);default:'Ticket';index"`

	FileName string `json:"file_name" gorm:"type:text;not null"`
	FilePath string `json:"file_path" gorm:"type:text;not null"` // Путь на диске или S3 URL
	MimeType string `json:"mime_type" gorm:"type:varchar(100)"`
	Size     int64  `json:"size"`

	UploaderID *uint     `json:"uploader_id" gorm:"index"`
	CreatedAt  time.Time `json:"created_at"`
}

func (a *Attachment) BeforeCreate(tx *gorm.DB) (err error) {
	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	return
}
