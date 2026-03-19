package tickets

import (
	"time"

	"gorm.io/datatypes"
)

const (
	HistoryActionFieldChanged     = "field_changed"
	HistoryActionCommentAdded     = "comment_added"
	HistoryActionCommentUpdated   = "comment_updated"
	HistoryActionCommentDeleted   = "comment_deleted"
	HistoryActionConnectionCopied = "connection_copied"
)

const (
	HistorySourceUI          = "ui"
	HistorySourceBitrix      = "bitrix"
	HistorySourceServiceDesk = "servicedesk"
	HistorySourceSystem      = "system"
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
	HistoryFieldBitrixLink  = "bitrix_link"
)

type TicketHistory struct {
	ID        uint              `json:"id" gorm:"primaryKey"`
	TicketID  string            `json:"ticket_id" gorm:"type:text;index;not null"`
	UserID    *uint             `json:"user_id" gorm:"index"`
	UserName  string            `json:"user_name,omitempty" gorm:"-"`
	Action    string            `json:"action" gorm:"type:varchar(50);index;not null;default:'field_changed'"`
	Field     string            `json:"field" gorm:"type:varchar(100)"`
	Source    string            `json:"source" gorm:"type:varchar(50);index;not null;default:'system'"`
	OldValue  string            `json:"old_value" gorm:"type:text"`
	NewValue  string            `json:"new_value" gorm:"type:text"`
	Meta      datatypes.JSONMap `json:"meta,omitempty" gorm:"type:jsonb"`
	CreatedAt time.Time         `json:"created_at" gorm:"index"`
}
