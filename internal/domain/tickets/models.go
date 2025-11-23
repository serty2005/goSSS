package tickets

import (
	"etalon-server/internal/domain/common"
	"time"
)

// Типы статусов заявки на основе данных из Naumen.
const (
	StatusRegistered       = "registered"
	StatusInProgress       = "inprogress"
	StatusWaitClientAnswer = "waitClientAnswer"
	StatusResolved         = "resolved"
	StatusClosed           = "closed"
)

// Типы активов для полиморфной связи.
const (
	AssetTypeServer         = "Server"
	AssetTypeFiscalRegister = "FiscalRegister"
	AssetTypeWorkstation    = "Workstation"
)

// Ticket представляет собой метаданные заявки, хранящиеся в локальной БД.
type Ticket struct {
	common.Base // Внедряем ID, CreatedAt, UpdatedAt, DeletedAt, MetaClass

	// Данные из ServiceDesk
	ServiceDeskUUID  string    `json:"service_desk_uuid" gorm:"-"` // gorm:"-" означает, что поле не хранится в таблице tickets, а подтягивается через JOIN
	Number           int       `json:"number" gorm:"index"`
	Status           string    `json:"status" gorm:"type:varchar(50);index"`
	Subject          string    `json:"subject" gorm:"type:text"`      // Тема заявки (из title)
	LastComment      string    `json:"last_comment" gorm:"type:text"` // Текст последнего комментария (preview)
	RequestDate      time.Time `json:"request_date"`
	LastModifiedDate time.Time `json:"last_modified_date"`

	// Связи (Внутренние ID)
	CompanyID  string  `json:"company_id" gorm:"type:text;index"`
	ContractID *string `json:"contract_id,omitempty" gorm:"type:text"`

	// Локальная привязка к оборудованию (Полиморфная связь)
	AssetID   *string `json:"asset_id,omitempty" gorm:"type:text;index"`
	AssetType *string `json:"asset_type,omitempty" gorm:"type:varchar(50)"`
}

// TicketDetails — составная структура для отображения полной информации на UI.
type TicketDetails struct {
	Metadata       Ticket    `json:"metadata"`
	DescriptionRTF string    `json:"description_rtf"`
	Comments       []Comment `json:"comments"`
}

// Comment представляет комментарий к заявке.
type Comment struct {
	UUID         string    `json:"uuid"`
	Text         string    `json:"text"`
	AuthorName   string    `json:"author_name"`
	CreationDate time.Time `json:"creation_date"`
	IsInternal   bool      `json:"is_internal"`
}
