package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Base содержит общие поля для всех моделей.
type Base struct {
	ID              string  `gorm:"primaryKey;type:text"`
	MetaClass       string  `gorm:"type:text"`
	ServiceDeskUUID *string `gorm:"type:text;uniqueIndex"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate будет вызван GORM перед созданием записи.
// Он генерирует новый UUID для поля ID.
func (base *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if base.ID == "" {
		base.ID = uuid.New().String()
	}
	return
}

// Company представляет сущность компании.
type Company struct {
	Base
	Address               *string    `gorm:"type:text"`
	Title                 *string    `gorm:"type:text"`
	ActiveContract        *bool      `gorm:"type:boolean"`
	LastModifiedDate      *time.Time `json:"last_modified_date"`
	AdditionalName        *string    `gorm:"type:text"`
	ParentServiceDeskUUID *string    `gorm:"type:text"`

	Servers         []Server         `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	Workstations    []Workstation    `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	FiscalRegisters []FiscalRegister `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
}

// Server представляет сущность сервера.
type Server struct {
	Base
	UniqueID             *string    `gorm:"type:text"`
	Teamviewer           *string    `gorm:"type:text"`
	RDP                  *string    `gorm:"type:text"`
	Anydesk              *string    `gorm:"type:text"`
	IP                   *string    `gorm:"type:text"`
	CabinetLink          *string    `gorm:"type:text"`
	DeviceName           *string    `gorm:"type:text"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	Litemanager          *string    `gorm:"type:text"`
	IikoVersion          *string    `gorm:"type:text"`
	Description          *string    `gorm:"type:text"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID
}

// Workstation представляет сущность рабочей станции.
type Workstation struct {
	Base
	Teamviewer           *string    `gorm:"type:text"`
	Anydesk              *string    `gorm:"type:text"`
	Litemanager          *string    `gorm:"type:text"`
	DeviceName           *string    `gorm:"type:text"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	Description          *string    `gorm:"type:text"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID
}

// FiscalRegister представляет сущность фискального регистратора.
type FiscalRegister struct {
	Base
	ModelKKT             *string    `gorm:"type:text"`
	FFD                  *string    `gorm:"type:text"`
	FRDownloader         *string    `gorm:"type:text"`
	RNKKT                *string    `gorm:"column:rn_kkt;type:text"` // ИСПРАВЛЕНИЕ: Явно указываем имя столбца
	LegalName            *string    `gorm:"type:text"`
	FRSerialNumber       *string    `gorm:"type:text"`
	FNNumber             *string    `gorm:"type:text"`
	KKTRegDate           *time.Time `json:"kkt_reg_date"`
	FNExpireDate         *time.Time `json:"fn_expire_date"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"` // Ссылка на Company.UUID
}

// Agent представляет экземпляр агента, установленного на машине клиента.
type Agent struct {
	UUID                 string         `gorm:"primaryKey;type:text"`      // UUID, который генерирует сам агент
	Type                 string         `gorm:"type:varchar(50);not null"` // 'workstation' или 'server'
	OwnerServiceDeskUUID string         `gorm:"type:text;index"`           // UUID сущности (Workstation или Server), к которой он привязан
	Config               datatypes.JSON `gorm:"type:jsonb"`                // Конфигурация агента в формате JSON
	LastHeartbeat        time.Time      // Время последнего heartbeat'а (будет обновляться)
	Version              string         `gorm:"type:varchar(50)"` // Версия бинарного файла агента
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// AgentFile хранит информацию о последней обработке файла с FTP.
type AgentFile struct {
	FileName              string    `gorm:"primaryKey;type:text"`
	LastProcessedModTime  time.Time `gorm:"not null"`
	LastProcessedFileSize int64     `gorm:"not null"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ReconciliationTask представляет задачу для ручного разбора администратором.
type ReconciliationTask struct {
	ID         uint           `gorm:"primarykey"`
	TaskType   string         `gorm:"type:varchar(50);not null;index"`      // 'owner_mismatch', 'new_client', 'delete_duplicate', 'delete_from_servicedesk'
	EntityType string         `gorm:"type:varchar(50)"`                     // 'FiscalRegister', 'Workstation', 'Server'
	EntityUUID string         `gorm:"type:text"`                            // UUID сущности, с которой связана задача
	Details    datatypes.JSON `gorm:"type:jsonb"`                           // Детали задачи, например, старый и новый владелец
	Status     string         `gorm:"type:varchar(50);default:'new';index"` // 'new', 'resolved'
	Comment    string         `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
