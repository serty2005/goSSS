package tickets

import (
	"time"
)

const (
	HistoryActionFieldChanged     = "field_changed"
	HistoryActionCommentAdded     = "comment_added"
	HistoryActionConnectionCopied = "connection_copied"
)

const (
	HistoryFieldStatus      = "status"
	HistoryFieldAssignee    = "assignee"
	HistoryFieldDescription = "description"
	HistoryFieldResult      = "result"
	HistoryFieldCompany     = "company"
	HistoryFieldAsset       = "asset"
	HistoryFieldComment     = "comment"
	HistoryFieldConnection  = "connection"
)

type TicketHistory struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	TicketID  string    `json:"ticket_id" gorm:"type:text;index;not null"`
	UserID    *uint     `json:"user_id" gorm:"index"`
	Action    string    `json:"action" gorm:"type:varchar(50);index;not null;default:'field_changed'"`
	Field     string    `json:"field" gorm:"type:varchar(100)"`
	OldValue  string    `json:"old_value" gorm:"type:text"`
	NewValue  string    `json:"new_value" gorm:"type:text"`
	CreatedAt time.Time `json:"created_at" gorm:"index"`
}
