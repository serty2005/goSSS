// Файл: internal/infra/iiko/client.go
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

// IikoServerInfo структура для парсинга XML-ответа от сервера iikoRMS
type IikoServerInfo struct {
	XMLName     xml.Name `xml:"r"`
	ServerName  string   `xml:"serverName"`
	Version     string   `xml:"version"`
	Edition     string   `xml:"edition"`
	ServerState string   `xml:"serverState"`
}

// IikoClient определяет интерфейс для взаимодействия с iikoRMS API.
type IikoClient interface {
	GetServerMonitoringInfo(ctx context.Context, serverURL string) (*IikoServerInfo, error)
	InstallLicense(ctx context.Context, serverURL, login, password, fallbackPassword, uid string) (bool, error)
}

type iikoClientImpl struct {
	httpClient *http.Client
	logger     logger.LoggerInterface
}

// NewIikoClient создает новый экземпляр клиента для iikoRMS.
func NewIikoClient(timeout time.Duration, logger logger.LoggerInterface) IikoClient {
	return &iikoClientImpl{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger,
	}
}

// GetServerMonitoringInfo получает статус и информацию о сервере.
func (c *iikoClientImpl) GetServerMonitoringInfo(ctx context.Context, serverURL string) (*IikoServerInfo, error) {
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

	// Первая попытка с основным паролем
	success, err := c.fetchAndInstallLicense(ctx, serverURL, login, password, uid, log)
	if err == nil && success {
		return true, nil
	}

	// Проверяем, является ли ошибка ошибкой аутентификации (401/403)
	var httpErr *HttpError
	if asHttpErr, ok := err.(*HttpError); ok {
		httpErr = asHttpErr
	}

	if (httpErr != nil && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden)) && fallbackPassword != "" {
		log.Warn("Первая попытка аутентификации не удалась, пробую с запасным паролем.")
		// Вторая попытка с запасным паролем
		return c.fetchAndInstallLicense(ctx, serverURL, login, fallbackPassword, uid, log)
	}

	return false, err
}

func (c *iikoClientImpl) fetchAndInstallLicense(ctx context.Context, serverURL, login, password, uid string, log logger.LoggerInterface) (bool, error) {
	// 1. Получаем информацию о сервере (версия, редакция)
	info, err := c.getServerInfoXML(ctx, serverURL)
	if err != nil {
		return false, err
	}

	// 2. Хэшируем пароль
	hasher := sha1.New()
	hasher.Write([]byte(password))
	passwordHash := hex.EncodeToString(hasher.Sum(nil))

	// 3. Формируем тело и заголовки запроса
	endpoint := fmt.Sprintf("%s/resto/services/licensing?methodName=fetchAndInstallLicense&", serverURL)
	xmlBody := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<args>
    <entities-version>2</entities-version>
    <client-type>BACK</client-type>
    <enable-warnings>false</enable-warnings>
    <use-raw-entities>true</use-raw-entities>
    <serialNumber>%s</serialNumber>
</args>`, uid)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewBufferString(xmlBody))
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

	// 4. Отправляем запрос
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("ошибка при отправке запроса: %w", err)
	}
	defer resp.Body.Close()

	// 5. Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return false, NewHttpError(resp.StatusCode, fmt.Sprintf("сервер вернул ошибку: %s. Тело ответа: %s", resp.Status, string(responseBody)))
	}

	log.Info("Запрос на установку лицензии успешно выполнен")
	return true, nil
}

// getServerInfoXML получает и подготавливает информацию о сервере для аутентификации.
func (c *iikoClientImpl) getServerInfoXML(ctx context.Context, serverURL string) (*IikoServerInfo, error) {
	info, err := c.GetServerMonitoringInfo(ctx, serverURL)
	if err != nil {
		return nil, err
	}
	// iiko API требует замены 'default' на 'IIKO_RMS' и 'chain' на 'IIKO_CHAIN' в заголовках
	info.Edition = strings.Replace(info.Edition, "default", "IIKO_RMS", -1)
	info.Edition = strings.Replace(info.Edition, "chain", "IIKO_CHAIN", -1)
	return info, nil
}

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
