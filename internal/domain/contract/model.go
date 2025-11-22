package contract

import (
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"time"

	"gorm.io/datatypes"
)

// Contract представляет сущность контракта (ServiceDesk Agreement).
type Contract struct {
	common.Base
	State            *string `gorm:"type:varchar(50);index"`
	StateStartTime   *time.Time
	Services         datatypes.JSON `gorm:"type:jsonb"` // Список названий или UUID услуг
	Recipients       datatypes.JSON `gorm:"type:jsonb"` // Список UUID получателей услуг
	LastModifiedDate *time.Time     `json:"last_modified_date"`
	ServiceLevel     int            `gorm:"default:-1;index"`

	// Связь Many-to-Many с компаниями
	Companies []company.Company `gorm:"many2many:company_contracts;"`
}
