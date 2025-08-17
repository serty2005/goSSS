package services

import (
	"context"
	"etalon-server/internal/models"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

// MockServiceDeskClient для тестирования мапперов, которым он нужен
type MockServiceDeskClient struct {
	mock.Mock
}

func (m *MockServiceDeskClient) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	args := m.Called(ctx, metaClass, full)
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	args := m.Called(ctx, uuid, metaClass)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockServiceDeskClient) CheckAgreementActive(ctx context.Context, agreementUUID string) (bool, error) {
	args := m.Called(ctx, agreementUUID)
	return args.Bool(0), args.Error(1)
}

// Вспомогательная функция для парсинга времени, т.к. utils не импортируется
func parseTime(t string) *time.Time {
	layout := "2006.01.02 15:04:05"
	parsed, err := time.Parse(layout, t)
	if err != nil {
		return nil
	}
	return &parsed
}

func TestDataToCompany(t *testing.T) {
	mockClient := new(MockServiceDeskClient)
	logger := zap.NewNop()
	ctx := context.Background()

	mockClient.On("CheckAgreementActive", mock.Anything, mock.AnythingOfType("string")).Return(true, nil)

	testCases := []struct {
		name        string
		input       map[string]interface{}
		expectError bool
		expected    *models.Company
	}{
		{
			name: "Полные корректные данные",
			input: map[string]interface{}{
				"UUID":             "company-uuid-1",
				"title":            "ООО Ромашка",
				"adress":           "г. Москва",
				"additionalName":   "Главный офис",
				"lastModifiedDate": "2023.10.30 15:00:00",
				"parent":           map[string]interface{}{"UUID": "parent-uuid-2"},
				"recipientAgreements": []interface{}{
					map[string]interface{}{"UUID": "agreement-uuid-3"},
				},
			},
			expectError: false,
			expected: &models.Company{
				Base:                  models.Base{ServiceDeskUUID: StringPtr("company-uuid-1")},
				Title:                 StringPtr("ООО Ромашка"),
				Address:               StringPtr("г. Москва"),
				AdditionalName:        StringPtr("Главный офис"),
				LastModifiedDate:      parseTime("2023.10.30 15:00:00"),
				ParentServiceDeskUUID: StringPtr("parent-uuid-2"),
				ActiveContract:        BoolPtr(true),
			},
		},
		{
			name: "Данные без UUID",
			input: map[string]interface{}{
				"title": "Компания без UUID",
			},
			expectError: true,
			expected:    nil,
		},
		{
			name: "Частичные данные",
			input: map[string]interface{}{
				"UUID":  "company-uuid-4",
				"title": "ООО Василек",
			},
			expectError: false,
			expected: &models.Company{
				Base:           models.Base{ServiceDeskUUID: StringPtr("company-uuid-4")},
				Title:          StringPtr("ООО Василек"),
				ActiveContract: BoolPtr(false), // Договоров нет
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			company, err := DataToCompany(ctx, tc.input, mockClient, logger)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected.ServiceDeskUUID, company.ServiceDeskUUID)
				assert.Equal(t, tc.expected.Title, company.Title)
				assert.Equal(t, tc.expected.Address, company.Address)
				assert.Equal(t, tc.expected.ParentServiceDeskUUID, company.ParentServiceDeskUUID)
				assert.Equal(t, tc.expected.ActiveContract, company.ActiveContract)
				if tc.expected.LastModifiedDate != nil {
					assert.True(t, tc.expected.LastModifiedDate.Equal(*company.LastModifiedDate))
				}
			}
		})
	}
}

func TestDataToServer(t *testing.T) {
	testCases := []struct {
		name        string
		input       map[string]interface{}
		expectError bool
		expected    *models.Server
	}{
		{
			name: "Полные корректные данные",
			input: map[string]interface{}{
				"UUID":             "server-uuid-1",
				"DeviceName":       "SRV-MAIN",
				"IP":               "192.168.1.10",
				"AnyDesk":          "111 222 333",
				"lastModifiedDate": "2023.10.30 16:00:00",
				"owner":            map[string]interface{}{"UUID": "owner-uuid-1"},
			},
			expectError: false,
			expected: &models.Server{
				Base:                 models.Base{ServiceDeskUUID: StringPtr("server-uuid-1")},
				DeviceName:           StringPtr("SRV-MAIN"),
				IP:                   StringPtr("192.168.1.10:8080"),
				Anydesk:              StringPtr("111222333"),
				LastModifiedDate:     parseTime("2023.10.30 16:00:00"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-1"),
			},
		},
		{
			name: "Данные без владельца",
			input: map[string]interface{}{
				"UUID": "server-uuid-2",
			},
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server, err := DataToServer(tc.input)
			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.expected.ServiceDeskUUID, server.ServiceDeskUUID)
				assert.Equal(t, tc.expected.DeviceName, server.DeviceName)
				assert.Equal(t, tc.expected.IP, server.IP)
				assert.Equal(t, tc.expected.Anydesk, server.Anydesk)
				assert.Equal(t, tc.expected.OwnerServiceDeskUUID, server.OwnerServiceDeskUUID)
			}
		})
	}
}

func TestDataToWorkstation(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected *models.Workstation
	}{
		{
			name: "Полные данные",
			input: map[string]interface{}{
				"UUID":        "ws-uuid-1",
				"DeviceName":  "KASSA-1",
				"AnyDesk":     "333 222 111",
				"Teamviewer":  "1234567890",
				"Commentariy": "Основная касса с MH_99999",
				"owner":       map[string]interface{}{"UUID": "owner-uuid-ws-1"},
			},
			expected: &models.Workstation{
				Base: models.Base{
					ServiceDeskUUID: StringPtr("ws-uuid-1"),
					MetaClass:       "objectBase$Workstation",
				},
				DeviceName:           StringPtr("KASSA-1"),
				Anydesk:              StringPtr("333222111"),
				Teamviewer:           StringPtr("1234567890"),
				Litemanager:          StringPtr("MH_99999"),
				Description:          StringPtr("Основная касса с MH_99999"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-ws-1"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ws, err := DataToWorkstation(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, ws)
		})
	}
}

func TestDataToFiscalRegister(t *testing.T) {
	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected *models.FiscalRegister
	}{
		{
			name: "Полные данные",
			input: map[string]interface{}{
				"UUID":           "fr-uuid-1",
				"ModelKKT":       map[string]interface{}{"title": "ШТРИХ-М-01Ф"},
				"FFD":            "1.2",
				"RNKKT":          "0001234567012345",
				"FRSerialNumber": "123456789",
				"FNNumber":       "987654321",
				"FNExpireDate":   "2025.12.31 23:59:59",
				"owner":          map[string]interface{}{"UUID": "owner-uuid-fr-1"},
			},
			expected: &models.FiscalRegister{
				Base:                 models.Base{ServiceDeskUUID: StringPtr("fr-uuid-1")},
				ModelKKT:             StringPtr("ШТРИХ-М-01Ф"),
				FFD:                  StringPtr("1.2"),
				RNKKT:                StringPtr("0001234567012345"),
				FRSerialNumber:       StringPtr("123456789"),
				FNNumber:             StringPtr("987654321"),
				FNExpireDate:         parseTime("2025.12.31 23:59:59"),
				OwnerServiceDeskUUID: StringPtr("owner-uuid-fr-1"),
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fr, err := DataToFiscalRegister(tc.input)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected.ServiceDeskUUID, fr.ServiceDeskUUID)
			assert.Equal(t, tc.expected.ModelKKT, fr.ModelKKT)
			assert.Equal(t, tc.expected.FFD, fr.FFD)
			assert.Equal(t, tc.expected.RNKKT, fr.RNKKT)
			assert.Equal(t, tc.expected.OwnerServiceDeskUUID, fr.OwnerServiceDeskUUID)
			assert.True(t, fr.FNExpireDate.Equal(*tc.expected.FNExpireDate))
		})
	}
}

// Вспомогательные функции для создания указателей в тестах
func StringPtr(s string) *string { return &s }
func BoolPtr(b bool) *bool       { return &b }
