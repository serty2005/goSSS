package handlers

import (
	"context"
	"etalon-server/internal/domain/telephony"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeMegafonVATSSyncService struct {
	refreshCount int
	refreshErr   error
	searchErr    error
	employees    []telephony.ProviderEmployee
	searchResult []telephony.ProviderEmployee
	enabled      bool
}

func (f *fakeMegafonVATSSyncService) IsEnabled() bool {
	return f.enabled
}

func (f *fakeMegafonVATSSyncService) Start(_ context.Context) {}

func (f *fakeMegafonVATSSyncService) RefreshEmployees(_ context.Context) (int, error) {
	return f.refreshCount, f.refreshErr
}

func (f *fakeMegafonVATSSyncService) SyncHistory(_ context.Context) (int, error) {
	return 0, nil
}

func (f *fakeMegafonVATSSyncService) ListCachedEmployees(_ context.Context) ([]telephony.ProviderEmployee, error) {
	return f.employees, nil
}

func (f *fakeMegafonVATSSyncService) SearchEmployeesByName(_ context.Context, _, _, _ string) ([]telephony.ProviderEmployee, error) {
	return f.searchResult, f.searchErr
}

func (f *fakeMegafonVATSSyncService) GetEmployee(_ context.Context, _ string) (*telephony.ProviderEmployee, error) {
	return nil, nil
}

func TestMegafonVATSHandler_RefreshUsers(t *testing.T) {
	h := NewMegafonVATSHandler(&fakeMegafonVATSSyncService{
		refreshCount: 1,
		employees: []telephony.ProviderEmployee{
			{
				Provider:      telephony.ProviderMegafonVATS,
				EmployeeLogin: "admin",
				EmployeeName:  "Иван Иванов",
				LastSeenAt:    time.Now(),
				UpdatedAt:     time.Now(),
			},
		},
		enabled: true,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/megafon-vats/users/refresh", nil)
	rec := httptest.NewRecorder()

	h.RefreshUsers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался код %d, получен %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"count\":1") {
		t.Fatalf("ожидали count=1 в ответе, получили %s", rec.Body.String())
	}
}

func TestMegafonVATSHandler_SuggestUser(t *testing.T) {
	h := NewMegafonVATSHandler(&fakeMegafonVATSSyncService{
		enabled: true,
		searchResult: []telephony.ProviderEmployee{
			{
				EmployeeLogin: "admin",
				EmployeeName:  "Иван Иванов",
			},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/megafon-vats/users/suggest?first_name=Иван&last_name=Иванов", nil)
	rec := httptest.NewRecorder()

	h.SuggestUser(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидался код %d, получен %d", http.StatusOK, rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "\"login\":\"admin\"") {
		t.Fatalf("ожидали suggestion.login=admin, получили %s", rec.Body.String())
	}
}
