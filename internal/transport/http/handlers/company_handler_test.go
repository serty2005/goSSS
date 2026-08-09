package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"etalon-server/internal/domain/company"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/go-chi/chi/v5"
)

func TestCompanyHandlerRegisterRoutes_ListsParentsBeforeCompanyIDRoute(t *testing.T) {
	t.Parallel()

	handler := NewCompanyHandler(companyHandlerServiceStub{})
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/companies/parents", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался статус %d, получен %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "parent-1") {
		t.Fatalf("ожидался список родительских компаний, получено тело: %s", rec.Body.String())
	}
}

type companyHandlerServiceStub struct{}

func (companyHandlerServiceStub) CreateCompany(context.Context, *api.CompanyCreateDTO) (*company.Company, error) {
	return nil, nil
}

func (companyHandlerServiceStub) UpdateCompany(context.Context, string, map[string]interface{}) error {
	return nil
}

func (companyHandlerServiceStub) DeleteCompany(context.Context, string) error {
	return nil
}

func (companyHandlerServiceStub) GetCompany(context.Context, string) (*company.Company, error) {
	return nil, nil
}

func (companyHandlerServiceStub) SearchCompanies(context.Context, string, int, int, []string) ([]company.Company, int64, error) {
	return nil, 0, nil
}

func (companyHandlerServiceStub) ListParents(context.Context, string, int) ([]company.ParentCompanyOption, error) {
	return []company.ParentCompanyOption{
		{ID: "parent-1", Title: "Родительская компания", ChildrenCount: 3},
	}, nil
}

func (companyHandlerServiceStub) GetInfrastructure(context.Context, string, bool) ([]api.FoundEntityDTO, error) {
	return nil, nil
}

func (companyHandlerServiceStub) GetChildren(context.Context, string) ([]company.Company, error) {
	return nil, nil
}

func (companyHandlerServiceStub) ListBitrixMappings(context.Context, string, int, int, []string) ([]company.BitrixMappingRow, error) {
	return nil, nil
}

func (companyHandlerServiceStub) GetBitrixMappingByCompanyID(context.Context, string) (*company.BitrixMappingRow, error) {
	return nil, nil
}

func (companyHandlerServiceStub) UpdateBitrixMapping(context.Context, *string, *int64) error {
	return nil
}

func (companyHandlerServiceStub) SyncBitrixContract(context.Context, string) error {
	return nil
}
