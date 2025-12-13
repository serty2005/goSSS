package user

import (
	"time"

	"golang.org/x/crypto/bcrypt"
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
	ID           uint   `gorm:"primarykey"`
	Username     string `gorm:"type:varchar(100);uniqueIndex;not null"`
	PasswordHash string `gorm:"type:text;not null"`
	FullName     string `gorm:"type:text"`

	// Новые поля
	Email      *string `gorm:"type:varchar(255);uniqueIndex"`
	Phone      *string `gorm:"type:varchar(50)"`
	Department string  `gorm:"type:varchar(100)"`
	IsActive   bool    `gorm:"default:true;index"`

	// RBAC: Связь многие-ко-многим
	Roles []Role `gorm:"many2many:user_roles;"`

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
