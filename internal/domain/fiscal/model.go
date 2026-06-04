package fiscal

import (
	"etalon-server/internal/domain/common"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// FiscalRegister представляет сущность фискального регистратора.
type FiscalRegister struct {
	common.Base
	ModelKKT               *string        `gorm:"type:text"`
	FFD                    *string        `gorm:"type:text"`
	RNKKT                  *string        `gorm:"column:rn_kkt;type:text;index"`
	LegalName              *string        `gorm:"type:text"`
	INN                    *string        `gorm:"column:inn;type:text;index"`
	FRSerialNumber         *string        `gorm:"type:text;index"`
	FRSerialNormalized     *string        `gorm:"column:fr_serial_normalized;type:text;uniqueIndex"`
	FNNumber               *string        `gorm:"type:text"`
	FNExecution            *string        `gorm:"type:text" json:"fn_execution"`
	KKTRegDate             *time.Time     `json:"kkt_reg_date"`
	FNExpireDate           *time.Time     `json:"fn_expire_date"`
	LastModifiedDate       *time.Time     `json:"last_modified_date"`
	FRDownloader           *string        `gorm:"type:varchar(100)"`
	FRFirmware             *string        `gorm:"type:text"`
	DriverVersion          *string        `gorm:"type:varchar(50)"`
	HealthStatus           string         `gorm:"type:varchar(50);default:'ok';index"`
	HealthStatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails          datatypes.JSON `gorm:"type:jsonb"`
	OwnerID                *string        `gorm:"type:text;index"`
	Licenses               datatypes.JSON `gorm:"type:jsonb"`
	Address                *string        `gorm:"type:text" json:"address"`
	AttributeExcise        *bool          `json:"attribute_excise"`
	AttributeMarked        *bool          `json:"attribute_marked"`
	OFDName                *string        `gorm:"type:text" json:"ofd_name"`
	WorkstationID          *string        `gorm:"column:workstation_id;type:text;index"`
	OwnerBindingMode       string         `gorm:"column:owner_binding_mode;type:varchar(16);default:'auto';index" json:"owner_binding_mode"`

	// Поля только для списков заполняются JOIN-ами и не хранятся в таблице fiscal_registers.
	OwnerTitle       *string `gorm:"->;column:owner_title" json:"owner_title,omitempty"`
	OwnerParentID    *string `gorm:"->;column:owner_parent_id" json:"owner_parent_id,omitempty"`
	OwnerParentTitle *string `gorm:"->;column:owner_parent_title" json:"owner_parent_title,omitempty"`
}

func (fr *FiscalRegister) BeforeCreate(tx *gorm.DB) (err error) {
	if strings.TrimSpace(fr.OwnerBindingMode) == "" {
		fr.OwnerBindingMode = "auto"
	}
	return fr.Base.BeforeCreate(tx)
}
