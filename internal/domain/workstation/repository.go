package workstation

import (
	"context"

	"gorm.io/gorm"
)

// Repository определяет интерфейс для работы с хранилищем рабочих станций.
type Repository interface {
	Create(ctx context.Context, tx *gorm.DB, workstation *Workstation) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)

	GetByID(ctx context.Context, internalID string) (*Workstation, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*Workstation, error)

	GetAllIDsAndDates(ctx context.Context) (map[string]*Workstation, error)
	Search(ctx context.Context, term string, limit, offset int) ([]Workstation, error)

	FindByRemoteIDs(ctx context.Context, tv, ad, lm string) (*Workstation, error)
	// FindAllByRemoteIDs ищет ВСЕ рабочие станции, совпадающие по идентификаторам (для поиска дублей).
	FindAllByRemoteIDs(ctx context.Context, tv, lm string) ([]Workstation, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]Workstation, error)
	SetOwnerWithBinding(ctx context.Context, tx *gorm.DB, internalID string, ownerID string, bindingMode string) (bool, error)

	// Методы массовой блокировки
	LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
	UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
}
