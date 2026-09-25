package tickets

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ChecklistMaxDepth ограничивает вложенность пунктов чеклиста (1 - только корневые пункты).
const ChecklistMaxDepth = 3

// TicketChecklistItem описывает пункт локального чеклиста тикета.
type TicketChecklistItem struct {
	ID          string                        `json:"id" gorm:"primaryKey;type:text"`
	TicketID    string                        `json:"ticket_id" gorm:"type:text;not null;index:idx_ticket_checklist_items_ticket_parent,priority:1"`
	ParentID    *string                       `json:"parent_id,omitempty" gorm:"type:text;index:idx_ticket_checklist_items_ticket_parent,priority:2"`
	Title       string                        `json:"title" gorm:"type:text;not null"`
	Position    int                           `json:"position" gorm:"not null;default:0"`
	IsDone      bool                          `json:"is_done" gorm:"not null;default:false"`
	DoneAt      *time.Time                    `json:"done_at,omitempty"`
	DoneByID    *uint                         `json:"done_by_id,omitempty"`
	CreatedByID *uint                         `json:"created_by_id,omitempty"`
	Assignees   []TicketChecklistItemAssignee `json:"assignees,omitempty" gorm:"foreignKey:ItemID;constraint:OnDelete:CASCADE"`
	CreatedAt   time.Time                     `json:"created_at"`
	UpdatedAt   time.Time                     `json:"updated_at"`
}

func (i *TicketChecklistItem) BeforeCreate(tx *gorm.DB) (err error) {
	if i.ID == "" {
		i.ID = uuid.New().String()
	}
	return
}

// TicketChecklistItemAssignee связывает пункт чеклиста с исполнителем.
type TicketChecklistItemAssignee struct {
	ItemID string `json:"item_id" gorm:"primaryKey;type:text"`
	UserID uint   `json:"user_id" gorm:"primaryKey;index"`
}

// ChecklistTemplateNode описывает пункт шаблона чеклиста вместе с подпунктами.
type ChecklistTemplateNode struct {
	Title    string                  `json:"title"`
	Children []ChecklistTemplateNode `json:"children,omitempty"`
}

// ChecklistTemplate хранит шаблон чеклиста, который можно применить к тикету.
type ChecklistTemplate struct {
	ID          string                                      `json:"id" gorm:"primaryKey;type:text"`
	Title       string                                      `json:"title" gorm:"type:text;not null"`
	Description string                                      `json:"description" gorm:"type:text"`
	Items       datatypes.JSONType[[]ChecklistTemplateNode] `json:"items" gorm:"type:jsonb;not null"`
	IsActive    bool                                        `json:"is_active" gorm:"not null;default:true;index"`
	CreatedByID *uint                                       `json:"created_by_id,omitempty"`
	CreatedAt   time.Time                                   `json:"created_at"`
	UpdatedAt   time.Time                                   `json:"updated_at"`
}

func (t *ChecklistTemplate) BeforeCreate(tx *gorm.DB) (err error) {
	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	return
}

// ChecklistRepository описывает хранилище чеклистов тикетов и шаблонов.
type ChecklistRepository interface {
	ListItems(ctx context.Context, ticketID string) ([]TicketChecklistItem, error)
	GetItem(ctx context.Context, ticketID string, itemID string) (*TicketChecklistItem, error)
	NextPosition(ctx context.Context, ticketID string, parentID *string) (int, error)
	CreateItems(ctx context.Context, items []TicketChecklistItem) error
	UpdateItem(ctx context.Context, ticketID string, itemID string, updates map[string]interface{}) error
	ReplaceAssignees(ctx context.Context, itemID string, userIDs []uint) error
	DeleteItems(ctx context.Context, ticketID string, itemIDs []string) error
	UpdatePositions(ctx context.Context, ticketID string, parentID *string, orderedIDs []string) error

	ListTemplates(ctx context.Context, activeOnly bool) ([]ChecklistTemplate, error)
	GetTemplate(ctx context.Context, id string) (*ChecklistTemplate, error)
	CreateTemplate(ctx context.Context, template *ChecklistTemplate) error
	UpdateTemplate(ctx context.Context, template *ChecklistTemplate) error
	DeleteTemplate(ctx context.Context, id string) error
}
