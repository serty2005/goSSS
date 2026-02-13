package user

import (
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Role представляет роль пользователя в системе (RBAC).
type Role struct {
	ID          uint   `gorm:"primarykey"`
	Name        string `gorm:"type:varchar(50);uniqueIndex;not null"` // admin, operator, engineer
	Description string `gorm:"type:text"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// User представляет пользователя системы (сотрудника или внешнего клиента).
type User struct {
	ID            uint           `gorm:"primarykey"`
	Username      string         `gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash  string         `gorm:"type:text;not null"`
	FullName      string         `gorm:"type:text"`
	FirstName     string         `gorm:"type:varchar(100);not null;default:''"`
	LastName      string         `gorm:"type:varchar(100);not null;default:''"`
	Position      string         `gorm:"type:varchar(50);not null;default:'intern'"`
	ExternalID    *string        `gorm:"type:varchar(128)"`
	ExternalType  *string        `gorm:"type:varchar(50)"`
	ScheduleType  string         `gorm:"type:varchar(10);not null;default:'5/2'"`
	HasLoggedIn   bool           `gorm:"not null;default:false"`
	ProfileConfig datatypes.JSON `gorm:"type:jsonb;default:'{}'"`

	// Новые поля
	Email      *string `gorm:"type:varchar(255);uniqueIndex"`
	Phone      *string `gorm:"type:varchar(50)"`
	Department string  `gorm:"type:varchar(100)"`
	IsActive   bool    `gorm:"default:true;index"`

	// RBAC: Связь многие-ко-многим
	Roles        []Role        `gorm:"many2many:user_roles;"`
	Integrations []Integration `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE;"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// HashPassword хеширует пароль пользователя.
func (u *User) HashPassword(password string) error {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	if err != nil {
		return err
	}
	u.PasswordHash = string(bytes)
	return nil
}

// CheckPassword проверяет совпадение пароля с хешем.
func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}

type Integration struct {
	ID              uint   `gorm:"primarykey"`
	UserID          uint   `gorm:"index;not null"`
	IntegrationType string `gorm:"type:varchar(50);not null;index"`
	ExternalID      string `gorm:"type:varchar(255);not null"`
	IsVerified      bool   `gorm:"not null;default:false;index"`
	IsLocked        bool   `gorm:"not null;default:false;index"`
	VerifiedName    string `gorm:"type:varchar(255)"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (Integration) TableName() string {
	return "user_integrations"
}
