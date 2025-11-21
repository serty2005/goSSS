package company

import (
	"etalon-server/internal/domain/common"
	"time"
)

// Company представляет сущность компании (Domain Entity).
type Company struct {
	common.Base
	Address          *string    `gorm:"type:text"`
	Title            *string    `gorm:"type:text"`
	ActiveContract   *bool      `gorm:"type:boolean"`
	LastModifiedDate *time.Time `json:"last_modified_date"`
	AdditionalName   *string    `gorm:"type:text"`
	ParentID         *string    `gorm:"type:text"`
	Parent           *Company   `gorm:"foreignKey:ParentID"`

	// Связи "один-ко-многим" и "многие-ко-многим" мы здесь НЕ объявляем явно,
	// если они требуют импорта `internal/domain/models`, чтобы избежать цикла.
	// GORM позволяет работать с ними через foreign keys или preload, если модель зарегистрирована.
}
