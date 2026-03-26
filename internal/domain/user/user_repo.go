package user

import (
	"context"
)

// UserRepo определяет интерфейс для работы с хранилищем пользователей.
type Repository interface {
	GetAll(ctx context.Context) ([]User, error)
	GetByID(ctx context.Context, id uint) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetDeletedByUsername(ctx context.Context, username string) (*User, error)
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id uint) error
	Restore(ctx context.Context, user *User) error

	// Методы для ролей
	GetRoleByName(ctx context.Context, name string) (*Role, error)
	EnsureRoleExists(ctx context.Context, name, description string) (*Role, error)
	ReplaceIntegrations(ctx context.Context, userID uint, items []Integration) error
	UpdateExternalFields(ctx context.Context, userID uint, externalType, externalID *string) error
}
