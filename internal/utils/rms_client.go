// internal/utils/rms_client.go
// internal/utils/rms_client.go
package utils

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

// ServerInfoXML структура для парсинга XML-ответа от сервера iikoRMS
type ServerInfoXML struct {
	XMLName     xml.Name `xml:"r"`
	ServerName  string   `xml:"serverName"`
	Version     string   `xml:"version"`
	Edition     string   `xml:"edition"`
	ServerState string   `xml:"serverState"`
}

// RMSClient определяет интерфейс для взаимодействия с RMS API.
type RMSClient interface {
	GetServerMonitoringInfo(ctx context.Context, serverURL string) (*ServerInfoXML, error)
}

type rmsClientImpl struct {
	httpClient *http.Client
	logger     *zap.Logger
}

// NewRMSClient создает новый экземпляр клиента для RMS.
func NewRMSClient(timeout time.Duration, logger *zap.Logger) RMSClient {
	return &rmsClientImpl{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// GetServerMonitoringInfo получает статус и информацию о сервере.
// ИСПРАВЛЕНО: Теперь в первую очередь парсит XML.
func (c *rmsClientImpl) GetServerMonitoringInfo(ctx context.Context, serverURL string) (*ServerInfoXML, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/resto/get_server_info.jsp?encoding=UTF-8", serverURL), nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать GET-запрос: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось выполнить GET-запрос для получения информации о сервере: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении информации: %s", resp.Status))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ответ от сервера: %w", err)
	}

	var info ServerInfoXML
	if err := xml.Unmarshal(body, &info); err != nil {
		// Попытка fallback на JSON, если XML не удался
		c.logger.Warn("Не удалось распарсить XML, попытка распарсить как JSON", zap.String("server_url", serverURL), zap.Error(err))
		var jsonInfo struct {
			ServerName  string `json:"serverName"`
			Edition     string `json:"edition"`
			Version     string `json:"version"`
			ServerState string `json:"serverState"`
		}
		if jsonErr := json.Unmarshal(body, &jsonInfo); jsonErr == nil {
			info.ServerName = jsonInfo.ServerName
			info.Edition = jsonInfo.Edition
			info.Version = jsonInfo.Version
			info.ServerState = jsonInfo.ServerState
		} else {
			return nil, fmt.Errorf("не удалось разобрать ответ ни как XML, ни как JSON: %w", err)
		}
	}

	return &info, nil
}

/*
 =================================================================================
  ОТНОСИТСЯ К СТАРОЙ ЛОГИКЕ
  ПОЛУЧЕНИЯ CRMID.
  ОН МОЖЕТ ПОНАДОБИТЬСЯ В БУДУЩЕМ ДЛЯ ДРУГИХ ЗАДАЧ.
 =================================================================================

// Структуры для парсинга XML-ответов от сервера iikoRMS
type ServerInfo struct {
	XMLName xml.Name `xml:"r"`
	Version string   `xml:"version"`
	Edition string   `xml:"edition"`
}

type LicenseInfoResponse struct {
	XMLName           xml.Name `xml:"result"`
	CrmOrganizationId string   `xml:"licenseInfo>licenseData>r>crmOrganizationId"`
	SerialNumber      string   `xml:"licenseInfo>licenseData>r>serialNumber"`
}

// GetCRMid подключается к серверу iikoRMS и возвращает его CRMid.
// Поддерживает попытку с fallbackPassword в случае ошибки аутентификации.
func (c *rmsClientImpl) GetCRMid(ctx context.Context, serverURL, login, password, fallbackPassword string) (string, error) {
	log := c.logger.With(zap.String("server_url", serverURL))

	// Первая попытка с основным паролем
	crmid, err := c.fetchCRMid(ctx, serverURL, login, password, log)
	if err == nil {
		return crmid, nil
	}

	// Проверяем, является ли ошибка ошибкой аутентификации (401/403)
	var httpErr *HttpError
	if asHttpErr, ok := err.(*HttpError); ok {
		httpErr = asHttpErr
	}

	if (httpErr != nil && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)) && fallbackPassword != "" {
		log.Warn("Первая попытка аутентификации не удалась, пробую с запасным паролем.")
		// Вторая попытка с запасным паролем
		return c.fetchCRMid(ctx, serverURL, login, fallbackPassword, log)
	}

	return "", err
}

func (c *rmsClientImpl) fetchCRMid(ctx context.Context, serverURL, login, password string, log *zap.Logger) (string, error) {
	// 1. Получаем информацию о сервере (версия, редакция)
	info, err := c.getServerInfoXML(ctx, serverURL)
	if err != nil {
		return "", err
	}

	// 2. Хэшируем пароль по алгоритму SHA1
	hasher := sha1.New()
	hasher.Write([]byte(password))
	passwordHash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Формируем тело и заголовки для запроса
	endpoint := fmt.Sprintf("%s/resto/services/licensing?methodName=getForceDeveloperSandboxModeInfo&", serverURL)
	xmlBody := `<?xml version="1.0" encoding="utf-8"?><args><entities-version>1</entities-version><client-type>BACK</client-type><enable-warnings>false</enable-warnings><client-call-id>30264dfd-570d-46c0-81b8-6bef9da5a2c9</client-call-id><license-hash>-1938788177</license-hash><restrictions-state-hash>5761</restrictions-state-hash><obtained-license-connections-ids /><request-watchdog-check-results>true</request-watchdog-check-results><use-raw-entities>true</use-raw-entities></args>`

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBufferString(xmlBody))
	if err != nil {
		return "", fmt.Errorf("не удалось создать POST-запрос: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-Resto-LoginName", login)
	req.Header.Set("X-Resto-PasswordHash", passwordHash)
	req.Header.Set("X-Resto-BackVersion", info.Version)
	req.Header.Set("X-Resto-AuthType", "BACK")
	req.Header.Set("X-Resto-ServerEdition", info.Edition)

	// 4. Отправляем запрос
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ошибка при отправке запроса на получение CRMid: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать ответ сервера: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении CRMid: %s. Ответ: %s", resp.Status, string(responseBody)))
	}

	// 5. Парсим ответ
	var licenseInfo LicenseInfoResponse
	cleanXML := strings.ReplaceAll(string(responseBody), "&lt;", "<")
	cleanXML = strings.ReplaceAll(cleanXML, "&gt;", ">")

	if err := xml.Unmarshal([]byte(cleanXML), &licenseInfo); err != nil {
		return "", fmt.Errorf("не удалось разобрать XML-ответ с лицензией: %w. Ответ: %s", err, string(responseBody))
	}

	if licenseInfo.CrmOrganizationId == "" {
		return "", fmt.Errorf("не удалось найти CRMid в ответе сервера. Ответ: %s", string(responseBody))
	}

	return licenseInfo.CrmOrganizationId, nil
}

func (c *rmsClientImpl) getServerInfoXML(ctx context.Context, serverURL string) (*ServerInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s/resto/get_server_info.jsp?encoding=UTF-8", serverURL), nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать GET-запрос: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось выполнить GET-запрос для получения информации о сервере: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку при получении информации: %s", resp.Status))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать ответ от сервера: %w", err)
	}

	var info ServerInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("не удалось разобрать XML с информацией о сервере: %w", err)
	}

	info.Edition = strings.Replace(info.Edition, "default", "IIKO_RMS", -1)
	info.Edition = strings.Replace(info.Edition, "chain", "IIKO_CHAIN", -1)

	return &info, nil
}
*/

// HttpError специальный тип ошибки для HTTP-ответов
type HttpError struct {
	StatusCode int
	Message    string
}

func (e *HttpError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.StatusCode, e.Message)
}

func NewHttpError(code int, message string) *HttpError {
	return &HttpError{StatusCode: code, Message: message}
}
