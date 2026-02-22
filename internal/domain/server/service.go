package server

import (
	"context"
	api "etalon-server/internal/transport/http/dtos"
)

type Service interface {
	Create(ctx context.Context, dto *api.ServerCreateDTO) (*Server, error)
	Update(ctx context.Context, id string, data map[string]interface{}) error
	Delete(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*Server, error)
	List(ctx context.Context, limit, offset int, companyIDs []string) ([]Server, int64, error)
	Search(ctx context.Context, term string, limit, offset int, companyIDs []string) ([]Server, int64, error)
}
