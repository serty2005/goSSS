package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"etalon-server/internal/contextkeys"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/handlers"

	"github.com/go-chi/chi/v5"
)

func TestBitrixModuleRegisterProtectedRoutes_RegistersContractSyncExecute(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
		bitrixHandler: handlers.NewBitrixHandler(nil, nil, nil, nil, nil),
	}

	router := chi.NewRouter()
	module.registerProtectedRoutes(router)

	routeContext := chi.NewRouteContext()
	matched := router.Match(routeContext, "POST", "/bitrix/service-points/contract-sync/execute")
	if !matched {
		t.Fatal("ожидался маршрут POST /bitrix/service-points/contract-sync/execute")
	}
}

func TestBitrixModuleRegisterProtectedRoutes_RegistersContractSyncRefresh(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
		bitrixHandler: handlers.NewBitrixHandler(nil, nil, nil, nil, nil),
	}

	router := chi.NewRouter()
	module.registerProtectedRoutes(router)

	routeContext := chi.NewRouteContext()
	matched := router.Match(routeContext, "POST", "/bitrix/service-points/contract-sync/refresh")
	if !matched {
		t.Fatal("ожидался маршрут POST /bitrix/service-points/contract-sync/refresh")
	}
}

func TestBitrixModuleRegisterCompanyRoutes_AllowsInternToReadMappings(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
	}

	router := chi.NewRouter()
	module.registerCompanyRoutes(router, handlers.NewCompanyHandler(bitrixModuleCompanyServiceStub{}))

	req := httptest.NewRequest(http.MethodGet, "/bitrix-service-point-mappings?company_id=company-1", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.UserRolesContextKey, []string{user.RoleIntern}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusOK, rec.Code)
	}
}

func TestBitrixModuleRegisterCompanyRoutes_StillForbidsSupportSpecialistToUpdateMappings(t *testing.T) {
	t.Parallel()

	module := &bitrixModule{
		cfg: &config.Config{
			EnableBitrixGateway: true,
		},
	}

	router := chi.NewRouter()
	module.registerCompanyRoutes(router, handlers.NewCompanyHandler(bitrixModuleCompanyServiceStub{}))

	req := httptest.NewRequest(http.MethodPut, "/bitrix-service-point-mappings", nil)
	req = req.WithContext(context.WithValue(req.Context(), contextkeys.UserRolesContextKey, []string{user.RoleSupportSpecialist}))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusForbidden, rec.Code)
	}
}

type bitrixModuleCompanyServiceStub struct{}

func (bitrixModuleCompanyServiceStub) CreateCompany(context.Context, *api.CompanyCreateDTO) (*company.Company, error) {
	return nil, nil
}

func (bitrixModuleCompanyServiceStub) UpdateCompany(context.Context, string, map[string]interface{}) error {
	return nil
}

func (bitrixModuleCompanyServiceStub) DeleteCompany(context.Context, string) error {
	return nil
}

func (bitrixModuleCompanyServiceStub) GetCompany(context.Context, string) (*company.Company, error) {
	return nil, nil
}

func (bitrixModuleCompanyServiceStub) SearchCompanies(context.Context, string, int, int) ([]company.Company, int64, error) {
	return nil, 0, nil
}

func (bitrixModuleCompanyServiceStub) GetInfrastructure(context.Context, string) ([]api.FoundEntityDTO, error) {
	return nil, nil
}

func (bitrixModuleCompanyServiceStub) GetChildren(context.Context, string) ([]company.Company, error) {
	return nil, nil
}

func (bitrixModuleCompanyServiceStub) ListBitrixMappings(context.Context, string, int, int) ([]company.BitrixMappingRow, error) {
	return []company.BitrixMappingRow{}, nil
}

func (bitrixModuleCompanyServiceStub) GetBitrixMappingByCompanyID(context.Context, string) (*company.BitrixMappingRow, error) {
	return &company.BitrixMappingRow{}, nil
}

func (bitrixModuleCompanyServiceStub) UpdateBitrixMapping(context.Context, *string, *int64) error {
	return nil
}

func (bitrixModuleCompanyServiceStub) SyncBitrixContract(context.Context, string) error {
	return nil
}
