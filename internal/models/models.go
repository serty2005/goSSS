package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Константы для статусов агента
const (
	StatusPendingOwner       = "pending_owner"
	StatusPendingZabbix      = "pending_zabbix_registration"
	StatusActive             = "active"
	StatusRegistrationFailed = "registration_failed"
)

// Base содержит общие поля для всех моделей.
type Base struct {
	ID              string  `gorm:"primaryKey;type:text"`
	MetaClass       string  `gorm:"type:text"`
	ServiceDeskUUID *string `gorm:"type:text;unique"`
	LastUpdatedBy   string  `gorm:"type:varchar(50);default:'unknown'"`
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
	Parent                *Company   `gorm:"foreignKey:ParentServiceDeskUUID;references:ServiceDeskUUID"`

	Contracts       []Contract       `gorm:"many2many:company_contracts;"`
	Servers         []Server         `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	Workstations    []Workstation    `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
	FiscalRegisters []FiscalRegister `gorm:"foreignKey:OwnerServiceDeskUUID;references:ServiceDeskUUID"`
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
	CompanyServiceDeskUUID  string   `gorm:"primaryKey"`
	Company                 Company  `gorm:"foreignKey:CompanyServiceDeskUUID;references:ServiceDeskUUID"`
	ContractServiceDeskUUID string   `gorm:"primaryKey"`
	Contract                Contract `gorm:"foreignKey:ContractServiceDeskUUID;references:ServiceDeskUUID"`
}

// Server представляет сущность сервера.
type Server struct {
	Base
	UniqueID             *string    `gorm:"type:text"`
	CRMid                *string    `gorm:"column:crm_id;type:text;index"`
	Teamviewer           *string    `gorm:"type:text"`
	RDP                  *string    `gorm:"type:text"`
	Anydesk              *string    `gorm:"type:text"`
	IP                   *string    `gorm:"type:text"`
	CabinetLink          *string    `gorm:"type:text"`
	DeviceName           *string    `gorm:"type:text;index"`
	LastModifiedDate     *time.Time `json:"last_modified_date"`
	Litemanager          *string    `gorm:"type:text"`
	ServerVersion        *string    `gorm:"type:text"`
	Description          *string    `gorm:"type:text"`
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"`

	AdditionalOwners []Company `gorm:"many2many:server_additional_owners;foreignKey:ServiceDeskUUID;joinForeignKey:ServerServiceDeskUUID;references:ServiceDeskUUID;joinReferences:CompanyServiceDeskUUID"`

	// Поля для опроса серверов
	ServerName       *string    `gorm:"type:text"`
	ServerEdition    *string    `gorm:"type:varchar(50)"`
	LastPolledAt     *time.Time `gorm:"column:last_polled_at"`
	Status           string     `gorm:"type:varchar(50);default:'unknown';index"` // 'active', 'inactive', 'to_delete', 'offline', 'license', 'starting', 'unknown', 'archived', 'locked'
	StatusBeforeLock *string    `gorm:"type:varchar(50)"`
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
	Status               *string    `gorm:"type:varchar(50);default:'offline'"` // Добавляем 'locked' как возможный статус
	StatusBeforeLock     *string    `gorm:"type:varchar(50)"`                   // Хранит статус до "заморозки"
	OwnerServiceDeskUUID *string    `gorm:"type:text;index"`                    // Ссылка на Company.UUID
}

// FiscalRegister представляет сущность фискального регистратора.
type FiscalRegister struct {
	Base
	ModelKKT             *string        `gorm:"type:text"`
	FFD                  *string        `gorm:"type:text"`
	RNKKT                *string        `gorm:"column:rn_kkt;type:text;index"`
	LegalName            *string        `gorm:"type:text"`
	INN                  *string        `gorm:"column:inn;type:text;index"`
	FRSerialNumber       *string        `gorm:"type:text;index"`
	FNNumber             *string        `gorm:"type:text"`
	KKTRegDate           *time.Time     `json:"kkt_reg_date"`
	FNExpireDate         *time.Time     `json:"fn_expire_date"`
	LastModifiedDate     *time.Time     `json:"last_modified_date"`
	FRDownloader         *string        `gorm:"type:varchar(100)"` // Загрузчик (из bootVersion)
	FRFirmware           *string        `gorm:"type:text"`         // Подписки (из licenses)
	DriverVersion        *string        `gorm:"type:varchar(50)"`
	Status               *string        `gorm:"type:varchar(50);default:'offline'"`
	StatusBeforeLock     *string        `gorm:"type:varchar(50)"`
	OwnerServiceDeskUUID *string        `gorm:"type:text;index"` // Ссылка на Company.UUID
	Licenses             datatypes.JSON `gorm:"type:jsonb"`      // Сырая информация о лицензиях
}

// Agent представляет экземпляр агента, установленного на машине клиента.
type Agent struct {
	UUID                 string         `gorm:"primaryKey;type:text"`      // UUID, который генерирует сам агент
	Type                 string         `gorm:"type:varchar(50);not null"` // 'workstation' или 'server'
	OwnerServiceDeskUUID string         `gorm:"type:text;index"`           // UUID сущности (Workstation или Server), к которой он привязан
	Config               datatypes.JSON `gorm:"type:jsonb"`                // Конфигурация агента в формате JSON
	LastHeartbeat        time.Time      // Время последнего heartbeat'а (будет обновляться)
	Version              string         `gorm:"type:varchar(50)"`       // Версия бинарного файла агента
	Hostname             string         `gorm:"type:text"`              // Имя хоста, переданное агентом
	ZabbixHostname       string         `gorm:"type:text"`              // Имя хоста, сгенерированное для Zabbix
	Status               string         `gorm:"type:varchar(50);index"` // Статус регистрации агента
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// AgentFile хранит информацию о последней обработке файла с FTP.
type AgentFile struct {
	FileName              string    `gorm:"primaryKey;type:text"`
	LastProcessedModTime  time.Time `gorm:"not null"`
	LastProcessedFileSize int64     `gorm:"not null"`
	LastSeenFRSerial      *string   `gorm:"type:text;index"`
	LastSeenRMSUrl        *string   `gorm:"type:text"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// ReconciliationTask представляет задачу для ручного разбора администратором.
type ReconciliationTask struct {
	ID         uint           `gorm:"primarykey"`
	TaskType   string         `gorm:"type:varchar(50);not null;index"`      // 'owner_mismatch', 'new_client', 'delete_duplicate', 'delete_from_servicedesk', 'data_conflict'
	EntityType string         `gorm:"type:varchar(50)"`                     // 'FiscalRegister', 'Workstation', 'Server'
	EntityUUID string         `gorm:"type:text"`                            // UUID сущности, с которой связана задача
	Details    datatypes.JSON `gorm:"type:jsonb"`                           // Детали задачи, например, старый и новый владелец
	Status     string         `gorm:"type:varchar(50);default:'new';index"` // 'new', 'resolved'
	Comment    string         `gorm:"type:text"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// User представляет пользователя системы.
type User struct {
	ID           uint           `gorm:"primarykey"`
	Username     string         `gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash string         `gorm:"type:text;not null"`
	FullName     string         `gorm:"type:text"`
	Roles        datatypes.JSON `gorm:"type:jsonb"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// HashPassword хеширует пароль с использованием bcrypt перед сохранением.
func (u *User) HashPassword(password string) error {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	u.PasswordHash = string(bytes)
	return nil
}

// CheckPassword проверяет, соответствует ли предоставленный пароль хешу.
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}
