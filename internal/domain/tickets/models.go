package tickets

import (
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/user"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Р–РµСЃС‚РєРёР№ Workflow СЃС‚Р°С‚СѓСЃРѕРІ.
const (
	StatusNew        = "new"
	StatusInProgress = "in_progress"
	StatusPending    = "pending"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

// РџСЂРёРѕСЂРёС‚РµС‚С‹.
const (
	PriorityCritical = "critical"
	PriorityHigh     = "high"
	PriorityMedium   = "medium"
	PriorityLow      = "low"
)

// РўРёРїС‹ Р·Р°СЏРІРѕРє.
const (
	TypeIncident       = "incident"
	TypeServiceRequest = "service_request"
)

// РўРёРїС‹ Р°РєС‚РёРІРѕРІ РґР»СЏ РїРѕР»РёРјРѕСЂС„РЅРѕР№ СЃРІСЏР·Рё.
const (
	AssetTypeServer         = "Server"
	AssetTypeFiscalRegister = "FiscalRegister"
	AssetTypeWorkstation    = "Workstation"
)

// Ticket РїСЂРµРґСЃС‚Р°РІР»СЏРµС‚ СЃРѕР±РѕР№ Р·Р°СЏРІРєСѓ ServiceDesk.
// РўРµРїРµСЂСЊ СЌС‚Рѕ РїРѕР»РЅРѕС†РµРЅРЅР°СЏ СЃСѓС‰РЅРѕСЃС‚СЊ СЃРёСЃС‚РµРјС‹, Р° РЅРµ РїСЂРѕСЃС‚Рѕ РєСЌС€ РёР· Naumen.
type Ticket struct {
	common.Base

	// РРґРµРЅС‚РёС„РёРєР°С†РёСЏ Рё РћСЃРЅРѕРІРЅС‹Рµ РґР°РЅРЅС‹Рµ
	Number      int    `json:"number" gorm:"uniqueIndex;autoIncrement"` // Р’РЅСѓС‚СЂРµРЅРЅРёР№ С‡РµР»РѕРІРµРєРѕ-С‡РёС‚Р°РµРјС‹Р№ РЅРѕРјРµСЂ
	Subject     string `json:"subject" gorm:"type:text;not null"`
	Description string `json:"description" gorm:"type:text"` // HTML/Markdown РѕРїРёСЃР°РЅРёРµ

	// Workflow Рё SLA
	Status     string     `json:"status" gorm:"type:varchar(50);default:'new';index"`
	Priority   string     `json:"priority" gorm:"type:varchar(20);default:'medium'"`
	Type       string     `json:"type" gorm:"type:varchar(50);default:'incident'"`
	DeadlineAt *time.Time `json:"deadline_at" gorm:"index"`

	// РЎРІСЏР·Рё СЃ РџРѕР»СЊР·РѕРІР°С‚РµР»СЏРјРё
	AssigneeID *uint      `json:"assignee_id" gorm:"index"`
	Assignee   *user.User `json:"assignee" gorm:"foreignKey:AssigneeID"`

	ReporterID *uint      `json:"reporter_id" gorm:"index"` // Р•СЃР»Рё Р·Р°СЏРІРєСѓ Р·Р°РІРµР» Р·Р°СЂРµРіРёСЃС‚СЂРёСЂРѕРІР°РЅРЅС‹Р№ РїРѕР»СЊР·РѕРІР°С‚РµР»СЊ
	Reporter   *user.User `json:"reporter" gorm:"foreignKey:ReporterID"`
	// Р”Р»СЏ РІРЅРµС€РЅРёС… Р·Р°СЏРІРѕРє (email/С‚РµР»РµС„РѕРЅ), РµСЃР»Рё ReporterID nil
	ReporterName  string `json:"reporter_name" gorm:"type:varchar(255)"`
	ReporterEmail string `json:"reporter_email" gorm:"type:varchar(255)"`

	// РЎРІСЏР·Рё СЃ CMDB
	CompanyID string `json:"company_id" gorm:"type:text;index"`
	// Read-only РїРѕР»Рµ РґР»СЏ JOIN СЃ С‚Р°Р±Р»РёС†РµР№ РєРѕРјРїР°РЅРёР№.
	// `->` РѕР·РЅР°С‡Р°РµС‚, С‡С‚Рѕ РїРѕР»Рµ С‚РѕР»СЊРєРѕ РґР»СЏ С‡С‚РµРЅРёСЏ (РЅРµ Р±СѓРґРµС‚ СЃРѕР·РґР°РЅР° РєРѕР»РѕРЅРєР° РІ С‚Р°Р±Р»РёС†Рµ tickets).
	CompanyName string  `json:"company_name,omitempty" gorm:"->"`
	ContractID  *string `json:"contract_id,omitempty" gorm:"type:text"`
	// Признак тикета по общему контракту (вычисляется, не хранится в БД).
	IsCommonContract bool `json:"is_common_contract,omitempty" gorm:"-"`

	// РџРѕР»РёРјРѕСЂС„РЅР°СЏ СЃРІСЏР·СЊ СЃ РѕР±РѕСЂСѓРґРѕРІР°РЅРёРµРј
	AssetID   *string `json:"asset_id,omitempty" gorm:"type:text;index"`
	AssetType *string `json:"asset_type,omitempty" gorm:"type:varchar(50)"`

	// Р’РЅРµС€РЅРёРµ СЃРёСЃС‚РµРјС‹ (РґР»СЏ РѕР±СЂР°С‚РЅРѕР№ СЃРѕРІРјРµСЃС‚РёРјРѕСЃС‚Рё Рё РјРёРіСЂР°С†РёРё)
	ServiceDeskUUID string `json:"service_desk_uuid" gorm:"index"`
}

// TicketDetails вЂ” СЃРѕСЃС‚Р°РІРЅР°СЏ СЃС‚СЂСѓРєС‚СѓСЂР° РґР»СЏ РѕС‚РѕР±СЂР°Р¶РµРЅРёСЏ РЅР° UI.
type TicketDetails struct {
	Metadata    Ticket          `json:"metadata"`
	CompanyName string          `json:"company_name,omitempty"`
	History     []TicketHistory `json:"history"`
	Attachments []Attachment    `json:"attachments"`
	Comments    []Comment       `json:"comments"` // РћСЃС‚Р°РІР»СЏРµРј РїРѕРєР° РґР»СЏ СЃРѕРІРјРµСЃС‚РёРјРѕСЃС‚Рё СЃ Р»РµРіР°СЃРё РєРѕРјРјРµРЅС‚Р°СЂРёСЏРјРё
}

// Comment РїСЂРµРґСЃС‚Р°РІР»СЏРµС‚ Р»РµРіР°СЃРё РєРѕРјРјРµРЅС‚Р°СЂРёР№ (РїР»Р°РЅРёСЂСѓРµС‚СЃСЏ Рє Р·Р°РјРµРЅРµ РЅР° History).
type Comment struct {
	UUID         string    `json:"uuid"`
	Text         string    `json:"text"`
	AuthorName   string    `json:"author_name"`
	CreationDate time.Time `json:"creation_date"`
	IsInternal   bool      `json:"is_internal"`
}

// TicketComment С…СЂР°РЅРёС‚ РєРѕРјРјРµРЅС‚Р°СЂРёРё РІ Р‘Р” (РѕС„Р»Р°Р№РЅ-СЂРµР¶РёРј/СЃРёРґРµСЂ).
type TicketComment struct {
	ID              string    `json:"id" gorm:"primaryKey;type:text"`
	TicketID        string    `json:"ticket_id" gorm:"type:text;index;not null"`
	ServiceDeskUUID string    `json:"service_desk_uuid" gorm:"type:text;index"`
	Text            string    `json:"text" gorm:"type:text"`
	AuthorName      string    `json:"author_name" gorm:"type:varchar(255)"`
	CreationDate    time.Time `json:"creation_date" gorm:"index"`
	IsInternal      bool      `json:"is_internal"`
}

// LastCommentInfo содержит данные о последнем комментарии.
type LastCommentInfo struct {
	Text       string `json:"text"`
	AuthorName string `json:"author_name"`
}

func (c *TicketComment) BeforeCreate(tx *gorm.DB) (err error) {
	if c.ID == "" {
		c.ID = uuid.New().String()
	}
	return
}
