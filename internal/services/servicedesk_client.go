package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// Атрибуты для запроса сущностей, как указано в требованиях.
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
}

// serviceDeskClientImpl реализует ServiceDeskClient.
type serviceDeskClientImpl struct {
	client     *http.Client
	baseURL    string
	apiKey     string
	limiter    *rate.Limiter
	logger     *zap.Logger
	maxRetries int
}

// NewServiceDeskClient создает новый клиент для ServiceDesk.
func NewServiceDeskClient(cfg *config.Config, logger *zap.Logger) ServiceDeskClient {
	// ИЗМЕНЕНИЕ: Детальная настройка транспорта для http.Client
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // Таймаут на установку TCP-соединения
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
			Timeout:   cfg.RequestTimeout, // Общий таймаут на весь запрос
		},
		baseURL:    strings.TrimRight(cfg.ServiceDeskBaseURL, "/"),
		apiKey:     cfg.ServiceDeskKey,
		limiter:    rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		logger:     logger,
		maxRetries: cfg.MaxRetries,
	}
}

// AgreementContextKey - тип ключа для передачи кэша через контекст.
type AgreementContextKey string

const agreementCacheKey AgreementContextKey = "agreementCache"

// FetchAgreementDetails получает детальную информацию о контракте по UUID.
// ИЗМЕНЕНИЕ: Логика кэширования теперь работает через контекст.
func (s *serviceDeskClientImpl) FetchAgreementDetails(ctx context.Context, agreementUUID string) (*AgreementDetailsDTO, error) {
	// 1. Проверяем кэш в контексте
	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		if cachedDetails, found := cache[agreementUUID]; found {
			s.logger.Debug("Детали контракта взяты из кэша контекста", zap.String("agreementUUID", agreementUUID))
			return cachedDetails, nil
		}
	}

	// 2. Если в кэше нет, делаем запрос
	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=state,stateStartTime,services,recipientsOU", s.baseURL, agreementUUID, s.apiKey)

	var response AgreementDetailsDTO
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
	if err != nil {
		return nil, err
	}

	// 3. Сохраняем в кэш контекста, если он есть
	if cache, ok := ctx.Value(agreementCacheKey).(map[string]*AgreementDetailsDTO); ok {
		cache[agreementUUID] = &response
		s.logger.Debug("Детали контракта получены по API и сохранены в кэш контекста", zap.String("agreementUUID", agreementUUID))
	}

	return &response, nil
}

// FetchEntityList получает список сущностей указанного метакласса.
func (s *serviceDeskClientImpl) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	attrs := minimalAttrsMap[metaClass]
	if full {
		attrs = attrsMap[metaClass]
	}

	// Все параметры в URL. Тело запроса будет пустым.
	url := fmt.Sprintf("%s/find/%s?accessKey=%s&attrs=%s", s.baseURL, metaClass, s.apiKey, attrs)

	var responseList []map[string]interface{}

	// Передаем nil в качестве тела запроса.
	err := s.doWithRetry(ctx, http.MethodPost, url, nil, &responseList)
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

	url := fmt.Sprintf("%s/get/%s?accessKey=%s&attrs=%s", s.baseURL, uuid, s.apiKey, attrs)

	var response map[string]interface{}
	err := s.doWithRetry(ctx, http.MethodGet, url, nil, &response)
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
func (s *serviceDeskClientImpl) doWithRetry(ctx context.Context, method, url string, body io.Reader, target interface{}) error {
	var err error
	for i := 0; i < s.maxRetries; i++ {
		if err = s.limiter.Wait(ctx); err != nil {
			return err // Контекст отменен
		}

		req, reqErr := http.NewRequestWithContext(ctx, method, url, body)
		if reqErr != nil {
			return fmt.Errorf("failed to create request: %w", reqErr)
		}
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, doErr := s.client.Do(req)
		if doErr != nil {
			err = fmt.Errorf("request failed: %w", doErr)
			s.logger.Warn("Request failed, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond) // Экспоненциальная задержка
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			bodyBytes, _ := io.ReadAll(resp.Body)
			err = fmt.Errorf("service desk api error: status %d, body: %s", resp.StatusCode, string(bodyBytes))
			if resp.StatusCode < 500 { // 4xx ошибки не повторяем
				return err
			}
			s.logger.Warn("Server error from ServiceDesk, retrying...", zap.Error(err), zap.Int("attempt", i+1))
			time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
			continue
		}

		if decodeErr := json.NewDecoder(resp.Body).Decode(target); decodeErr != nil {
			return fmt.Errorf("failed to decode response: %w", decodeErr)
		}

		return nil // Успех
	}
	return fmt.Errorf("request failed after %d retries: %w", s.maxRetries, err)
}
