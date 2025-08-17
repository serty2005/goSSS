package seeder

import (
	"context"
	"encoding/json"
	"etalon-server/internal/models"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
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
// dataPath - это путь к директории с файлами *.json.
func NewMockServiceDeskClient(logger *zap.Logger, dataPath string) services.ServiceDeskClient {
	return &MockServiceDeskClient{
		logger:   logger,
		dataPath: dataPath,
	}
}

// FetchEntityList читает список сущностей из JSON-файла.
// Имя файла определяется по metaClass. Например, "ou$company" -> "companies.json".
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
	default:
		return nil, fmt.Errorf("неизвестный metaClass для мок-клиента: %s", metaClass)
	}

	fullPath := filepath.Join(m.dataPath, fileName)
	m.logger.Info("Чтение мок-данных из файла", zap.String("path", fullPath))

	file, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл с мок-данными %s: %w", fullPath, err)
	}

	// ИСПРАВЛЕНИЕ: Мы ожидаем JSON-массив, а не объект с ключом "list"
	var responseList []map[string]interface{}

	if err := json.Unmarshal(file, &responseList); err != nil {
		return nil, fmt.Errorf("не удалось декодировать JSON из файла %s: %w", fullPath, err)
	}

	return responseList, nil
}

// CheckAgreementActive для мок-клиента всегда возвращает true.
// Это упрощает логику наполнения, так как нам не нужно иметь моки для договоров.
func (m *MockServiceDeskClient) CheckAgreementActive(ctx context.Context, agreementUUID string) (bool, error) {
	return true, nil
}

// FetchEntityDetails не используется в режиме наполнения, но является частью интерфейса.
func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	return nil, fmt.Errorf("метод FetchEntityDetails не реализован для мок-клиента")
}

// DataToCompanyForSeeder - это специальная версия маппера для seeder'а.
// Она использует мок-клиент и логгер, переданные как аргументы.
func DataToCompanyForSeeder(ctx context.Context, data map[string]interface{}, sdClient services.ServiceDeskClient, logger *zap.Logger) (*models.Company, error) {
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
	// Учитываем, что в реальном JSON поле называется 'adress'
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		// В JSON может быть `null`, который Go парсит как `nil` для `interface{}`.
		// Проверяем, что это не так, прежде чем делать каст.
		if addName != "" {
			company.AdditionalName = &addName
		}
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	if parent, ok := data["parent"].(map[string]interface{}); ok && parent != nil {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.ParentServiceDeskUUID = &parentUUID
		}
	}

	active := false
	if agreements, ok := data["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			if agrMap, agrOk := agr.(map[string]interface{}); agrOk {
				if agrUUID, uuidOk := agrMap["UUID"].(string); uuidOk {
					// Используем переданный мок-клиент для проверки
					isActive, err := sdClient.CheckAgreementActive(ctx, agrUUID)
					if err != nil {
						logger.Warn("Ошибка при проверке статуса договора (мок)", zap.String("agreementUUID", agrUUID), zap.Error(err))
						continue
					}
					if isActive {
						active = true
						break
					}
				}
			}
		}
	}
	company.ActiveContract = &active

	return company, nil
}
