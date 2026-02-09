package tickets

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TicketFileLink связывает тикет с файлом и хранит происхождение вложения.
type TicketFileLink struct {
	ID           string    `json:"id" gorm:"primaryKey;type:text"`
	TicketID     string    `json:"ticket_id" gorm:"type:text;not null;index"`
	FileID       string    `json:"file_id" gorm:"type:text;not null;index"`
	RelationType string    `json:"relation_type" gorm:"type:varchar(50);not null;index"`
	CommentUUID  *string   `json:"comment_uuid" gorm:"type:text;index"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (l *TicketFileLink) BeforeCreate(tx *gorm.DB) (err error) {
	if l.ID == "" {
		l.ID = uuid.New().String()
	}
	return
}
