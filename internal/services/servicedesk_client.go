package services

import (
	"bytes"
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

var attrsMap = map[string]string{
	"ou$company":             "adress,UUID,title,lastModifiedDate,additionalName,parent,recipientAgreements",
	"objectBase$Server":      "UniqueID,Teamviewer,RDP,AnyDesk,UUID,IP,CabinetLink,DeviceName,lastModifiedDate,iikoVersion,description,nameforclient,owner,litemanagerID",
	"objectBase$Workstation": "Commentariy,Teamviewer,AnyDesk,DeviceName,litemanagerID,lastModifiedDate,UUID,owner",
	"objectBase$FR":          "UUID,ModelKKT,lastModifiedDate,owner,FFD,FRDownloader,RNKKT,KKTRegDate,FNExpireDate,LegalName,FRSerialNumber,FNNumber",
}

var minimalAttrsMap = map[string]string{
	"ou$company":             "UUID,lastModifiedDate,parent,recipientAgreements",
	"objectBase$Server":      "UUID,lastModifiedDate,owner",
	"objectBase$Workstation": "UUID,lastModifiedDate,owner",
	"objectBase$FR":          "UUID,lastModifiedDate,owner",
}

// AgreementDetailsDTO содержит детали контракта, полученные от ServiceDesk.
type AgreementDetailsDTO struct {
	State          string `json:"state"`
	StateStartTime string `json:"stateStartTime"`
	Services       []struct {
		UUID      string `json:"UUID"`
		Title     string `json:"title"`
		MetaClass string `json:"metaClass"`
	} `json:"services"`
	RecipientsOU []struct {
		UUID      string `json:"UUID"`
		Title     string `json:"title"`
		MetaClass string `json:"metaClass"`
	} `json:"recipientsOU"`
}

// ServiceDeskClient определяет интерфейс для взаимодействия с API ServiceDesk.
type ServiceDeskClient interface {
	FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error)
	FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error)
	FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error)
	UpdateEntity(ctx context.Context, metaClass, uuid string, data map[string]interface{}) error
	CreateEntity(ctx context.Context, metaClass string, data map[string]interface{}) (map[string]interface{}, error)
	FindReferenceID(ctx context.Context, metaClass, title string, useSubstringSearch bool) (string, error)
}

// serviceDeskClientImpl реализует ServiceDeskClient.
type serviceDeskClientImpl struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	limiter    *rate.Limiter
	logger     *zap.Logger
	maxRetries int
	dryRun     bool

	referenceCache map[string]string
	cacheMutex     sync.RWMutex
}

// NewServiceDeskClient создает новый клиент для ServiceDesk.
func NewServiceDeskClient(cfg *config.Config, logger *zap.Logger) ServiceDeskClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	return &serviceDeskClientImpl{
		client: &http.Client{
			Transport: transport,
			Timeout:   cfg.RequestTimeout,
		},
		baseURL:        strings.TrimRight(cfg.ServiceDeskBaseURL, "/"),
		apiKey:         cfg.ServiceDeskKey,
		limiter:        rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		logger:         logger,
		maxRetries:     cfg.MaxRetries,
		dryRun:         cfg.ServiceDeskDryRun,
		referenceCache: make(map[string]string),
	}
}

// FindReferenceID находит ID в справочнике ServiceDesk.
// Возвращает ТОЛЬКО ЧИСТЫЙ UUID (например, "2645001"), а не "FFD$2645001".
func (s *serviceDeskClientImpl) FindReferenceID(ctx context.Context, metaClass, title string, useSubstringSearch bool) (string, error) {
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
					zap.String("metaClass", metaClass), zap.String("title", title),
					zap.String("found1", foundTitle), zap.String("found2", itemTitle))
				break
			}
			foundUUID = itemUUID
			foundTitle = itemTitle
		}
	}

	if foundUUID == "" {
		return "", fmt.Errorf("не найдено значение '%s' в справочнике %s", title, metaClass)
	}

	// Сохраняем в кеш и возвращаем ЧИСТЫЙ UUID.
	s.cacheMutex.Lock()
	s.referenceCache[cacheKey] = foundUUID
	s.cacheMutex.Unlock()

	return foundUUID, nil
}

// AgreementContextKey - тип ключа для передачи кэша через контекст.
type AgreementContextKey string

const agreementCacheKey AgreementContextKey = "agreementCache"

// FetchAgreementDetails получает детальную информацию о контракте по UUID.
func (s *serviceDeskClientImpl) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error) {
	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		if cachedDetails, found := cache[agreementUUID]; found {
			s.logger.Debug("Детали контракта взяты из кэша контекста", zap.String("agreementUUID", agreementUUID))
			return cachedDetails, nil
		}
	}

	url := fmt.Sprintf("%s/get/%s", s.baseURL, agreementUUID)
	params := map[string]string{"attrs": "state,stateStartTime,services,recipientsOU"}

	var response AgreementDetailsDTO
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response, params)
	if err != nil {
		return nil, err
	}

	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		cache[agreementUUID] = &response
	}

	return &response, nil
}

// FetchEntityList получает список сущностей указанного метакласса.
func (s *serviceDeskClientImpl) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	attrs := minimalAttrsMap[metaClass]
	if full {
		attrs = attrsMap[metaClass]
	}

	// ИСПРАВЛЕНО: Убран accessKey из URL
	url := fmt.Sprintf("%s/find/%s", s.baseURL, metaClass)
	params := map[string]string{"attrs": attrs}

	var responseList []map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &responseList, params)
	if err != nil {
		return nil, err
	}

	return responseList, nil
}

// FetchEntityDetails получает детальную информацию о сущности по UUID.
func (s *serviceDeskClientImpl) FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error) {
	attrs, ok := attrsMap[metaClass]
	if !ok {
		return nil, fmt.Errorf("unknown metaclass: %s", metaClass)
	}

	// ИСПРАВЛЕНО: Убран accessKey из URL
	url := fmt.Sprintf("%s/get/%s", s.baseURL, uuid)
	params := map[string]string{"attrs": attrs}

	var response map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response, params)
	if err != nil {
		return nil, err
	}

	return response, nil
}

// CheckAgreementActive проверяет, активен ли договор.
func (s *serviceDeskClientImpl) CheckAgreementActive(ctx context.Context, agreementUUID string) (bool, error) {
	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=state,UUID", s.baseURL, agreementUUID, s.apiKey)

	var response struct {
		State string `json:"state"`
	}

	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
	if err != nil {
		return false, err
	}

	return response.State == "active", nil
}

// doWithRetry выполняет HTTP-запрос с политикой повторов.
func (s *serviceDeskClientImpl) doWithRetry(ctx context.Context, method, url string, body io.Reader, target interface{}, queryParams ...map[string]string) error {
	var bodyBytes []byte
	var err error

	// Для логирования нам нужно прочитать тело. Так как io.Reader одноразовый,
	// мы читаем его в байты, а затем создаем новый Reader для запроса.
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

		// Создаем новый reader из байтов на каждой итерации
		var requestBody io.Reader
		if bodyBytes != nil {
			requestBody = bytes.NewBuffer(bodyBytes)
		}

		req, reqErr := http.NewRequestWithContext(ctx, method, url, requestBody)
		if reqErr != nil {
			return fmt.Errorf("failed to create request: %w", reqErr)
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

		// --- УЛУЧШЕННОЕ ЛОГИРОВАНИЕ ---
		s.logger.Debug("Отправка запроса в ServiceDesk",
			zap.String("method", method),
			zap.String("url", req.URL.String()),
			zap.String("body", string(bodyBytes)),
		)

		resp, doErr := s.client.Do(req)
		if doErr != nil {
			err = fmt.Errorf("request failed: %w", doErr)
			s.logger.Warn("Request failed, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			respBodyBytes, _ := io.ReadAll(resp.Body)
			bodyString := string(respBodyBytes)
			err = fmt.Errorf("service desk api error: status %d, body: %s", resp.StatusCode, bodyString)

			if resp.StatusCode == 500 && strings.Contains(bodyString, "ключ авторизации") && strings.Contains(bodyString, "не найден") {
				s.logger.Fatal("Критическая ошибка: ключ доступа ServiceDesk невалиден. Проверьте конфигурацию.",
					zap.String("used_key", s.apiKey),
					zap.String("sd_response", bodyString),
				)
			}

			if resp.StatusCode < 500 {
				return err
			}
			s.logger.Warn("Server error from ServiceDesk, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if target != nil {
			if resp.StatusCode != http.StatusNoContent && resp.ContentLength != 0 {
				if decodeErr := json.NewDecoder(resp.Body).Decode(target); decodeErr != nil {
					return fmt.Errorf("failed to decode response: %w", decodeErr)
				}
			}
		}

		return nil
	}
	return fmt.Errorf("request failed after %d retries: %w", s.maxRetries, err)
}

// UpdateEntity обновляет существующую сущность в ServiceDesk.
func (s *serviceDeskClientImpl) UpdateEntity(ctx context.Context, metaClass, uuid string, data map[string]interface{}) error {
	if len(data) == 0 {
		return nil
	}

	if s.dryRun {
		// Сообщение стало более общим
		s.logger.Warn("[DRY RUN] Отправка запроса на ОБНОВЛЕНИЕ в ServiceDesk пропущена.",
			zap.String("metaClass", metaClass),
			zap.String("uuid", uuid),
		)
		return nil
	}

	entityShortName := strings.Split(metaClass, "$")[1]
	fullUUID := fmt.Sprintf("%s$%s", entityShortName, uuid)
	url := fmt.Sprintf("%s/update/%s", s.baseURL, fullUUID)

	bodyBytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("ошибка сериализации данных: %w", err)
	}

	return s.doWithRetry(ctx, http.MethodPost, url, bytes.NewBuffer(bodyBytes), nil)
}

// CreateEntity создает новую сущность в ServiceDesk.
func (s *serviceDeskClientImpl) CreateEntity(ctx context.Context, metaClass string, data map[string]interface{}) (map[string]interface{}, error) {
	if s.dryRun {
		// Сообщение стало более общим
		s.logger.Warn("[DRY RUN] Отправка запроса на СОЗДАНИЕ в ServiceDesk пропущена.",
			zap.String("metaClass", metaClass),
		)
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
	if err != nil {
		return nil, err
	}
	return response, nil
}
