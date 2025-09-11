// internal/models/models.go
package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ExternalSystemLink хранит связь между нашей внутренней сущностью и ее ID во внешней системе.
type ExternalSystemLink struct {
	InternalID      string `gorm:"primaryKey;type:text"`        // UUID нашей сущности (из поля Base.ID)
	SystemName      string `gorm:"primaryKey;type:varchar(50)"` // Название внешней системы, например, "servicedesk", "zabbix"
	ServiceDeskUUID string `gorm:"type:text;unique;not null"`   // ID сущности во внешней системе ServiceDesk
	EntityType      string `gorm:"type:varchar(50);not null"`   // Тип нашей сущности: "Company", "Server" и т.д.
	LastSyncedAt    time.Time
}

// Константы для статусов агента
const (
	StatusPendingOwner       = "pending_owner"
	StatusPendingZabbix      = "pending_zabbix_registration"
	StatusActive             = "active"
	StatusRegistrationFailed = "registration_failed"
)

// Base содержит общие поля для всех моделей.
type Base struct {
	ID            string `gorm:"primaryKey;type:text"`
	MetaClass     string `gorm:"type:text"`
	LastUpdatedBy string `gorm:"type:varchar(50);default:'unknown'"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     gorm.DeletedAt `gorm:"index"`
}

// BeforeCreate будет вызван GORM перед созданием записи.
func (base *Base) BeforeCreate(tx *gorm.DB) (err error) {
	if base.ID == "" {
		base.ID = uuid.New().String()
	}
	return
}

// Company представляет сущность компании.
type Company struct {
	Base
	Address          *string    `gorm:"type:text"`
	Title            *string    `gorm:"type:text"`
	ActiveContract   *bool      `gorm:"type:boolean"`
	LastModifiedDate *time.Time `json:"last_modified_date"`
	AdditionalName   *string    `gorm:"type:text"`
	ParentID         *string    `gorm:"type:text"`
	Parent           *Company   `gorm:"foreignKey:ParentID"`

	Contracts       []Contract       `gorm:"many2many:company_contracts;"`
	Servers         []Server         `gorm:"foreignKey:OwnerID"`
	Workstations    []Workstation    `gorm:"foreignKey:OwnerID"`
	FiscalRegisters []FiscalRegister `gorm:"foreignKey:OwnerID"`
}

// Contract представляет сущность контракта.
type Contract struct {
	Base
	State            *string `gorm:"type:varchar(50);index"`
	StateStartTime   *time.Time
	Services         datatypes.JSON `gorm:"type:jsonb"`
	Recipients       datatypes.JSON `gorm:"type:jsonb"`
	LastModifiedDate *time.Time     `json:"last_modified_date"`
	ServiceLevel     int            `gorm:"default:-1;index"`
	Companies        []Company      `gorm:"many2many:company_contracts;"`
}

type CompanyContract struct {
	CompanyID  string   `gorm:"primaryKey"`
	Company    Company  `gorm:"foreignKey:CompanyID"`
	ContractID string   `gorm:"primaryKey"`
	Contract   Contract `gorm:"foreignKey:ContractID"`
}

// Server представляет сущность сервера.
type Server struct {
	Base
	UniqueID         *string    `gorm:"type:text"`
	CRMid            *string    `gorm:"column:crm_id;type:text;index"`
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

	AdditionalOwners []Company `gorm:"many2many:server_additional_owners;foreignKey:ID;joinForeignKey:ServerID;references:ID;joinReferences:CompanyID"`

	ServerName       *string        `gorm:"type:text"`
	ServerEdition    *string        `gorm:"type:varchar(50)"`
	LastPolledAt     *time.Time     `gorm:"column:last_polled_at"`
	Status           string         `gorm:"type:varchar(50);default:'unknown';index"`
	StatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails    datatypes.JSON `gorm:"type:jsonb"`
}

// Workstation представляет сущность рабочей станции.
type Workstation struct {
	Base
	Teamviewer       *string        `gorm:"type:text"`
	Anydesk          *string        `gorm:"type:text"`
	Litemanager      *string        `gorm:"type:text"`
	DeviceName       *string        `gorm:"type:text"`
	LastModifiedDate *time.Time     `json:"last_modified_date"`
	Description      *string        `gorm:"type:text"`
	Status           *string        `gorm:"type:varchar(50);default:'offline'"`
	StatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails    datatypes.JSON `gorm:"type:jsonb"`
	OwnerID          *string        `gorm:"type:text;index"`
}

// FiscalRegister представляет сущность фискального регистратора.
type FiscalRegister struct {
	Base
	ModelKKT         *string        `gorm:"type:text"`
	FFD              *string        `gorm:"type:text"`
	RNKKT            *string        `gorm:"column:rn_kkt;type:text;index"`
	LegalName        *string        `gorm:"type:text"`
	INN              *string        `gorm:"column:inn;type:text;index"`
	FRSerialNumber   *string        `gorm:"type:text;index"`
	FNNumber         *string        `gorm:"type:text"`
	KKTRegDate       *time.Time     `json:"kkt_reg_date"`
	FNExpireDate     *time.Time     `json:"fn_expire_date"`
	LastModifiedDate *time.Time     `json:"last_modified_date"`
	FRDownloader     *string        `gorm:"type:varchar(100)"`
	FRFirmware       *string        `gorm:"type:text"`
	DriverVersion    *string        `gorm:"type:varchar(50)"`
	Status           *string        `gorm:"type:varchar(50);default:'offline'"`
	StatusBeforeLock *string        `gorm:"type:varchar(50)"`
	StatusDetails    datatypes.JSON `gorm:"type:jsonb"`
	OwnerID          *string        `gorm:"type:text;index"`
	Licenses         datatypes.JSON `gorm:"type:jsonb"`
}

// EquipmentStatusLog хранит историю изменений статусов оборудования.
type EquipmentStatusLog struct {
	ID         uint           `gorm:"primarykey"`
	EntityType string         `gorm:"type:varchar(50);index"`
	EntityID   string         `gorm:"type:text;index"`
	OldStatus  string         `gorm:"type:varchar(50)"`
	NewStatus  string         `gorm:"type:varchar(50)"`
	Details    datatypes.JSON `gorm:"type:jsonb"`
	ChangedBy  string         `gorm:"type:varchar(50)"`
	Timestamp  time.Time      `gorm:"index"`
}

// Agent представляет экземпляр агента, установленного на машине клиента.
type Agent struct {
	UUID           string         `gorm:"primaryKey;type:text"`
	Type           string         `gorm:"type:varchar(50);not null"`
	OwnerID        string         `gorm:"type:text;index"`
	Config         datatypes.JSON `gorm:"type:jsonb"`
	LastHeartbeat  time.Time
	Version        string `gorm:"type:varchar(50)"`
	Hostname       string `gorm:"type:text"`
	ZabbixHostname string `gorm:"type:text"`
	Status         string `gorm:"type:varchar(50);index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AgentFile struct {
	FileName              string    `gorm:"primaryKey;type:text"`
	LastProcessedModTime  time.Time `gorm:"not null"`
	LastProcessedFileSize int64     `gorm:"not null"`
	LastSeenFRSerial      *string   `gorm:"type:text;index"`
	LastSeenRMSUrl        *string   `gorm:"type:text"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ReconciliationTask struct {
	ID         uint           `gorm:"primarykey"`
	TaskType   string         `gorm:"type:varchar(50);not null;index"`
	EntityType string         `gorm:"type:varchar(50)"`
	EntityUUID string         `gorm:"type:text"`
	Details    datatypes.JSON `gorm:"type:jsonb"`
	Status     string         `gorm:"type:varchar(50);default:'new';index"`
	Comment    string         `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type User struct {
	ID           uint           `gorm:"primarykey"`
	Username     string         `gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash string         `gorm:"type:text;not null"`
	FullName     string         `gorm:"type:text"`
	Roles        datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (u *User) HashPassword(password string) error {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	u.PasswordHash = string(bytes)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}
