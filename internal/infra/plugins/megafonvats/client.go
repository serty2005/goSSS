package megafonvats

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultListLimit = 100

var errMegafonVATSNotFound = errors.New("ресурс Мегафон ВАТС не найден")

type Client struct {
	configured bool
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     logger.LoggerInterface
}

type User struct {
	Login          string         `json:"login"`
	Name           string         `json:"name"`
	Position       string         `json:"position"`
	Email          string         `json:"email"`
	Ext            string         `json:"ext"`
	Telnum         string         `json:"telnum"`
	Role           string         `json:"role"`
	Mobile         string         `json:"mobile"`
	MobileRedirect MobileRedirect `json:"mobile_redirect"`
	Status         string         `json:"status"`
}

type MobileRedirect struct {
	Enabled bool `json:"enabled"`
	Forward bool `json:"forward"`
	Delay   int  `json:"delay"`
}

type listInfo struct {
	Search string `json:"search"`
	Start  int    `json:"start"`
	Limit  int    `json:"limit"`
	Total  int    `json:"total"`
	Next   *int   `json:"next"`
}

type usersListResponse struct {
	Items []User   `json:"items"`
	Info  listInfo `json:"info"`
}

func NewClient(cfg *config.Config, log logger.LoggerInterface) *Client {
	timeout := 15 * time.Second
	baseURL := ""
	apiKey := ""
	if cfg != nil {
		timeout = cfg.RequestTimeout
		baseURL = normalizeMegafonBaseURL(cfg.MegafonVATSBaseURL)
		apiKey = strings.TrimSpace(cfg.MegafonVATSAPIKey)
	}

	return &Client{
		configured: baseURL != "" && apiKey != "",
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: timeout},
		logger:     log,
	}
}

func (c *Client) IsConfigured() bool {
	return c != nil && c.configured
}

func (c *Client) ListUsers(ctx context.Context, withStatus bool) ([]User, error) {
	if !c.IsConfigured() {
		return nil, errors.New("клиент Мегафон ВАТС не настроен")
	}

	start := 0
	all := make([]User, 0, defaultListLimit)
	for {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(defaultListLimit))
		if start > 0 {
			query.Set("start", strconv.Itoa(start))
		}
		if withStatus {
			query.Set("with", "status")
		}

		var response usersListResponse
		if err := c.doJSON(ctx, http.MethodGet, "users", query, &response); err != nil {
			return nil, err
		}
		all = append(all, response.Items...)

		next := 0
		if response.Info.Next != nil && *response.Info.Next > 0 {
			next = *response.Info.Next
		}
		if next <= 0 {
			if response.Info.Total > 0 && len(all) < response.Info.Total && len(response.Items) == defaultListLimit {
				start += len(response.Items)
				continue
			}
			break
		}
		start = next
	}

	return all, nil
}

func (c *Client) GetUser(ctx context.Context, login string, withStatus bool) (*User, error) {
	if !c.IsConfigured() {
		return nil, errors.New("клиент Мегафон ВАТС не настроен")
	}
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, errors.New("не передан login сотрудника ВАТС")
	}

	query := url.Values{}
	if withStatus {
		query.Set("with", "status")
	}

	var response User
	if err := c.doJSON(ctx, http.MethodGet, "users/"+url.PathEscape(login), query, &response); err != nil {
		if errors.Is(err, errMegafonVATSNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &response, nil
}

func (c *Client) doJSON(ctx context.Context, method string, path string, query url.Values, out any) error {
	endpoint, err := c.buildURL(path, query)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("ошибка HTTP-запроса к Мегафон ВАТС: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("не удалось прочитать ответ Мегафон ВАТС: %w", err)
	}

	if c.logger != nil {
		c.logger.Debug(
			"Мегафон ВАТС API запрос",
			"method", method,
			"url", redactAPIKey(endpoint),
			"status_code", resp.StatusCode,
			"body", string(body),
		)
	}

	if resp.StatusCode == http.StatusNotFound {
		return errMegafonVATSNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Мегафон ВАТС вернул статус %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if len(body) == 0 || out == nil {
		return nil
	}
	if err = json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("не удалось распарсить ответ Мегафон ВАТС: %w", err)
	}
	return nil
}

func (c *Client) buildURL(path string, query url.Values) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	rel, err := url.Parse(strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	full := base.ResolveReference(rel)
	if query != nil {
		full.RawQuery = query.Encode()
	}
	return full.String(), nil
}

func normalizeMegafonBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	value = strings.TrimRight(value, "/")
	if !strings.Contains(strings.ToLower(value), "/crmapi/") {
		value += "/crmapi/v1"
	}
	return value + "/"
}

func redactAPIKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.String()
}
