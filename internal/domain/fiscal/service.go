package fiscal

import (
	"context"
	api "etalon-server/internal/transport/http/dtos"
)

type Service interface {
	Create(ctx context.Context, dto *api.FiscalRegisterCreateDTO) (*FiscalRegister, error)
	Update(ctx context.Context, id string, data map[string]interface{}) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*FiscalRegister, error)
	List(ctx context.Context, filter ListFilter) ([]FiscalRegister, int64, error)
	ListModels(ctx context.Context) ([]string, error)
	Search(ctx context.Context, term string, limit, offset int) ([]FiscalRegister, int64, error)
}
