package tickets

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	RelationTypeDirectTicketAttachment = "direct_ticket_attachment"
	RelationTypeInlineDescription      = "inline_description"
	RelationTypeInlineComment          = "inline_comment"
	RelationTypeInlineResult           = "inline_result"
)

// FileAsset описывает внутреннюю сущность файла в хранилище.
type FileAsset struct {
	ID           string    `json:"id" gorm:"primaryKey;type:text"`
	StorageKey   string    `json:"storage_key" gorm:"type:text;not null;uniqueIndex"`
	OriginalName string    `json:"original_name" gorm:"type:text;not null"`
	MimeType     string    `json:"mime_type" gorm:"type:varchar(100)"`
	Size         int64     `json:"size"`
	Checksum     string    `json:"checksum" gorm:"type:char(64);index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (f *FileAsset) BeforeCreate(tx *gorm.DB) (err error) {
	if f.ID == "" {
		f.ID = uuid.New().String()
	}
	return
}
