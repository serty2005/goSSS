package workstation

import (
	"etalon-server/internal/domain/common"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Workstation описывает рабочую станцию в инфраструктуре компании.
type Workstation struct {
	common.Base
	IdentityHash           *string        `gorm:"column:identity_hash;type:text;index"`
	Teamviewer             *string        `gorm:"type:text"`
	Anydesk                *string        `gorm:"type:text"`
	Litemanager            *string        `gorm:"type:text"`
	DeviceName             *string        `gorm:"type:text"`
	ServerID               *string        `gorm:"column:server_id;type:text;index"`
	IsNew                  bool           `gorm:"column:is_new;default:false;index" json:"is_new"`
	LastModifiedDate       *time.Time     `json:"last_modified_date"`
	Description            *string        `gorm:"type:text"`
	HealthStatus           string         `gorm:"type:varchar(50);default:'ok';index"`
	HealthStatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails          datatypes.JSON `gorm:"type:jsonb"`
	OwnerID                *string        `gorm:"type:text;index"`
}

func (w *Workstation) BeforeCreate(tx *gorm.DB) (err error) {
	return w.Base.BeforeCreate(tx)
}
