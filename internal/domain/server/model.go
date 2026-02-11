package server

import (
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"time"

	"gorm.io/datatypes"
)

// Server представляет сущность сервера.
type Server struct {
	common.Base
	UniqueID         *string    `gorm:"type:text"`
	CRMid            *string    `gorm:"column:crm_id;type:text;index"`
	ServerKey        *string    `gorm:"column:server_key;type:text;index"`
	Teamviewer       *string    `gorm:"type:text"`
	RDP              *string    `gorm:"type:text"`
	Anydesk          *string    `gorm:"type:text"`
	IP               *string    `gorm:"type:text"`
	CabinetLink      *string    `gorm:"type:text"`
	DeviceName       *string    `gorm:"type:text;index"`
	LastModifiedDate *time.Time `json:"last_modified_date"`
	Litemanager      *string    `gorm:"type:text"`
	ServerVersion    *string    `gorm:"type:text"`
	Description      *string    `gorm:"type:text"`
	OwnerID          *string    `gorm:"type:text;index"`

	// Связь Many-to-Many с Company
	AdditionalOwners []company.Company `gorm:"many2many:server_additional_owners;foreignKey:ID;joinForeignKey:ServerID;references:ID;joinReferences:CompanyID"`

	ServerName             *string        `gorm:"type:text"`
	ServerEdition          *string        `gorm:"type:varchar(50)"`
	LastPolledAt           *time.Time     `gorm:"column:last_polled_at"`
	Status                 string         `gorm:"type:varchar(50);default:'unknown';index"` // Операционный статус (active, offline)
	StatusBeforeLock       *string        `gorm:"type:varchar(50)"`
	HealthStatus           string         `gorm:"type:varchar(50);default:'ok';index"` // Статус состояния (ok, attention_required)
	HealthStatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails          datatypes.JSON `gorm:"type:jsonb"`
}
