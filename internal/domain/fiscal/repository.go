package fiscal

import (
	"context"
	"time"

	"gorm.io/gorm"
)

type ListFilter struct {
	Limit        int
	Offset       int
	SearchQuery  string
	CompanyIDs   []string
	Models       []string
	FNExpireFrom *time.Time
	FNExpireTo   *time.Time
	FNExpireMin  *time.Time
	SortBy       string
	SortOrder    string
}

// Repository определяет интерфейс для работы с хранилищем фискальных регистраторов.
type Repository interface {
	Create(ctx context.Context, tx *gorm.DB, fr *FiscalRegister) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)

	GetByID(ctx context.Context, internalID string) (*FiscalRegister, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*FiscalRegister, error)

	GetAllIDsAndDates(ctx context.Context) (map[string]*FiscalRegister, error)
	List(ctx context.Context, filter ListFilter) ([]FiscalRegister, int64, error)
	ListModels(ctx context.Context) ([]string, error)
	Search(ctx context.Context, term string, limit, offset int) ([]FiscalRegister, error)
	SearchWithTotal(ctx context.Context, term string, limit, offset int) ([]FiscalRegister, int64, error)

	FindBySerialNumber(ctx context.Context, sn string) (*FiscalRegister, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]FiscalRegister, error)
	SetOwnerWithBinding(ctx context.Context, tx *gorm.DB, internalID string, ownerID string, bindingMode string) (bool, error)

	// Методы массовой блокировки
	LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
	UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
}
