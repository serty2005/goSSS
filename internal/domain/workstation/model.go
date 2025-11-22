package workstation

import (
	"etalon-server/internal/domain/common"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Workstation представляет сущность рабочей станции.
type Workstation struct {
	common.Base
	Teamviewer             *string        `gorm:"type:text"`
	Anydesk                *string        `gorm:"type:text"`
	Litemanager            *string        `gorm:"type:text"`
	DeviceName             *string        `gorm:"type:text"`
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
