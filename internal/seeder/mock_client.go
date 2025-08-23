// internal/seeder/mock_client.go
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
	"gorm.io/datatypes"
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
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	if parent, ok := data["parent"].(map[string]interface{}); ok && parent != nil {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.ParentServiceDeskUUID = &parentUUID
		}
	}

	// ИЗМЕНЕНИЕ: Добавлено полное наполнение ContractInfo
	isActiveContract := false
	contractInfo := services.ContractInfo{
		Services:          []string{},
		OtherRecipients:   []string{},
		ActiveContractIDs: []string{},
	}
	serviceSet := make(map[string]struct{})
	recipientSet := make(map[string]struct{})

	if agreements, ok := data["recipientAgreements"].([]interface{}); ok {
		for _, agr := range agreements {
			if agrMap, agrOk := agr.(map[string]interface{}); agrOk {
				if agrUUID, uuidOk := agrMap["UUID"].(string); uuidOk {
					details, detailsFound := agreementsCache[agrUUID]
					if !detailsFound {
						continue
					}

					if state, stateOk := details["state"].(string); stateOk && state == "active" {
						isActiveContract = true
						contractInfo.ActiveContractIDs = append(contractInfo.ActiveContractIDs, agrUUID)

						// Парсим сервисы
						if servicesData, sOk := details["services"].([]interface{}); sOk {
							for _, serviceItem := range servicesData {
								if serviceMap, smOk := serviceItem.(map[string]interface{}); smOk {
									if serviceTitle, tOk := serviceMap["title"].(string); tOk {
										if _, exists := serviceSet[serviceTitle]; !exists {
											serviceSet[serviceTitle] = struct{}{}
										}
									}
								}
							}
						}

						// Парсим получателей
						if recipientsData, rOk := details["recipientsOU"].([]interface{}); rOk {
							for _, recipientItem := range recipientsData {
								if recipientMap, rmOk := recipientItem.(map[string]interface{}); rmOk {
									if recipientUUID, uOk := recipientMap["UUID"].(string); uOk && recipientUUID != uuid {
										if _, exists := recipientSet[recipientUUID]; !exists {
											recipientSet[recipientUUID] = struct{}{}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// Заполняем срезы из сетов
	for serviceTitle := range serviceSet {
		contractInfo.Services = append(contractInfo.Services, serviceTitle)
	}
	for recipientUUID := range recipientSet {
		contractInfo.OtherRecipients = append(contractInfo.OtherRecipients, recipientUUID)
	}

	company.ActiveContract = &isActiveContract
	contractInfoJSON, err := json.Marshal(contractInfo)
	if err != nil {
		logger.Error("Не удалось сериализовать информацию о контракте в JSON для сидера", zap.String("companyUUID", uuid), zap.Error(err))
	} else {
		company.ContractInfo = datatypes.JSON(contractInfoJSON)
	}

	return company, nil
}
