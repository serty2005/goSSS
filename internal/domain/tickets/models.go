package tickets

import (
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/user"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Жесткий Workflow статусов.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusPending    = "pending"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
	StatusDeferred   = "deferred"
	StatusOnsite     = "onsite"
	StatusToManager  = "to_manager"
	StatusDone       = "done"
	StatusSpam       = "spam"
	StatusExecution  = "execution"
)

// Приоритеты.
const (
	PriorityCritical = "critical"
	PriorityHigh     = "high"
	PriorityMedium   = "medium"
	PriorityLow      = "low"
)

// Типы заявок.
const (
	TypeIncident       = "incident"
	TypeServiceRequest = "service_request"
	TypeConsultation   = "consultation"
	TypeCTO            = "cto"
	TypeAO             = "acceptance_ao"
	TypePaidWorks      = "paid_works"
)

// Типы активов для полиморфной связи.
const (
	AssetTypeServer         = "Server"
	AssetTypeFiscalRegister = "FiscalRegister"
	AssetTypeWorkstation    = "Workstation"
)

const (
	CommentSourceUI          = "ui"
	CommentSourceBitrix      = "bitrix"
	CommentSourcePyrus       = "pyrus"
	CommentSourceServiceDesk = "servicedesk"
)

// Ticket представляет собой заявку ServiceDesk.
// Теперь это полноценная сущность системы, а не просто кэш из Naumen.
type Ticket struct {
	common.Base

	// Идентификация и Основные данные
	Number      int    `json:"number" gorm:"uniqueIndex;autoIncrement"` // Внутренний человеко-читаемый номер
	Subject     string `json:"subject" gorm:"type:text;not null"`
	Description string `json:"description" gorm:"type:text"` // HTML/Markdown описание
	Result      string `json:"result" gorm:"type:text"`

	// Workflow и SLA
	Status        string     `json:"status" gorm:"type:varchar(50);default:'new';index"`
	Priority      string     `json:"priority" gorm:"type:varchar(20);default:'medium'"`
	Type          string     `json:"type" gorm:"type:varchar(50);default:'incident'"`
	DeadlineAt    *time.Time `json:"deadline_at" gorm:"index"`
	DeferredUntil *time.Time `json:"deferred_until,omitempty" gorm:"index"`
	DeferredByID  *uint      `json:"deferred_by_id,omitempty" gorm:"index"`

	// Связи с Пользователями
	AssigneeID *uint      `json:"assignee_id" gorm:"index"`
	Assignee   *user.User `json:"assignee" gorm:"foreignKey:AssigneeID"`

	ReporterID *uint      `json:"reporter_id" gorm:"index"` // Если заявку завел зарегистрированный пользователь
	Reporter   *user.User `json:"reporter" gorm:"foreignKey:ReporterID"`
	// Для внешних заявок (email/телефон), если ReporterID nil
	ReporterName  string `json:"reporter_name" gorm:"type:varchar(255)"`
	ReporterEmail string `json:"reporter_email" gorm:"type:varchar(255)"`

	// Связи с CMDB
	CompanyID string `json:"company_id" gorm:"type:text;index"`
	// Read-only поле для JOIN с таблицей компаний.
	// `->` означает, что поле только для чтения (не будет создана колонка в таблице tickets).
	CompanyName string  `json:"company_name,omitempty" gorm:"->"`
	ContractID  *string `json:"contract_id,omitempty" gorm:"type:text"`
	ContactID   *uint   `json:"contact_id,omitempty" gorm:"index"`
	// Признак тикета по общему контракту (вычисляется, не хранится в БД).
	IsCommonContract bool `json:"is_common_contract,omitempty" gorm:"-"`

	// Полиморфная связь с оборудованием
	AssetID   *string `json:"asset_id,omitempty" gorm:"type:text;index"`
	AssetType *string `json:"asset_type,omitempty" gorm:"type:varchar(50)"`

	// Внешние системы (для обратной совместимости и миграции)
	ServiceDeskUUID string     `json:"service_desk_uuid" gorm:"index"`
	SyncWithBitrix  bool       `json:"sync_with_bitrix" gorm:"not null;default:true;index"`
	IsArchived      bool       `json:"is_archived" gorm:"not null;default:false;index"`
	ArchivedAt      *time.Time `json:"archived_at,omitempty" gorm:"index"`

	// Точка обслуживания в Bitrix24 (ID элемента списка IBLOCK_ID=101).
	BitrixServicePointID *int64 `json:"bitrix_service_point_id,omitempty" gorm:"index"`
	BitrixDealTitle      string `json:"bitrix_deal_title" gorm:"type:text"`
	BitrixDealID         *int64 `json:"bitrix_deal_id,omitempty" gorm:"-"`
	BitrixDealURL        string `json:"bitrix_deal_url,omitempty" gorm:"-"`
	PyrusTaskID          *int64 `json:"pyrus_task_id,omitempty" gorm:"-"`
	PyrusTaskURL         string `json:"pyrus_task_url,omitempty" gorm:"-"`
}

// TicketDetails — составная структура для отображения на UI.
type TicketDetails struct {
	Metadata    Ticket          `json:"metadata"`
	CompanyName string          `json:"company_name,omitempty"`
	History     []TicketHistory `json:"history"`
	Attachments []Attachment    `json:"attachments"`
	Comments    []Comment       `json:"comments"` // Оставляем пока для совместимости с легаси комментариями
}

// Comment представляет легаси комментарий (планируется к замене на History).
type Comment struct {
	UUID          string    `json:"uuid"`
	Text          string    `json:"text"`
	AuthorName    string    `json:"author_name"`
	CreationDate  time.Time `json:"creation_date"`
	IsInternal    bool      `json:"is_internal"`
	IsPrivate     bool      `json:"is_private"`
	ReplyToClient bool      `json:"reply_to_client"`
}

// TicketComment хранит комментарии в БД (офлайн-режим/сидер).
type TicketComment struct {
	ID                string     `json:"id" gorm:"primaryKey;type:text"`
	TicketID          string     `json:"ticket_id" gorm:"type:text;index;not null"`
	ServiceDeskUUID   string     `json:"service_desk_uuid" gorm:"type:text;index"`
	Text              string     `json:"text" gorm:"type:text"`
	AuthorName        string     `json:"author_name" gorm:"type:varchar(255)"`
	AuthorUserID      *uint      `json:"author_user_id,omitempty" gorm:"index"`
	Source            string     `json:"source" gorm:"type:varchar(50);not null;default:'ui';index"`
	CreationDate      time.Time  `json:"creation_date" gorm:"index"`
	IsInternal        bool       `json:"is_internal"`
	IsPrivate         bool       `json:"is_private" gorm:"not null;default:false;index"`
	ReplyToClient     bool       `json:"reply_to_client" gorm:"not null;default:false;index"`
	DeletedInBitrix   bool       `json:"deleted_in_bitrix" gorm:"not null;default:false;index"`
	DeletedInBitrixAt *time.Time `json:"deleted_in_bitrix_at"`
}

// LastCommentInfo содержит данные о последнем комментарии.
type LastCommentInfo struct {
	Text       string `json:"text"`
	AuthorName string `json:"author_name"`
	IsPrivate  bool   `json:"is_private"`
}

type ConnectionCopyStat struct {
	EntityType   string     `json:"entity_type"`
	EntityID     string     `json:"entity_id"`
	CopyCount    int        `json:"copy_count"`
	LastCopiedAt *time.Time `json:"last_copied_at,omitempty"`
}

func (c *TicketComment) BeforeCreate(tx *gorm.DB) (err error) {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	if c.Source == "" {
		c.Source = CommentSourceUI
	}
	return
}
