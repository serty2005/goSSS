package company

import (
	"etalon-server/internal/domain/common"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Company представляет сущность компании (Domain Entity).
type Company struct {
	common.Base
	Address          *string    `gorm:"type:text"`
	Title            *string    `gorm:"type:text"`
	ActiveContract   *bool      `gorm:"type:boolean"`
	OwnerMode        string     `gorm:"type:varchar(32);default:'normal';index" json:"owner_mode"`
	LastModifiedDate *time.Time `json:"last_modified_date"`
	AdditionalName   *string    `gorm:"type:text"`
	ParentID         *string    `gorm:"type:text"`
	ParentTitle      *string    `json:"parent_title,omitempty" gorm:"->"`
	ContractID       *string    `json:"contract_id,omitempty" gorm:"->"`
	ContractType     *string    `json:"contract_type,omitempty" gorm:"->"`
	Parent           *Company   `gorm:"foreignKey:ParentID"`

	// Связи "один-ко-многим" и "многие-ко-многим" мы здесь НЕ объявляем явно,
	// если они требуют импорта `internal/domain/models`, чтобы избежать цикла.
	// GORM позволяет работать с ними через foreign keys или preload, если модель зарегистрирована.
}

func (c *Company) BeforeCreate(tx *gorm.DB) (err error) {
	if strings.TrimSpace(c.OwnerMode) == "" {
		c.OwnerMode = "normal"
	}
	return c.Base.BeforeCreate(tx)
}

type BitrixMappingRow struct {
	Company                  Company
	BitrixServicePointID     *int64
	BitrixServicePointName   *string
	BitrixServicePointCode   *string
	BitrixServicePointStatus *bool
}

type ParentCompanyOption struct {
	ID            string
	Title         string
	ChildrenCount int64
}
