package contract

import (
	"context"
	api "etalon-server/internal/transport/http/dtos"
)

// Service определяет бизнес-логику для работы с контрактами.
type Service interface {
	// SyncContracts выполняет полную синхронизацию контрактов из внешней системы.
	// Включает upsert контрактов, обновление связей и пересчет статусов компаний/оборудования.
	SyncContracts(ctx context.Context, rawData []map[string]interface{}) error
	CreateContract(ctx context.Context, dto *api.ContractCreateDTO) (*Contract, error)
	UpdateContract(ctx context.Context, id string, data map[string]interface{}) error
	DeleteContract(ctx context.Context, id string) error
	GetContract(ctx context.Context, id string) (*Contract, error)
}
