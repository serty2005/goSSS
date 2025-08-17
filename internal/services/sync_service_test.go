package services

import (
	"context"
	"etalon-server/internal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MockCompanyRepo для тестирования SyncService
type MockCompanyRepo struct {
	mock.Mock
}

func (m *MockCompanyRepo) Create(ctx context.Context, tx *gorm.DB, company *models.Company) error {
	args := m.Called(ctx, tx, company)
	return args.Error(0)
}
func (m *MockCompanyRepo) Update(ctx context.Context, tx *gorm.DB, uuid string, data map[string]interface{}) (bool, error) {
	args := m.Called(ctx, tx, uuid, data)
	return args.Bool(0), args.Error(1)
}
func (m *MockCompanyRepo) Delete(ctx context.Context, tx *gorm.DB, uuid string) (bool, error) {
	return false, nil
}
func (m *MockCompanyRepo) GetByUUID(ctx context.Context, uuid string) (*models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]*models.Company), args.Error(1)
}

func TestSyncService_SyncCompanies(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()
	now := time.Now()
	yesterday := now.Add(-24 * time.Hour)
	tomorrow := now.Add(24 * time.Hour)

	t.Run("creates new company", func(t *testing.T) {
		localMockRepo := new(MockCompanyRepo)
		localMockSDClient := new(MockServiceDeskClient)
		localSyncService := NewSyncService(localMockSDClient, localMockRepo, nil, nil, nil, logger, 1)

		remoteList := []map[string]interface{}{
			{"UUID": "new-company-1", "lastModifiedDate": "2023.10.30 10:00:00"},
		}
		localMap := make(map[string]*models.Company)

		localMockSDClient.On("FetchEntityList", ctx, "ou$company", false).Return(remoteList, nil).Once()
		localMockRepo.On("GetAllUUIDsAndDates", ctx).Return(localMap, nil).Once()
		localMockSDClient.On("FetchEntityDetails", ctx, "new-company-1", "ou$company").Return(map[string]interface{}{
			"UUID": "new-company-1", "title": "Новая Компания",
		}, nil).Once()
		// Ожидаем, что tx будет nil
		localMockRepo.On("Create", ctx, (*gorm.DB)(nil), mock.AnythingOfType("*models.Company")).Return(nil).Once()

		localSyncService.(*syncServiceImpl).syncCompanies(ctx)

		localMockSDClient.AssertExpectations(t)
		localMockRepo.AssertExpectations(t)
	})

	t.Run("updates existing company", func(t *testing.T) {
		localMockRepo := new(MockCompanyRepo)
		localMockSDClient := new(MockServiceDeskClient)
		localSyncService := NewSyncService(localMockSDClient, localMockRepo, nil, nil, nil, logger, 1)

		remoteList := []map[string]interface{}{
			{"UUID": "existing-company-1", "lastModifiedDate": now.Format("2006.01.02 15:04:05")},
		}
		localMap := map[string]*models.Company{
			"existing-company-1": {Base: models.Base{ServiceDeskUUID: StringPtr("existing-company-1")}, LastModifiedDate: &yesterday},
		}

		localMockSDClient.On("FetchEntityList", ctx, "ou$company", false).Return(remoteList, nil).Once()
		localMockRepo.On("GetAllUUIDsAndDates", ctx).Return(localMap, nil).Once()
		localMockSDClient.On("FetchEntityDetails", ctx, "existing-company-1", "ou$company").Return(map[string]interface{}{
			"UUID": "existing-company-1", "title": "Обновленная Компания",
		}, nil).Once()
		// Ожидаем, что tx будет nil
		localMockRepo.On("Update", ctx, (*gorm.DB)(nil), "existing-company-1", mock.Anything).Return(true, nil).Once()

		localSyncService.(*syncServiceImpl).syncCompanies(ctx)

		localMockSDClient.AssertExpectations(t)
		localMockRepo.AssertExpectations(t)
	})

	t.Run("skips up-to-date company", func(t *testing.T) {
		localMockRepo := new(MockCompanyRepo)
		localMockSDClient := new(MockServiceDeskClient)
		localSyncService := NewSyncService(localMockSDClient, localMockRepo, nil, nil, nil, logger, 1)

		remoteList := []map[string]interface{}{
			{"UUID": "uptodate-company-1", "lastModifiedDate": now.Format("2006.01.02 15:04:05")},
		}
		localMap := map[string]*models.Company{
			"uptodate-company-1": {Base: models.Base{ServiceDeskUUID: StringPtr("uptodate-company-1")}, LastModifiedDate: &tomorrow},
		}

		localMockSDClient.On("FetchEntityList", ctx, "ou$company", false).Return(remoteList, nil).Once()
		localMockRepo.On("GetAllUUIDsAndDates", ctx).Return(localMap, nil).Once()

		localSyncService.(*syncServiceImpl).syncCompanies(ctx)

		localMockSDClient.AssertExpectations(t)
		localMockRepo.AssertExpectations(t)
		localMockSDClient.AssertNotCalled(t, "FetchEntityDetails", mock.Anything, mock.Anything, mock.Anything)
		localMockRepo.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		localMockRepo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything)
	})
}
