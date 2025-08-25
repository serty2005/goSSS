// internal/seeder/mock_client.go
package seeder

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

// MockServiceDeskClient имитирует клиент ServiceDesk для чтения данных из локальных файлов.
type MockServiceDeskClient struct {
	logger   *zap.Logger
	dataPath string
}

// NewMockServiceDeskClient создает новый мок-клиент.
func NewMockServiceDeskClient(logger *zap.Logger, dataPath string) services.ServiceDeskClient {
	return &MockServiceDeskClient{
		logger:   logger,
		dataPath: dataPath,
	}
}

// FetchEntityList читает список сущностей из JSON-файла.
func (m *MockServiceDeskClient) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	var fileName string
	switch metaClass {
	case "ou$company":
		fileName = "companies.json"
	case "objectBase$Server":
		fileName = "servers.json"
	case "objectBase$Workstation":
		fileName = "workstations.json"
	case "objectBase$FR":
		fileName = "fiscal_registers.json"
	case "agreement$agreement":
		fileName = "agreements.json"
	default:
		return nil, fmt.Errorf("неизвестный metaClass для мок-клиента: %s", metaClass)
	}

	fullPath := filepath.Join(m.dataPath, fileName)
	m.logger.Info("Чтение мок-данных из файла", zap.String("path", fullPath))

	file, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл с мок-данными %s: %w", fullPath, err)
	}

	var responseList []map[string]interface{}
	if err := json.Unmarshal(file, &responseList); err != nil {
		return nil, fmt.Errorf("не удалось декодировать JSON из файла %s: %w", fullPath, err)
	}

	return responseList, nil
}

// FetchAgreementDetails не используется в seeder'е, но является частью интерфейса.
func (m *MockServiceDeskClient) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*services.AgreementDetailsDTO, error) {
	return nil, fmt.Errorf("метод FetchAgreementDetails не должен вызываться в мок-клиенте для seeder'а")
}

// FetchEntityDetails не используется в режиме наполнения, но является частью интерфейса.
func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("метод FetchEntityDetails не реализован для мок-клиента")
}

// DataToCompanyForSeeder - это специальная версия маппера для seeder'а.
// ИСПРАВЛЕНИЕ: Удалена вся логика, связанная с ContractInfo.
func DataToCompanyForSeeder(ctx context.Context, data map[string]interface{}, agreementsCache map[string]map[string]interface{}, logger *zap.Logger) (*models.Company, error) {
	uuid, _ := data["UUID"].(string)
	if uuid == "" {
		return nil, fmt.Errorf("в данных компании отсутствует UUID")
	}

	company := &models.Company{}
	company.ServiceDeskUUID = &uuid
	company.MetaClass = "ou$company"

	if title, ok := data["title"].(string); ok {
		company.Title = &title
	}
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		if addName != "" {
			company.AdditionalName = &addName
		}
	}

	return company, nil
}
