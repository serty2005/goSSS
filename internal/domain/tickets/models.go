package tickets

import (
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/user"
	"time"
)

// Жесткий Workflow статусов.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusPending    = "pending"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
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
)

// Типы активов для полиморфной связи.
const (
	AssetTypeServer         = "Server"
	AssetTypeFiscalRegister = "FiscalRegister"
	AssetTypeWorkstation    = "Workstation"
)

// Ticket представляет собой заявку ServiceDesk.
// Теперь это полноценная сущность системы, а не просто кэш из Naumen.
type Ticket struct {
	common.Base

	// Идентификация и Основные данные
	Number      int    `json:"number" gorm:"uniqueIndex;autoIncrement"` // Внутренний человеко-читаемый номер
	Subject     string `json:"subject" gorm:"type:text;not null"`
	Description string `json:"description" gorm:"type:text"` // HTML/Markdown описание

	// Workflow и SLA
	Status     string     `json:"status" gorm:"type:varchar(50);default:'new';index"`
	Priority   string     `json:"priority" gorm:"type:varchar(20);default:'medium'"`
	Type       string     `json:"type" gorm:"type:varchar(50);default:'incident'"`
	DeadlineAt *time.Time `json:"deadline_at" gorm:"index"`

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
	CompanyName string  `json:"company_name" gorm:"->"`
	ContractID  *string `json:"contract_id,omitempty" gorm:"type:text"`

	// Полиморфная связь с оборудованием
	AssetID   *string `json:"asset_id,omitempty" gorm:"type:text;index"`
	AssetType *string `json:"asset_type,omitempty" gorm:"type:varchar(50)"`

	// Внешние системы (для обратной совместимости и миграции)
	ServiceDeskUUID string `json:"service_desk_uuid" gorm:"index"`
}

// TicketDetails — составная структура для отображения на UI.
type TicketDetails struct {
	Metadata    Ticket          `json:"metadata"`
	CompanyName string          `json:"company_name"`
	History     []TicketHistory `json:"history"`
	Attachments []Attachment    `json:"attachments"`
	Comments    []Comment       `json:"comments"` // Оставляем пока для совместимости с легаси комментариями
}

// Comment представляет легаси комментарий (планируется к замене на History).
type Comment struct {
	UUID         string    `json:"uuid"`
	Text         string    `json:"text"`
	AuthorName   string    `json:"author_name"`
	CreationDate time.Time `json:"creation_date"`
	IsInternal   bool      `json:"is_internal"`
}
