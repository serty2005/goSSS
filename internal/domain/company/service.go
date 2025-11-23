package company

import (
	"context"
	api "etalon-server/internal/transport/http/dtos"
)

// Service определяет бизнес-логику для работы с компаниями.
type Service interface {
	CreateCompany(ctx context.Context, dto *api.CompanyCreateDTO) (*Company, error)
	UpdateCompany(ctx context.Context, id string, data map[string]interface{}) error
	DeleteCompany(ctx context.Context, id string) error
	GetCompany(ctx context.Context, id string) (*Company, error)
	SearchCompanies(ctx context.Context, term string, limit, offset int) ([]Company, error)
}
