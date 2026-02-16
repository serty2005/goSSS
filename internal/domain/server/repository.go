package server

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// Repository определяет интерфейс для работы с хранилищем серверов.
type Repository interface {
	Create(ctx context.Context, tx *gorm.DB, server *Server) error
	Update(ctx context.Context, tx *gorm.DB, internalID string, updateData map[string]interface{}) (bool, error)
	Delete(ctx context.Context, tx *gorm.DB, internalID string) (bool, error)

	GetByID(ctx context.Context, internalID string) (*Server, error)
	GetByIDUnscoped(ctx context.Context, internalID string) (*Server, error)

	GetAllIDsAndDates(ctx context.Context) (map[string]*Server, error)
	List(ctx context.Context, limit, offset int) ([]Server, int64, error)
	Search(ctx context.Context, term string, limit, offset int) ([]Server, error)
	SearchWithTotal(ctx context.Context, term string, limit, offset int) ([]Server, int64, error)

	FindByCRMidOrIP(ctx context.Context, crmid string, ip string) (*Server, error)
	FindByOwnerIDs(ctx context.Context, ownerIDs []string) ([]Server, error)
	FindForPolling(ctx context.Context, limit int, interval time.Duration) ([]Server, error)

	// Методы массовой блокировки
	LockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
	UnlockByOwner(ctx context.Context, tx *gorm.DB, ownerID string) error
	AddAdditionalOwner(ctx context.Context, serverID, companyID string) error
	RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error
}
