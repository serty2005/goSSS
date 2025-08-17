package services

import (
	"bytes"
	"context"
	"encoding/json"
	"etalon-server/internal/config"
	"fmt"
	"io"
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

// ServiceDeskClient определяет интерфейс для взаимодействия с API ServiceDesk.
type ServiceDeskClient interface {
	FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error)
	FetchEntityDetails(ctx context.Context, uuid string, metaClass string) (map[string]interface{}, error)
	CheckAgreementActive(ctx context.Context, agreementUUID string) (bool, error)
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
	return &serviceDeskClientImpl{
		client: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		baseURL:    strings.TrimRight(cfg.ServiceDeskBaseURL, "/"),
		apiKey:     cfg.ServiceDeskKey,
		limiter:    rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
		logger:     logger,
		maxRetries: cfg.MaxRetries,
	}
}

// FetchEntityList получает список сущностей указанного метакласса.
func (s *serviceDeskClientImpl) FetchEntityList(ctx context.Context, metaClass string, full bool) ([]map[string]interface{}, error) {
	attrs := minimalAttrsMap[metaClass]
	if full {
		attrs = attrsMap[metaClass]
	}

	url := fmt.Sprintf("%s/find/%s", s.baseURL, metaClass)
	body := map[string]string{
		"accessKey": s.apiKey,
		"attrs":     attrs,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	// ИСПРАВЛЕНИЕ: Мы ожидаем в ответе массив, а не объект
	var responseList []map[string]interface{}

	err = s.doWithRetry(ctx, http.MethodPost, url, bytes.NewReader(payload), &responseList)
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
