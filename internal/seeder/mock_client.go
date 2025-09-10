// internal/seeder/mock_client.go
package seeder

import (
	"context"
	"encoding/json"
	"etalon-server/internal/external"
	"etalon-server/internal/logger"
	"etalon-server/internal/models"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gorm.io/datatypes"
)

// MockServiceDeskClient имитирует клиент ServiceDesk для чтения данных из локальных файлов.
type MockServiceDeskClient struct {
	logger   logger.LoggerInterface
	dataPath string
	mapper   external.Mapper
}

// NewMockServiceDeskClient создает новый мок-клиент.
func NewMockServiceDeskClient(logger logger.LoggerInterface, dataPath string) external.ExternalSystemClient {
	return &MockServiceDeskClient{
		logger:   logger,
		dataPath: dataPath,
		mapper:   newMockMapper(logger),
	}
}

func (m *MockServiceDeskClient) Mapper() external.Mapper {
	return m.mapper
}

func (m *MockServiceDeskClient) FetchEntityList(ctx context.Context, entityType string) ([]map[string]interface{}, error) {
	var fileName string
	switch entityType {
	case "Company":
		fileName = "companies.json"
	case "Server":
		fileName = "servers.json"
	case "Workstation":
		fileName = "workstations.json"
	case "FiscalRegister":
		fileName = "fiscal_registers.json"
	case "Contract":
		fileName = "agreements.json"
	default:
		return nil, fmt.Errorf("неизвестный entityType для мок-клиента: %s", entityType)
	}
	fullPath := filepath.Join(m.dataPath, fileName)
	file, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}
	var responseList []map[string]interface{}
	if err := json.Unmarshal(file, &responseList); err != nil {
		return nil, err
	}
	return responseList, nil
}

// --- Заглушки для методов интерфейса, не используемых в сидере ---
func (m *MockServiceDeskClient) FetchEntitySummaries(ctx context.Context, entityType string) ([]map[string]interface{}, error) {
	return m.FetchEntityList(ctx, entityType)
}
func (m *MockServiceDeskClient) FetchEntityDetails(ctx context.Context, externalID string, entityType string) (map[string]interface{}, error) {
	return nil, nil
}
func (m *MockServiceDeskClient) FetchCompanyContractStates(ctx context.Context) (map[string]external.ContractState, error) {
	return nil, nil
}
func (m *MockServiceDeskClient) UpdateEntity(ctx context.Context, externalID string, entityType string, data map[string]interface{}) error {
	return nil
}
func (m *MockServiceDeskClient) CreateEntity(ctx context.Context, entityType string, data map[string]interface{}) (map[string]interface{}, error) {
	return nil, nil
}
func (m *MockServiceDeskClient) FindReferenceID(ctx context.Context, referenceType, title string, useSubstringSearch bool) (string, error) {
	return "", nil
}

// --- Мок-реализация маппера специально для сидера ---
type mockMapper struct {
	logger logger.LoggerInterface
}

func newMockMapper(logger logger.LoggerInterface) external.Mapper {
	return &mockMapper{logger: logger}
}

var innRegex = regexp.MustCompile(`ИНН:\s*(\d{10,12})`)

func getOwnerUUID(data map[string]interface{}) string {
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if oUUID, oOk := owner["UUID"].(string); oOk {
			return oUUID
		}
	}
	return ""
}

func (m *mockMapper) DataToCompany(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Company, error) {
	company := &models.Company{}
	company.MetaClass = "ou$company"
	if title, ok := data["title"].(string); ok {
		company.Title = &title
	}
	if address, ok := data["adress"].(string); ok {
		company.Address = &address
	}
	if addName, ok := data["additionalName"].(string); ok {
		company.AdditionalName = &addName
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		company.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}
	if parent, ok := data["parent"].(map[string]interface{}); ok {
		if parentUUID, p_ok := parent["UUID"].(string); p_ok {
			company.MetaClass = parentUUID
		}
	}

	// Логи для диагностики проблем с парсингом
	extID, _ := data["UUID"].(string)
	m.logger.Info("Парсинг компании", "extID", extID)

	if rawAdditionalName, ok := data["additionalName"].(string); ok && rawAdditionalName != "" {
		m.logger.Info("Найден additionalName", "additionalName", rawAdditionalName)
	} else {
		m.logger.Warn("additionalName отсутствует или пустой", "extID", extID)
	}

	return company, nil
}

func (m *mockMapper) DataToServer(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Server, error) {
	server := &models.Server{}
	server.MetaClass = "objectBase$Server"
	ownerExtID := getOwnerUUID(data)
	server.OwnerID = &ownerExtID
	rawUniqueID, _ := data["UniqueID"].(string)
	server.UniqueID = validators.ValidateUniqueID(rawUniqueID)
	rawTeamviewer, _ := data["Teamviewer"].(string)
	server.Teamviewer = validators.ValidateRemoteAccessID(rawTeamviewer)
	rawAnydesk, _ := data["AnyDesk"].(string)
	server.Anydesk = validators.ValidateRemoteAccessID(rawAnydesk)
	rawIP, _ := data["IP"].(string)
	server.IP = validators.ValidateIPAddress(rawIP)
	if rawRDP, ok := data["RDP"].(string); ok && rawRDP != "" {
		server.RDP = &rawRDP
	}
	if rawDeviceName, ok := data["DeviceName"].(string); ok && rawDeviceName != "" {
		server.DeviceName = &rawDeviceName
	}
	if rawIikoVersion, ok := data["iikoVersion"].(string); ok && rawIikoVersion != "" {
		server.ServerVersion = &rawIikoVersion
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		server.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Логи для диагностики проблем с парсингом
	extID, _ := data["UUID"].(string)
	m.logger.Info("Парсинг сервера", "extID", extID)

	if rawCabinetLink, ok := data["CabinetLink"].(string); ok && rawCabinetLink != "" {
		m.logger.Info("Найден CabinetLink", "cabinetLink", rawCabinetLink)
		link := validators.ValidateCabinetLink(rawCabinetLink, "")
		server.CabinetLink = &link
	} else {
		m.logger.Warn("CabinetLink отсутствует или пустой", "extID", extID)
	}

	if rawDescription, ok := data["description"].(string); ok && rawDescription != "" {
		m.logger.Info("Найдено description", "description", rawDescription)
		server.Description = &rawDescription
	} else {
		m.logger.Warn("description отсутствует или пустое", "extID", extID)
	}

	if rawNameForClient, ok := data["nameforclient"].(string); ok && rawNameForClient != "" {
		m.logger.Info("Найдено nameforclient", "nameforclient", rawNameForClient)
		server.ServerName = &rawNameForClient
	} else {
		m.logger.Warn("nameforclient отсутствует или пустое", "extID", extID)
	}

	if rawLitemanager, ok := data["litemanagerID"].(string); ok && rawLitemanager != "" {
		m.logger.Info("Найден litemanagerID", "litemanagerID", rawLitemanager)
		server.Litemanager = &rawLitemanager
	} else {
		m.logger.Warn("litemanagerID отсутствует или пустой", "extID", extID)
	}

	return server, nil
}

func (m *mockMapper) DataToWorkstation(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Workstation, error) {
	ws := &models.Workstation{}
	ws.MetaClass = "objectBase$Workstation"
	ownerExtID := getOwnerUUID(data)
	ws.OwnerID = &ownerExtID
	if tv, ok := data["Teamviewer"].(string); ok {
		ws.Teamviewer = validators.ValidateRemoteAccessID(tv)
	}
	if ad, ok := data["AnyDesk"].(string); ok {
		ws.Anydesk = validators.ValidateRemoteAccessID(ad)
	}
	if dn, ok := data["DeviceName"].(string); ok {
		ws.DeviceName = &dn
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		ws.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	// Логи для диагностики проблем с парсингом
	extID, _ := data["UUID"].(string)
	m.logger.Info("Парсинг рабочей станции", "extID", extID)

	if rawCommentariy, ok := data["Commentariy"].(string); ok && rawCommentariy != "" {
		m.logger.Info("Найден Commentariy", "commentariy", rawCommentariy)
		ws.Description = &rawCommentariy
	} else {
		m.logger.Warn("Commentariy отсутствует или пустой", "extID", extID)
	}

	if rawLitemanager, ok := data["litemanagerID"].(string); ok && rawLitemanager != "" {
		m.logger.Info("Найден litemanagerID", "litemanagerID", rawLitemanager)
		ws.Litemanager = &rawLitemanager
	} else {
		m.logger.Warn("litemanagerID отсутствует или пустой", "extID", extID)
	}

	return ws, nil
}

func (m *mockMapper) DataToFiscalRegister(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.FiscalRegister, error) {
	fr := &models.FiscalRegister{}
	fr.MetaClass = "objectBase$FR"
	ownerExtID := getOwnerUUID(data)
	fr.OwnerID = &ownerExtID
	if val, ok := data["ModelKKT"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.ModelKKT = &title
		}
	} else if val, ok := data["ModelKKT"].(string); ok {
		fr.ModelKKT = &val
	}
	if val, ok := data["RNKKT"].(string); ok {
		normalizedRNKKT := utils.NormalizeRNKKT(val)
		fr.RNKKT = &normalizedRNKKT
	}
	if val, ok := data["LegalName"].(string); ok {
		if matches := innRegex.FindStringSubmatch(val); len(matches) > 1 {
			inn := matches[1]
			fr.INN = &inn
			cleanName := strings.TrimSpace(innRegex.ReplaceAllString(val, ""))
			fr.LegalName = &cleanName
		} else {
			fr.LegalName = &val
		}
	}
	if val, ok := data["FRSerialNumber"].(string); ok {
		fr.FRSerialNumber = &val
	}
	if val, ok := data["FNNumber"].(string); ok {
		fr.FNNumber = &val
	}
	if val, ok := data["KKTRegDate"].(string); ok {
		fr.KKTRegDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["FNExpireDate"].(string); ok {
		fr.FNExpireDate = utils.ParseServiceDeskTime(val)
	}
	if val, ok := data["lastModifiedDate"].(string); ok {
		fr.LastModifiedDate = utils.ParseServiceDeskTime(val)
	}

	// Логи для диагностики проблем с парсингом
	extID, _ := data["UUID"].(string)
	m.logger.Info("Парсинг фискального регистратора", "extID", extID)

	if rawFFD, ok := data["FFD"]; ok {
		if ffdMap, ok2 := rawFFD.(map[string]interface{}); ok2 {
			if title, ok3 := ffdMap["title"].(string); ok3 {
				m.logger.Info("Найден FFD", "ffd", title)
				fr.FFD = &title
			} else {
				m.logger.Warn("FFD найден, но title отсутствует", "extID", extID)
			}
		} else if ffdStr, ok2 := rawFFD.(string); ok2 {
			m.logger.Info("Найден FFD (строка)", "ffd", ffdStr)
			fr.FFD = &ffdStr
		} else {
			m.logger.Warn("FFD найден, но не является map или string", "extID", extID)
		}
	} else {
		m.logger.Warn("FFD отсутствует", "extID", extID)
	}

	if rawFRDownloader, ok := data["FRDownloader"].(string); ok && rawFRDownloader != "" {
		m.logger.Info("Найден FRDownloader", "frDownloader", rawFRDownloader)
		fr.FRDownloader = &rawFRDownloader
	} else {
		m.logger.Warn("FRDownloader отсутствует или пустой", "extID", extID)
	}

	if rawFRFirmware, ok := data["FRFirmware"].(string); ok && rawFRFirmware != "" {
		m.logger.Info("Найден FRFirmware", "frFirmware", rawFRFirmware)
		fr.FRFirmware = &rawFRFirmware
	} else {
		m.logger.Warn("FRFirmware отсутствует или пустой", "extID", extID)
	}

	return fr, nil
}

func (m *mockMapper) DataToContract(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Contract, error) {
	contract := &models.Contract{}
	contract.MetaClass = "agreement$agreement"
	if state, ok := data["state"].(string); ok {
		contract.State = &state
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		contract.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}
	if sst, ok := data["stateStartTime"].(string); ok {
		contract.StateStartTime = utils.ParseServiceDeskTime(sst)
	}
	var serviceTitles []string
	if services, ok := data["services"].([]interface{}); ok {
		for _, item := range services {
			if m, _ := item.(map[string]interface{}); m != nil {
				if t, _ := m["title"].(string); t != "" {
					serviceTitles = append(serviceTitles, t)
				}
			}
		}
		if j, err := json.Marshal(serviceTitles); err == nil {
			contract.Services = datatypes.JSON(j)
		}
	}
	contract.ServiceLevel = m.determineServiceLevel(serviceTitles)
	if recipients, ok := data["recipientsOU"].([]interface{}); ok {
		var recipientUUIDs []string
		for _, item := range recipients {
			if m, _ := item.(map[string]interface{}); m != nil {
				if u, _ := m["UUID"].(string); u != "" {
					recipientUUIDs = append(recipientUUIDs, u)
				}
			}
		}
		if j, err := json.Marshal(recipientUUIDs); err == nil {
			contract.Recipients = datatypes.JSON(j)
		}
	}
	return contract, nil
}

func (m *mockMapper) determineServiceLevel(serviceTitles []string) int { return -1 }

func (m *mockMapper) GetCompanyUUIDsFromContract(data map[string]interface{}) []string {
	var uuids []string
	if recipients, ok := data["recipientsOU"].([]interface{}); ok {
		for _, r := range recipients {
			if rMap, rOk := r.(map[string]interface{}); rOk {
				if uuid, uuidOk := rMap["UUID"].(string); uuidOk {
					uuids = append(uuids, uuid)
				}
			}
		}
	}
	return uuids
}
