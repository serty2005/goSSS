// internal/models/models.go
package models

import (
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
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

type CompanyContract struct {
	CompanyID  string            `gorm:"primaryKey"`
	Company    company.Company   `gorm:"foreignKey:CompanyID"`
	ContractID string            `gorm:"primaryKey"`
	Contract   contract.Contract `gorm:"foreignKey:ContractID"`
}

// EquipmentStatusLog хранит историю изменений статусов оборудования.
type EquipmentStatusLog struct {
	ID              uint           `gorm:"primarykey"`
	EntityType      string         `gorm:"type:varchar(50);index"`
	EntityID        string         `gorm:"type:text;index"`
	OldHealthStatus string         `gorm:"type:varchar(50)"`
	NewHealthStatus string         `gorm:"type:varchar(50)"`
	Details         datatypes.JSON `gorm:"type:jsonb"`
	ChangedBy       string         `gorm:"type:varchar(50)"`
	Timestamp       time.Time      `gorm:"index"`
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
