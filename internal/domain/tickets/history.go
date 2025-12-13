package tickets

import (
	"time"
)

// TicketHistory хранит аудит изменений заявки.
type TicketHistory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	TicketID  string    `json:"ticket_id" gorm:"type:text;index;not null"` // UUID тикета
	UserID    *uint     `json:"user_id" gorm:"index"`                      // Кто изменил (nil если система)
	Field     string    `json:"field" gorm:"type:varchar(100)"`            // Измененное поле (status, assignee, etc.)
	OldValue  string    `json:"old_value" gorm:"type:text"`
	NewValue  string    `json:"new_value" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}
