package company

import (
	"context"
)

// Repository определяет интерфейс хранилища компаний.
type Repository interface {
	Create(ctx context.Context, company *Company) error
	Update(ctx context.Context, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, internalID string) (bool, error)
	GetByID(ctx context.Context, internalID string) (*Company, error)
	GetByIDs(ctx context.Context, internalIDs []string) ([]Company, error)
	GetChildren(ctx context.Context, parentID string) ([]Company, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*Company, error)
	GetAllParentIDs(ctx context.Context, childID string) ([]string, error)
	GetAllIDsAndDates(ctx context.Context) (map[string]*Company, error)
	Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]Company, error)
}
