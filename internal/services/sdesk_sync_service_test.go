package services

import (
	"context"
	"etalon-server/internal/models"

	"github.com/stretchr/testify/mock"
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
func (m *MockCompanyRepo) GetByUUIDs(ctx context.Context, uuids []string) ([]models.Company, error) {
	// В данном наборе тестов этот метод не вызывается,
	// поэтому мы можем просто вернуть пустые значения.
	args := m.Called(ctx, uuids)
	val, ok := args.Get(0).([]models.Company)
	if !ok {
		return nil, args.Error(1)
	}
	return val, args.Error(1)
}
func (m *MockCompanyRepo) GetByUUIDUnscoped(ctx context.Context, uuid string) (*models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) Search(ctx context.Context, term string, showInactive bool, limit, offset int) ([]models.Company, error) {
	return nil, nil
}
func (m *MockCompanyRepo) GetAllUUIDsAndDates(ctx context.Context) (map[string]*models.Company, error) {
	args := m.Called(ctx)
	// ВАЖНО: Тест теперь устарел, т.к. возвращаемый тип изменился.
	// Оставляем как есть для компиляции, но тест требует переписывания.
	val, ok := args.Get(0).(map[string]*models.Company)
	if !ok {
		return nil, args.Error(1)
	}
	return val, args.Error(1)
}
