package fiscal

import (
	"context"

	"gorm.io/gorm"
)

// Repository определяет интерфейс для работы с хранилищем фискальных регистраторов.
type Repository interface {
	Create(ctx context.Context, tx *gorm.DB, fr *FiscalRegister) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)

	GetByID(ctx context.Context, internalID string) (*FiscalRegister, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*FiscalRegister, error)

	GetAllIDsAndDates(ctx context.Context) (map[string]*FiscalRegister, error)
	Search(ctx context.Context, term string, limit, offset int) ([]FiscalRegister, error)

	FindBySerialNumber(ctx context.Context, sn string) (*FiscalRegister, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]FiscalRegister, error)

	// Методы массовой блокировки
	LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
	UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
}
