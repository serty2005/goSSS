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

	// GetInfrastructure возвращает список всего активного оборудования компании.
	GetInfrastructure(ctx context.Context, companyID string) ([]api.FoundEntityDTO, error)

	// GetChildren возвращает список дочерних компаний для указанной hub-компании.
	GetChildren(ctx context.Context, companyID string) ([]Company, error)

	ListBitrixMappings(ctx context.Context, term string, limit, offset int) ([]BitrixMappingRow, error)
	UpdateBitrixMapping(ctx context.Context, companyID *string, bitrixServicePointID *int64) error
}
