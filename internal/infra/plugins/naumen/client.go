package naumen

import (
	"bytes"
	"context"
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/transport/http/validators"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// карты атрибутов, специфичные для Naumen ServiceDesk.
var attrsMap = map[string]string{
	"ou$company":             "adress,UUID,title,lastModifiedDate,additionalName,parent,recipientAgreements",
	"objectBase$Server":      "UniqueID,Teamviewer,RDP,AnyDesk,UUID,IP,CabinetLink,DeviceName,lastModifiedDate,iikoVersion,description,nameforclient,owner,litemanagerID",
	"objectBase$Workstation": "Commentariy,Teamviewer,AnyDesk,DeviceName,litemanagerID,lastModifiedDate,UUID,owner",
	"objectBase$FR":          "UUID,ModelKKT,lastModifiedDate,owner,FFD,FRDownloader,RNKKT,KKTRegDate,FNExpireDate,LegalName,FRSerialNumber,FNNumber,FRFirmware",
	"agreement$agreement":    "state,stateStartTime,services,recipientsOU,lastModifiedDate", // Добавлено для контрактов
}

// карты минимальных атрибутов, специфичные для Naumen ServiceDesk.
var minimalAttrsMap = map[string]string{
	"ou$company":             "UUID,lastModifiedDate,parent",
	"objectBase$Server":      "UUID,lastModifiedDate,owner",
	"objectBase$Workstation": "UUID,lastModifiedDate,owner",
	"objectBase$FR":          "UUID,lastModifiedDate,owner",
}

type naumenClientImpl struct {
	client         *http.Client
	baseURL        string
	apiKey         string
	limiter        *rate.Limiter
	logger         logger.LoggerInterface
	maxRetries     int
	dryRun         bool
	referenceCache map[string]string
	cacheMutex     sync.RWMutex
	mapper         external.Mapper
}

// NewNaumenClient создает новый клиент для Naumen ServiceDesk.
// Возвращает тип интерфейса external.ExternalSystemClient.
func NewNaumenClient(cfg *config.Config, logger logger.LoggerInterface, db *gorm.DB, linkRepo repositories.LinkRepo) external.ExternalSystemClient {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	client := &naumenClientImpl{
		client:         &http.Client{Transport: transport, Timeout: cfg.RequestTimeout},
		baseURL:        strings.TrimRight(cfg.ServiceDeskBaseURL, "/"),
		apiKey:         cfg.ServiceDeskKey,
		limiter:        rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		logger:         logger,
		maxRetries:     cfg.MaxRetries,
		dryRun:         cfg.ServiceDeskDryRun,
		referenceCache: make(map[string]string),
	}
	client.mapper = newNaumenMapper(db, linkRepo, logger)
	return client
}

// Mapper возвращает реализацию маппера, специфичную для Naumen.
func (s *naumenClientImpl) Mapper() external.Mapper {
	return s.mapper
}

// FetchCompanyContractStates реализует метод интерфейса, инкапсулируя логику Naumen.
func (s *naumenClientImpl) FetchCompanyContractStates(ctx context.Context) (map[string]external.ContractState, error) {
	remoteContracts, err := s.FetchEntityList(ctx, "Contract") // Используем наш внутренний тип "Contract"
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список контрактов для определения статусов: %w", err)
	}

	companyStates := make(map[string]external.ContractState)
	for _, contractData := range remoteContracts {
		state, _ := contractData["state"].(string)
		isActive := state == "active"

		if recipients, ok := contractData["recipientsOU"].([]interface{}); ok {
			for _, r := range recipients {
				if rMap, rOk := r.(map[string]interface{}); rOk {
					if uuid, uuidOk := rMap["UUID"].(string); uuidOk {
						if existing, exists := companyStates[uuid]; !exists || !existing.IsActive {
							companyStates[uuid] = external.ContractState{IsActive: isActive}
						}
					}
				}
			}
		}
	}

	return companyStates, nil
}

// FetchEntityList получает список сущностей.
func (s *naumenClientImpl) FetchEntityList(ctx context.Context, entityType string) ([]map[string]interface{}, error) {
	metaClass, ok := s.mapEntityTypeToMetaClass(entityType)
	if !ok {
		return nil, fmt.Errorf("неизвестный тип сущности для Naumen: %s", entityType)
	}
	attrs := attrsMap[metaClass]
	url := fmt.Sprintf("%s/find/%s", s.baseURL, metaClass)
	params := map[string]string{"attrs": attrs}
	var responseList []map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &responseList, params)
	return responseList, err
}

// FetchEntitySummaries получает КРАТКИЙ список сущностей для быстрой сверки.
func (s *naumenClientImpl) FetchEntitySummaries(ctx context.Context, entityType string) ([]map[string]interface{}, error) {
	metaClass, ok := s.mapEntityTypeToMetaClass(entityType)
	if !ok {
		return nil, fmt.Errorf("неизвестный тип сущности для Naumen: %s", entityType)
	}
	attrs, ok := minimalAttrsMap[metaClass]
	if !ok {
		// Для некоторых типов (например, Contract) может не быть минимального набора, тогда берем полный
		attrs = attrsMap[metaClass]
	}
	url := fmt.Sprintf("%s/find/%s", s.baseURL, metaClass)
	params := map[string]string{"attrs": attrs}
	var responseList []map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &responseList, params)
	return responseList, err
}

// FetchEntityDetails получает детальную информацию о сущности.
func (s *naumenClientImpl) FetchEntityDetails(ctx context.Context, externalID string, entityType string) (map[string]interface{}, error) {
	metaClass, ok := s.mapEntityTypeToMetaClass(entityType)
	if !ok {
		return nil, fmt.Errorf("неизвестный тип сущности для Naumen: %s", entityType)
	}
	attrs := attrsMap[metaClass]
	url := fmt.Sprintf("%s/get/%s", s.baseURL, externalID)
	params := map[string]string{"attrs": attrs}
	var response map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response, params)
	return response, err
}

// UpdateEntity обновляет сущность.
func (s *naumenClientImpl) UpdateEntity(ctx context.Context, externalID string, entityType string, data map[string]interface{}) error {
	if len(data) == 0 {
		return nil
	}
	if s.dryRun {
		s.logger.Warn("[DRY RUN] Отправка запроса на ОБНОВЛЕНИЕ в Naumen SD пропущена.",
			"externalID", externalID,
			"params", data,
		)
		return nil
	}
	url := fmt.Sprintf("%s/edit/%s", s.baseURL, externalID)
	queryParams := make(map[string]string)
	for key, value := range data {
		queryParams[key] = fmt.Sprintf("%v", value)
	}
	return s.doWithRetry(ctx, http.MethodPost, url, nil, nil, queryParams)
}

// CreateEntity создает сущность.
func (s *naumenClientImpl) CreateEntity(ctx context.Context, entityType string, data map[string]interface{}) (map[string]interface{}, error) {
	metaClass, ok := s.mapEntityTypeToMetaClass(entityType)
	if !ok {
		return nil, fmt.Errorf("неизвестный тип сущности для Naumen: %s", entityType)
	}
	if s.dryRun {
		s.logger.Warn("[DRY RUN] Отправка запроса на СОЗДАНИЕ в Naumen SD пропущена.", "metaClass", metaClass)
		return map[string]interface{}{"UUID": "dry-run-fake-uuid"}, nil
	}
	url := fmt.Sprintf("%s/create-m2m/%s", s.baseURL, metaClass)
	bodyBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("ошибка сериализации данных: %w", err)
	}
	var response map[string]interface{}
	params := map[string]string{"attrs": "UUID"}
	err = s.doWithRetry(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes), &response, params)
	return response, err
}

// FindReferenceID ищет ID в справочнике.
func (s *naumenClientImpl) FindReferenceID(ctx context.Context, referenceType, title string, useSubstringSearch bool) (string, error) {
	metaClass, ok := s.mapEntityTypeToMetaClass(referenceType)
	if !ok {
		return "", fmt.Errorf("неизвестный тип справочника для Naumen: %s", referenceType)
	}
	cacheKey := fmt.Sprintf("%s:%s", metaClass, title)
	s.cacheMutex.RLock()
	cachedID, found := s.referenceCache[cacheKey]
	s.cacheMutex.RUnlock()
	if found {
		return cachedID, nil
	}
	url := fmt.Sprintf("%s/find/%s", s.baseURL, metaClass)
	params := map[string]string{"attrs": "UUID,title"}
	var response []map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &response, params)
	if err != nil {
		return "", fmt.Errorf("ошибка получения справочника %s: %w", metaClass, err)
	}
	var foundUUID, foundTitle string
	for _, item := range response {
		itemTitle, okT := item["title"].(string)
		itemUUID, okU := item["UUID"].(string)
		if !okT || !okU {
			continue
		}
		match := false
		if useSubstringSearch {
			if strings.Contains(itemTitle, title) {
				match = true
			}
		} else {
			if itemTitle == title {
				match = true
			}
		}
		if match {
			if foundUUID != "" {
				s.logger.Warn("Найдено несколько значений в справочнике, будет использовано первое",
					"metaClass", metaClass, "title", title,
					"found1", foundTitle, "found2", itemTitle)
				break
			}
			foundUUID = itemUUID
			foundTitle = itemTitle
		}
	}
	if foundUUID == "" {
		return "", fmt.Errorf("не найдено значение '%s' в справочнике %s", title, metaClass)
	}
	s.cacheMutex.Lock()
	s.referenceCache[cacheKey] = foundUUID
	s.cacheMutex.Unlock()
	return foundUUID, nil
}

// doWithRetry выполняет HTTP-запрос с политикой повторов и улучшенным логированием.
func (s *naumenClientImpl) doWithRetry(ctx context.Context, method, url string, body io.Reader, target interface{}, queryParams ...map[string]string) error {
	var bodyBytes []byte
	var err error
	if body != nil {
		bodyBytes, err = io.ReadAll(body)
		if err != nil {
			return fmt.Errorf("не удалось прочитать тело запроса для логирования: %w", err)
		}
	}
	for i := 0; i < s.maxRetries; i++ {
		if err = s.limiter.Wait(ctx); err != nil {
			return err
		}
		var requestBody io.Reader
		if bodyBytes != nil {
			requestBody = bytes.NewBuffer(bodyBytes)
		}
		req, reqErr := http.NewRequestWithContext(ctx, method, url, requestBody)
		if reqErr != nil {
			return fmt.Errorf("не удалось создать запрос: %w", reqErr)
		}
		q := req.URL.Query()
		q.Add("accessKey", s.apiKey)
		if len(queryParams) > 0 {
			for k, v := range queryParams[0] {
				q.Add(k, v)
			}
		}
		req.URL.RawQuery = q.Encode()
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		// ИЗМЕНЕНИЕ 2.0: Маскирование ключа в логах
		urlToLog := *req.URL
		qLog := urlToLog.Query()
		if qLog.Has("accessKey") {
			qLog.Set("accessKey", "[REDACTED]")
		}
		urlToLog.RawQuery = qLog.Encode()

		s.logger.Info("Отправка запроса в Naumen ServiceDesk",
			"метод", method,
			"url", urlToLog.String(),
			"тело", string(bodyBytes),
		)

		resp, doErr := s.client.Do(req)
		if doErr != nil {
			err = fmt.Errorf("ошибка выполнения запроса: %w", doErr)
			s.logger.Warn("Запрос не удался, повторная попытка...", "error", err, "попытка", i+1)
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			bodyString := string(respBodyBytes)
			err = fmt.Errorf("API Naumen SD вернуло ошибку: статус %d, тело: %s", resp.StatusCode, bodyString)
			s.logger.Error("Получена ошибка от Naumen ServiceDesk",
				"статус", resp.StatusCode,
				"ответ", bodyString,
				"url_запроса", urlToLog.String(),
			)
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				// Ошибка авторизации, нет смысла повторять
				return err
			}
			if resp.StatusCode == 500 && strings.Contains(bodyString, "ключ авторизации") && strings.Contains(bodyString, "не найден") {
				s.logger.Error("Критическая ошибка: ключ доступа ServiceDesk (accessKey) невалиден. Проверьте конфигурацию.",
					"sd_ответ", bodyString,
				)
			}
			s.logger.Warn("Ошибка сервера Naumen ServiceDesk, повторная попытка...", "error", err, "попытка", i+1)
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}
		if target != nil {
			if resp.StatusCode != http.StatusNoContent && resp.ContentLength != 0 {
				if decodeErr := json.NewDecoder(resp.Body).Decode(target); decodeErr != nil {
					return fmt.Errorf("не удалось декодировать ответ: %w", decodeErr)
				}
			}
		}
		return nil
	}
	return fmt.Errorf("запрос не удался после %d попыток: %w", s.maxRetries, err)
}

// naumenMapper реализует интерфейс external.Mapper.
type naumenMapper struct {
	db       *gorm.DB
	linkRepo repositories.LinkRepo
	logger   logger.LoggerInterface
}

// newNaumenMapper создает новый экземпляр маппера для Naumen.
func newNaumenMapper(db *gorm.DB, linkRepo repositories.LinkRepo, logger logger.LoggerInterface) external.Mapper {
	return &naumenMapper{
		db:       db,
		linkRepo: linkRepo,
		logger:   logger,
	}
}

// --- СПЕЦИФИЧНЫЕ ДЛЯ NAUMEN КОНСТАНТЫ ---
const (
	metaClassCompany     = "ou$company"
	metaClassServer      = "objectBase$Server"
	metaClassWorkstation = "objectBase$Workstation"
	metaClassFR          = "objectBase$FR"
	metaClassAgreement   = "agreement$agreement"
)

// DataToCompany преобразует мапу от Naumen в модель Company.
func (m *naumenMapper) DataToCompany(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Company, error) {
	company := &models.Company{}
	company.MetaClass = metaClassCompany

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
			parentInternalID, err := m.linkRepo.FindInternalIDByExternalID(ctx, m.db, "naumen", parentUUID)
			if err != nil {
				return nil, fmt.Errorf("ошибка поиска внутреннего ID родителя %s: %w", parentUUID, err)
			}
			if parentInternalID != "" {
				company.ParentID = &parentInternalID
			} else {
				m.logger.Warn("Родительская компания не найдена в локальной БД, связь не будет установлена", "parent_external_uuid", parentUUID)
			}
		}
	}

	initialActiveState := false
	company.ActiveContract = &initialActiveState
	return company, nil
}

// getOwnerInternalID - хелпер для получения внутреннего ID владельца.
func (m *naumenMapper) getOwnerInternalID(ctx context.Context, data map[string]interface{}) (string, error) {
	if owner, ok := data["owner"].(map[string]interface{}); ok {
		if ownerUUID, oOk := owner["UUID"].(string); oOk {
			internalID, err := m.linkRepo.FindInternalIDByExternalID(ctx, m.db, "naumen", ownerUUID)
			if err != nil {
				return "", fmt.Errorf("ошибка поиска внутреннего ID владельца %s: %w", ownerUUID, err)
			}
			if internalID == "" {
				return "", fmt.Errorf("владелец с внешним ID %s не найден в локальной БД", ownerUUID)
			}
			return internalID, nil
		}
	}
	return "", fmt.Errorf("данные о владельце отсутствуют или некорректны")
}

// DataToServer преобразует мапу от Naumen в модель Server.
func (m *naumenMapper) DataToServer(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Server, error) {
	ownerInternalID, err := m.getOwnerInternalID(ctx, data)
	if err != nil {
		externalUUID, _ := data["UUID"].(string)
		return nil, fmt.Errorf("сервер (ext: %s): %w", externalUUID, err)
	}
	server := &models.Server{OwnerID: &ownerInternalID}
	server.MetaClass = metaClassServer

	rawUniqueID, _ := data["UniqueID"].(string)
	rawTeamviewer, _ := data["Teamviewer"].(string)
	rawRDP, _ := data["RDP"].(string)
	rawAnydesk, _ := data["AnyDesk"].(string)
	rawIP, _ := data["IP"].(string)
	rawDeviceName, _ := data["DeviceName"].(string)
	rawIikoVersion, _ := data["iikoVersion"].(string)
	rawDescription, _ := data["description"].(string)
	rawNameForClient, _ := data["nameforclient"].(string)
	rawLitemanager, _ := data["litemanagerID"].(string)

	server.UniqueID = validators.ValidateUniqueID(rawUniqueID)
	server.Teamviewer = validators.ValidateRemoteAccessID(rawTeamviewer)
	server.Anydesk = validators.ValidateRemoteAccessID(rawAnydesk)
	server.IP = validators.ValidateIPAddress(rawIP)

	if rawRDP != "" {
		server.RDP = &rawRDP
	}
	if rawDeviceName != "" {
		server.DeviceName = &rawDeviceName
	}
	if rawIikoVersion != "" {
		server.ServerVersion = &rawIikoVersion
	}

	var descriptionParts []string
	if rawNameForClient != "" {
		descriptionParts = append(descriptionParts, "Имя для клиента: "+rawNameForClient)
	}
	if rawDescription != "" {
		descriptionParts = append(descriptionParts, "Описание: "+rawDescription)
	}
	// добавление остальных полей в description
	fullDescription := strings.Join(descriptionParts, " | ")
	if fullDescription != "" {
		server.Description = &fullDescription
	}

	if lmd, ok := data["lastModifiedDate"].(string); ok {
		server.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}

	if rawLitemanager != "" && validators.LiteManagerIDRegex.MatchString(rawLitemanager) {
		server.Litemanager = &rawLitemanager
	} else {
		server.Litemanager = validators.ExtractLiteManagerID(data, fullDescription)
	}

	if cl, ok := data["CabinetLink"].(string); ok && server.IP != nil {
		companyType := validators.DetermineCompanyTypeFromIP(*server.IP)
		link := validators.ValidateCabinetLink(cl, companyType)
		server.CabinetLink = &link
	}
	return server, nil
}

// DataToWorkstation преобразует мапу от Naumen в модель Workstation.
func (m *naumenMapper) DataToWorkstation(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Workstation, error) {
	ownerInternalID, err := m.getOwnerInternalID(ctx, data)
	if err != nil {
		externalUUID, _ := data["UUID"].(string)
		return nil, fmt.Errorf("рабочая станция (ext: %s): %w", externalUUID, err)
	}
	ws := &models.Workstation{OwnerID: &ownerInternalID}
	ws.MetaClass = metaClassWorkstation
	if tv, ok := data["Teamviewer"].(string); ok {
		ws.Teamviewer = validators.ValidateRemoteAccessID(tv)
	}
	if ad, ok := data["AnyDesk"].(string); ok {
		ws.Anydesk = validators.ValidateRemoteAccessID(ad)
	}
	if dn, ok := data["DeviceName"].(string); ok {
		ws.DeviceName = &dn
	}
	if desc, ok := data["Commentariy"].(string); ok {
		ws.Description = &desc
		ws.Litemanager = validators.ExtractLiteManagerID(data, desc)
	}
	if lmd, ok := data["lastModifiedDate"].(string); ok {
		ws.LastModifiedDate = utils.ParseServiceDeskTime(lmd)
	}
	return ws, nil
}

// DataToFiscalRegister преобразует мапу от Naumen в модель FiscalRegister.
func (m *naumenMapper) DataToFiscalRegister(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.FiscalRegister, error) {
	ownerInternalID, err := m.getOwnerInternalID(ctx, data)
	if err != nil {
		externalUUID, _ := data["UUID"].(string)
		return nil, fmt.Errorf("фискальный регистратор (ext: %s): %w", externalUUID, err)
	}
	fr := &models.FiscalRegister{OwnerID: &ownerInternalID}
	fr.MetaClass = metaClassFR

	if val, ok := data["ModelKKT"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.ModelKKT = &title
		}
	} else if val, ok := data["ModelKKT"].(string); ok {
		fr.ModelKKT = &val
	}
	if val, ok := data["FFD"].(map[string]interface{}); ok {
		if title, ok2 := val["title"].(string); ok2 {
			fr.FFD = &title
		}
	} else if val, ok := data["FFD"].(string); ok {
		fr.FFD = &val
	}
	if val, ok := data["FRDownloader"].(string); ok {
		fr.FRDownloader = &val
	}
	if val, ok := data["FRFirmware"].(string); ok {
		fr.FRFirmware = &val
	}
	if val, ok := data["RNKKT"].(string); ok {
		normalizedRNKKT := utils.NormalizeRNKKT(val)
		fr.RNKKT = &normalizedRNKKT
	}
	if val, ok := data["LegalName"].(string); ok {
		matches := innRegex.FindStringSubmatch(val)
		if len(matches) > 1 {
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
	return fr, nil
}

var innRegex = regexp.MustCompile(`ИНН:\s*(\d{10,12})`)

// DataToContract преобразует мапу от Naumen в модель Contract.
func (m *naumenMapper) DataToContract(ctx context.Context, mc *external.MapperContext, data map[string]interface{}) (*models.Contract, error) {
	contract := &models.Contract{}
	contract.MetaClass = metaClassAgreement

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
		for _, serviceItem := range services {
			if serviceMap, smOk := serviceItem.(map[string]interface{}); smOk {
				if title, tOk := serviceMap["title"].(string); tOk {
					serviceTitles = append(serviceTitles, title)
				}
			}
		}
		if servicesJSON, err := json.Marshal(serviceTitles); err == nil {
			contract.Services = datatypes.JSON(servicesJSON)
		}
	}

	contract.ServiceLevel = m.determineServiceLevel(serviceTitles)

	if recipients, ok := data["recipientsOU"].([]interface{}); ok {
		var recipientUUIDs []string
		for _, recipientItem := range recipients {
			if recipientMap, rmOk := recipientItem.(map[string]interface{}); rmOk {
				if recipientUUID, uOk := recipientMap["UUID"].(string); uOk {
					recipientUUIDs = append(recipientUUIDs, recipientUUID)
				}
			}
		}
		if recipientsJSON, err := json.Marshal(recipientUUIDs); err == nil {
			contract.Recipients = datatypes.JSON(recipientsJSON)
		}
	}

	return contract, nil
}

// determineServiceLevel - специфичная для Naumen логика определения уровня сервиса.
func (m *naumenMapper) determineServiceLevel(serviceTitles []string) int {
	serviceSet := make(map[string]struct{})
	for _, title := range serviceTitles {
		serviceSet[title] = struct{}{}
	}
	// Специфичные для Naumen названия услуг
	if _, ok := serviceSet["TS Standard (без выездов)"]; ok {
		return 3
	}
	if _, ok := serviceSet["TS Standard"]; ok {
		return 2
	}
	if _, ok := serviceSet["TS Cloud"]; ok {
		return 1
	}
	if _, ok := serviceSet["Прием на АО"]; ok {
		return 0
	}
	if _, ok := serviceSet["Разовое обращение"]; ok {
		return 5
	}
	if _, ok := serviceSet["Базовая услуга"]; ok {
		return 5
	}
	return -1
}

// GetCompanyUUIDsFromContract - специфичная для Naumen логика извлечения UUID.
func (m *naumenMapper) GetCompanyUUIDsFromContract(data map[string]interface{}) []string {
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

// Вспомогательная функция для маппинга.
func (s *naumenClientImpl) mapEntityTypeToMetaClass(entityType string) (string, bool) {
	switch entityType {
	case "Company":
		return "ou$company", true
	case "Server":
		return "objectBase$Server", true
	case "Workstation":
		return "objectBase$Workstation", true
	case "FiscalRegister":
		return "objectBase$FR", true
	case "Contract":
		return "agreement$agreement", true
	case "ModeliFR":
		return "ModeliFR", true
	case "FFD":
		return "FFD", true
	case "SrokiFN":
		return "SrokiFN", true
	default:
		return "", false
	}
}

type HttpError struct {
	StatusCode int
	Message    string
}

func (e *HttpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message) }
func NewHttpError(code int, message string) *HttpError {
	return &HttpError{StatusCode: code, Message: message}
}
