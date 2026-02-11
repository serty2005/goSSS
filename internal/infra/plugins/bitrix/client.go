package bitrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Deal struct {
	ID         int64
	OriginID   string
	Originator string
	StageID    string
	CategoryID int
	Title      string
	Raw        map[string]interface{}
}

type TimelineComment struct {
	ID       int64
	Comment  string
	AuthorID *int64
	Raw      map[string]interface{}
}

type ListElement struct {
	ID      int64
	Name    string
	RawJSON string
}

type User struct {
	ID         int64
	Name       string
	Active     bool
	LastName   string
	FirstName  string
	SecondName string
	Email      string
	Phone      string
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     logger.LoggerInterface
}

func NewClient(cfg *config.Config, log logger.LoggerInterface) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BitrixBaseURL), "/") + "/",
		httpClient: &http.Client{
			Timeout: cfg.RequestTimeout,
		},
		logger: log,
	}
}

func (c *Client) IsConfigured() bool {
	return c.baseURL != "/"
}

func (c *Client) DealListByOrigin(ctx context.Context, originatorID, originID string) ([]Deal, error) {
	body := map[string]interface{}{
		"filter": map[string]interface{}{
			"ORIGINATOR_ID": originatorID,
			"ORIGIN_ID":     originID,
		},
		"select": []string{"ID", "ORIGINATOR_ID", "ORIGIN_ID", "STAGE_ID", "CATEGORY_ID", "TITLE"},
	}
	raw, _, err := c.call(ctx, "crm.deal.list", body)
	if err != nil {
		return nil, err
	}
	return parseDeals(raw), nil
}

func (c *Client) DealListByOriginator(ctx context.Context, originatorID string, start int) ([]Deal, int, error) {
	body := map[string]interface{}{
		"filter": map[string]interface{}{
			"ORIGINATOR_ID": originatorID,
		},
		"select": []string{"ID", "ORIGINATOR_ID", "ORIGIN_ID", "STAGE_ID", "CATEGORY_ID", "TITLE"},
		"start":  start,
	}
	raw, next, err := c.call(ctx, "crm.deal.list", body)
	if err != nil {
		return nil, 0, err
	}
	return parseDeals(raw), next, nil
}

func (c *Client) DealAdd(ctx context.Context, fields map[string]interface{}) (int64, error) {
	raw, _, err := c.call(ctx, "crm.deal.add", map[string]interface{}{"fields": fields})
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) DealUpdate(ctx context.Context, dealID int64, fields map[string]interface{}) error {
	_, _, err := c.call(ctx, "crm.deal.update", map[string]interface{}{
		"id":     dealID,
		"fields": fields,
	})
	return err
}

func (c *Client) TimelineCommentAdd(ctx context.Context, dealID int64, comment string, authorID *int64) (int64, error) {
	body := map[string]interface{}{
		"fields": map[string]interface{}{
			"ENTITY_ID":   dealID,
			"ENTITY_TYPE": "deal",
			"COMMENT":     comment,
		},
	}
	if authorID != nil && *authorID > 0 {
		bodyFields := body["fields"].(map[string]interface{})
		bodyFields["AUTHOR_ID"] = *authorID
	}
	raw, _, err := c.call(ctx, "crm.timeline.comment.add", body)
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) TimelineCommentList(ctx context.Context, dealID int64, start int) ([]TimelineComment, int, error) {
	body := map[string]interface{}{
		"filter": map[string]interface{}{
			"ENTITY_TYPE": "deal",
			"ENTITY_ID":   dealID,
		},
		"start": start,
	}
	raw, next, err := c.call(ctx, "crm.timeline.comment.list", body)
	if err != nil {
		return nil, 0, err
	}

	items, ok := raw.([]interface{})
	if !ok {
		return []TimelineComment{}, next, nil
	}

	out := make([]TimelineComment, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id := toInt64(m["ID"])
		comment, _ := m["COMMENT"].(string)
		author := toInt64(m["AUTHOR_ID"])
		var authorPtr *int64
		if author > 0 {
			authorPtr = &author
		}
		out = append(out, TimelineComment{
			ID:       id,
			Comment:  comment,
			AuthorID: authorPtr,
			Raw:      m,
		})
	}
	return out, next, nil
}

func (c *Client) ListsGetIblockTypeID(ctx context.Context, iblockID int) (string, error) {
	raw, _, err := c.call(ctx, "lists.get.iblock.type.id", map[string]interface{}{"IBLOCK_ID": iblockID})
	if err != nil {
		return "", err
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v), nil
	case float64:
		return strconv.FormatInt(int64(v), 10), nil
	default:
		return "", errors.New("РїСѓСЃС‚РѕР№ IBLOCK_TYPE_ID РІ РѕС‚РІРµС‚Рµ Bitrix24")
	}
}

func (c *Client) ListsElementGet(ctx context.Context, iblockTypeID string, iblockID int, start int) ([]ListElement, int, error) {
	raw, next, err := c.call(ctx, "lists.element.get", map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"start":          start,
	})
	if err != nil {
		return nil, 0, err
	}
	items, ok := raw.([]interface{})
	if !ok {
		return []ListElement{}, next, nil
	}
	result := make([]ListElement, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		rawJSON, _ := json.Marshal(m)
		result = append(result, ListElement{
			ID:      toInt64(m["ID"]),
			Name:    strings.TrimSpace(toString(m["NAME"])),
			RawJSON: string(rawJSON),
		})
	}
	return result, next, nil
}

func (c *Client) UserGet(ctx context.Context, start int) ([]User, int, error) {
	raw, next, err := c.call(ctx, "user.get", map[string]interface{}{
		"start": start,
	})
	if err != nil {
		return nil, 0, err
	}
	items, ok := raw.([]interface{})
	if !ok {
		return []User{}, next, nil
	}
	out := make([]User, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id := toInt64(m["ID"])
		if id == 0 {
			continue
		}
		out = append(out, User{
			ID:         id,
			Name:       strings.TrimSpace(toString(m["NAME"])),
			Active:     strings.EqualFold(toString(m["ACTIVE"]), "Y"),
			LastName:   strings.TrimSpace(toString(m["LAST_NAME"])),
			FirstName:  strings.TrimSpace(toString(m["NAME"])),
			SecondName: strings.TrimSpace(toString(m["SECOND_NAME"])),
			Email:      strings.TrimSpace(toString(m["EMAIL"])),
			Phone:      strings.TrimSpace(toString(m["PERSONAL_MOBILE"])),
		})
	}
	return out, next, nil
}

type bitrixEnvelope struct {
	Result           interface{} `json:"result"`
	Error            string      `json:"error"`
	ErrorDescription string      `json:"error_description"`
	Next             interface{} `json:"next"`
}

var bitrixWebhookKeyRe = regexp.MustCompile(`(/rest/[^/]+/)([^/]+)(/)`)

func (c *Client) call(ctx context.Context, method string, body map[string]interface{}) (interface{}, int, error) {
	if !c.IsConfigured() {
		return nil, 0, errors.New("не настроен BITRIX_BASE_URL")
	}
	url := c.baseURL + method + ".json"
	redactedURL := redactWebhookURL(url)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, err
	}
	payloadText := string(payload)

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
			time.Sleep(delay)
		}
		c.logger.Info("Bitrix24 запрос", "http_method", http.MethodPost, "method", method, "url", redactedURL, "attempt", attempt+1, "body", payloadText)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			c.logger.Error("Bitrix24 ошибка HTTP-запроса", "method", method, "url", redactedURL, "attempt", attempt+1, "error", err)
			continue
		}

		rawBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			c.logger.Error("Bitrix24 ошибка чтения ответа", "method", method, "url", redactedURL, "attempt", attempt+1, "status_code", resp.StatusCode, "error", readErr)
			continue
		}
		c.logger.Info("Bitrix24 ответ", "method", method, "url", redactedURL, "attempt", attempt+1, "status_code", resp.StatusCode, "body", string(rawBody))

		var env bitrixEnvelope
		if err := json.Unmarshal(rawBody, &env); err != nil {
			lastErr = fmt.Errorf("не удалось распарсить ответ Bitrix24 (%s): %w", method, err)
			c.logger.Error("Bitrix24 ошибка парсинга JSON", "method", method, "url", redactedURL, "attempt", attempt+1, "error", err)
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("временная ошибка Bitrix24 (%s): %d", method, resp.StatusCode)
			continue
		}

		if strings.TrimSpace(env.Error) != "" {
			errText := fmt.Sprintf("%s: %s", strings.TrimSpace(env.Error), strings.TrimSpace(env.ErrorDescription))
			if strings.Contains(strings.ToUpper(env.Error), "QUERY_LIMIT_EXCEEDED") {
				lastErr = errors.New(errText)
				c.logger.Warn("Bitrix24 ограничение запросов", "method", method, "url", redactedURL, "attempt", attempt+1, "error", env.Error, "error_description", env.ErrorDescription)
				continue
			}
			c.logger.Error("Bitrix24 вернул бизнес-ошибку", "method", method, "url", redactedURL, "error", env.Error, "error_description", env.ErrorDescription)
			return nil, 0, errors.New(errText)
		}

		return env.Result, toInt(env.Next), nil
	}

	return nil, 0, fmt.Errorf("вызов Bitrix24 %s завершился ошибкой: %w", method, lastErr)
}

func redactWebhookURL(url string) string {
	if strings.TrimSpace(url) == "" {
		return url
	}
	return bitrixWebhookKeyRe.ReplaceAllString(url, "${1}REDACTED${3}")
}
func parseDeals(raw interface{}) []Deal {
	items, ok := raw.([]interface{})
	if !ok {
		return []Deal{}
	}
	result := make([]Deal, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, Deal{
			ID:         toInt64(m["ID"]),
			OriginID:   strings.TrimSpace(toString(m["ORIGIN_ID"])),
			Originator: strings.TrimSpace(toString(m["ORIGINATOR_ID"])),
			StageID:    strings.TrimSpace(toString(m["STAGE_ID"])),
			CategoryID: toInt(m["CATEGORY_ID"]),
			Title:      strings.TrimSpace(toString(m["TITLE"])),
			Raw:        m,
		})
	}
	return result
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case int:
		return strconv.Itoa(x)
	default:
		return ""
	}
}

func toInt64(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	default:
		return 0
	}
}

func toInt(v interface{}) int {
	return int(toInt64(v))
}
