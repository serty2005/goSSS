package iiko

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// IikoServerInfo структура для парсинга ответа от сервера iikoRMS.
type IikoServerInfo struct {
	XMLName     xml.Name `xml:"r"`
	ServerName  string   `xml:"serverName"`
	Version     string   `xml:"version"`
	Edition     string   `xml:"edition"`
	ServerState string   `xml:"serverState"`
}

// LicenseInfoResponse содержит данные лицензии из XML-ответа iiko.
type LicenseInfoResponse struct {
	XMLName           xml.Name `xml:"result"`
	CrmOrganizationID string   `xml:"licenseInfo>licenseData>r>crmOrganizationId"`
	SerialNumber      string   `xml:"licenseInfo>licenseData>r>serialNumber"`
}

// IikoClient определяет интерфейс для взаимодействия с iikoRMS API.
type IikoClient interface {
	GetServerMonitoringInfo(ctx context.Context, serverURL string) (*IikoServerInfo, error)
	InstallLicense(ctx context.Context, serverURL, login, password, fallbackPassword, uid string) (bool, error)
	GetCRMid(ctx context.Context, serverURL, login, password, fallbackPassword string) (string, error)
}

type iikoClientImpl struct {
	httpClient *http.Client
	logger     logger.LoggerInterface
}

// NewIikoClient создает новый экземпляр клиента для iikoRMS.
func NewIikoClient(timeout time.Duration, logger logger.LoggerInterface) IikoClient {
	return &iikoClientImpl{
		httpClient: &http.Client{Timeout: timeout},
		logger:     logger,
	}
}

// GetServerMonitoringInfo получает статус и информацию о сервере.
func (c *iikoClientImpl) GetServerMonitoringInfo(ctx context.Context, serverURL string) (*IikoServerInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/resto/get_server_info.jsp?encoding=UTF-8", serverURL), nil)
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

	var info IikoServerInfo
	if err := xml.Unmarshal(body, &info); err != nil {
		c.logger.Warn("Не удалось распарсить XML, попытка распарсить как JSON", "server_url", serverURL, "error", err)
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

// InstallLicense устанавливает лицензию на сервер iikoRMS.
func (c *iikoClientImpl) InstallLicense(ctx context.Context, serverURL, login, password, fallbackPassword, uid string) (bool, error) {
	log := c.logger.With("server_url", serverURL, "uid", uid)

	success, err := c.fetchAndInstallLicense(ctx, serverURL, login, password, uid, log)
	if err == nil && success {
		return true, nil
	}

	var httpErr *HttpError
	if asHTTP, ok := err.(*HttpError); ok {
		httpErr = asHTTP
	}

	if (httpErr != nil && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)) && fallbackPassword != "" {
		log.Warn("Первая попытка аутентификации не удалась, пробую с запасным паролем")
		return c.fetchAndInstallLicense(ctx, serverURL, login, fallbackPassword, uid, log)
	}

	return false, err
}

// GetCRMid получает CRM ID сервера iikoRMS.
func (c *iikoClientImpl) GetCRMid(ctx context.Context, serverURL, login, password, fallbackPassword string) (string, error) {
	log := c.logger.With("server_url", serverURL)

	crmid, err := c.fetchCRMid(ctx, serverURL, login, password)
	if err == nil {
		return crmid, nil
	}

	var httpErr *HttpError
	if asHTTP, ok := err.(*HttpError); ok {
		httpErr = asHTTP
	}

	if (httpErr != nil && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)) && fallbackPassword != "" {
		log.Warn("Первая попытка аутентификации для получения CRM ID не удалась, пробую с запасным паролем")
		return c.fetchCRMid(ctx, serverURL, login, fallbackPassword)
	}

	return "", err
}

func (c *iikoClientImpl) fetchAndInstallLicense(ctx context.Context, serverURL, login, password, uid string, log logger.LoggerInterface) (bool, error) {
	info, err := c.getServerInfoXML(ctx, serverURL)
	if err != nil {
		return false, err
	}

	hasher := sha1.New()
	hasher.Write([]byte(password))
	passwordHash := hex.EncodeToString(hasher.Sum(nil))

	endpoint := fmt.Sprintf("%s/resto/services/licensing?methodName=fetchAndInstallLicense&", serverURL)
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<args>
    <entities-version>2</entities-version>
    <client-type>BACK</client-type>
    <enable-warnings>false</enable-warnings>
    <use-raw-entities>true</use-raw-entities>
    <serialNumber>%s</serialNumber>
</args>`, uid)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(xmlBody))
	if err != nil {
		return false, fmt.Errorf("не удалось создать POST-запрос: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-Resto-LoginName", login)
	req.Header.Set("X-Resto-PasswordHash", passwordHash)
	req.Header.Set("X-Resto-BackVersion", info.Version)
	req.Header.Set("X-Resto-AuthType", "BACK")
	req.Header.Set("X-Resto-ServerEdition", info.Edition)

	log.Info("Отправка запроса на установку лицензии на iiko-сервер")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("ошибка при отправке запроса: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return false, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку: %s. Тело ответа: %s", resp.Status, string(responseBody)))
	}

	log.Info("Запрос на установку лицензии успешно выполнен")
	return true, nil
}

func (c *iikoClientImpl) fetchCRMid(ctx context.Context, serverURL, login, password string) (string, error) {
	info, err := c.getServerInfoXML(ctx, serverURL)
	if err != nil {
		return "", err
	}

	hasher := sha1.New()
	hasher.Write([]byte(password))
	passwordHash := hex.EncodeToString(hasher.Sum(nil))

	endpoint := fmt.Sprintf("%s/resto/services/licensing?methodName=getForceDeveloperSandboxModeInfo&", serverURL)
	xmlBody := `<?xml version="1.0" encoding="utf-8"?><args><entities-version>1</entities-version><client-type>BACK</client-type><enable-warnings>false</enable-warnings><client-call-id>30264dfd-570d-46c0-81b8-6bef9da5a2c9</client-call-id><license-hash>-1938788177</license-hash><restrictions-state-hash>5761</restrictions-state-hash><obtained-license-connections-ids /><request-watchdog-check-results>true</request-watchdog-check-results><use-raw-entities>true</use-raw-entities></args>`

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(xmlBody))
	if err != nil {
		return "", fmt.Errorf("не удалось создать POST-запрос: %w", err)
	}

	req.Header.Set("Content-Type", "text/xml")
	req.Header.Set("X-Resto-LoginName", login)
	req.Header.Set("X-Resto-PasswordHash", passwordHash)
	req.Header.Set("X-Resto-BackVersion", info.Version)
	req.Header.Set("X-Resto-AuthType", "BACK")
	req.Header.Set("X-Resto-ServerEdition", info.Edition)

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

	var licenseInfo LicenseInfoResponse
	cleanXML := strings.ReplaceAll(string(responseBody), "&lt;", "<")
	cleanXML = strings.ReplaceAll(cleanXML, "&gt;", ">")

	if err := xml.Unmarshal([]byte(cleanXML), &licenseInfo); err != nil {
		return "", fmt.Errorf("не удалось разобрать XML-ответ с лицензией: %w. Ответ: %s", err, string(responseBody))
	}

	if licenseInfo.CrmOrganizationID == "" {
		return "", fmt.Errorf("не удалось найти CRMid в ответе сервера. Ответ: %s", string(responseBody))
	}

	return licenseInfo.CrmOrganizationID, nil
}

// getServerInfoXML получает и подготавливает информацию о сервере для аутентификации.
func (c *iikoClientImpl) getServerInfoXML(ctx context.Context, serverURL string) (*IikoServerInfo, error) {
	info, err := c.GetServerMonitoringInfo(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	info.Edition = strings.Replace(info.Edition, "default", "IIKO_RMS", -1)
	info.Edition = strings.Replace(info.Edition, "chain", "IIKO_CHAIN", -1)
	return info, nil
}

// HttpError специальный тип ошибки для HTTP-ответов.
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
