package bitrix

import (
	"bytes"
	"context"
	"encoding/base64"
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
	AssignedBy *int64
	ModifiedBy *int64
	Raw        map[string]interface{}
}

type TimelineComment struct {
	ID         int64
	Comment    string
	AuthorID   *int64
	EntityType string
	EntityID   int64
	Raw        map[string]interface{}
}

type FileToUpload struct {
	Name          string
	Base64Content string
}

type DiskFile struct {
	ID          int64
	Name        string
	Size        int64
	FileID      int64
	DownloadURL string
	Raw         map[string]interface{}
}

type ListElement struct {
	ID         int64
	Name       string
	Code       string
	Properties map[string]interface{}
	RawJSON    string
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

func (c *Client) DealGet(ctx context.Context, dealID int64) (*Deal, error) {
	raw, _, err := c.call(ctx, "crm.deal.get", map[string]interface{}{
		"id": dealID,
		"select": []string{
			"ID",
			"ORIGINATOR_ID",
			"ORIGIN_ID",
			"STAGE_ID",
			"CATEGORY_ID",
			"TITLE",
			"ASSIGNED_BY_ID",
			"MODIFY_BY_ID",
			"UF_CRM_1766060620",
			"UF_CRM_1766062398",
		},
	})
	if err != nil {
		return nil, err
	}
	item := parseDeal(raw)
	if item == nil {
		return nil, nil
	}
	return item, nil
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
		bodyFields["AUTHOR_ID"] = strconv.FormatInt(*authorID, 10)
	}
	raw, _, err := c.call(ctx, "crm.timeline.comment.add", body)
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) TimelineCommentAddWithFiles(
	ctx context.Context,
	entityType string,
	entityID int64,
	comment string,
	files []FileToUpload,
) (int64, error) {
	body := map[string]interface{}{
		"fields": buildTimelineCommentAddFields(entityType, entityID, comment, files),
	}
	raw, _, err := c.call(ctx, "crm.timeline.comment.add", body)
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) TimelineCommentUpdateWithFiles(ctx context.Context, commentID int64, comment string, files []FileToUpload) error {
	_, _, err := c.call(ctx, "crm.timeline.comment.update", map[string]interface{}{
		"id":     commentID,
		"fields": buildTimelineCommentUpdateFields(comment, files, files != nil),
	})
	return err
}

func (c *Client) TimelineCommentList(ctx context.Context, dealID int64, start int) ([]TimelineComment, int, error) {
	body := map[string]interface{}{
		"filter": map[string]interface{}{
			"ENTITY_TYPE": "deal",
			"ENTITY_ID":   dealID,
		},
		"select": []string{"ID", "COMMENT", "AUTHOR_ID", "ENTITY_TYPE", "ENTITY_ID"},
		"start":  start,
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
			ID:         id,
			Comment:    comment,
			AuthorID:   authorPtr,
			EntityType: strings.TrimSpace(strings.ToLower(toString(m["ENTITY_TYPE"]))),
			EntityID:   toInt64(m["ENTITY_ID"]),
			Raw:        m,
		})
	}
	return out, next, nil
}

func (c *Client) TimelineCommentGet(ctx context.Context, commentID int64) (*TimelineComment, error) {
	raw, _, err := c.call(ctx, "crm.timeline.comment.get", map[string]interface{}{"id": commentID})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	id := toInt64(m["ID"])
	author := toInt64(m["AUTHOR_ID"])
	var authorPtr *int64
	if author > 0 {
		authorPtr = &author
	}
	item := &TimelineComment{
		ID:         id,
		Comment:    strings.TrimSpace(toString(m["COMMENT"])),
		AuthorID:   authorPtr,
		EntityType: strings.TrimSpace(strings.ToLower(toString(m["ENTITY_TYPE"]))),
		EntityID:   toInt64(m["ENTITY_ID"]),
		Raw:        m,
	}
	if item.EntityType == "" {
		item.EntityType = strings.TrimSpace(strings.ToLower(toString(m["BINDINGS_ENTITY_TYPE"])))
	}
	if item.EntityID == 0 {
		item.EntityID = toInt64(m["BINDINGS_ENTITY_ID"])
	}
	return item, nil
}

func (c *Client) DiskFileGet(ctx context.Context, diskFileID int64) (*DiskFile, error) {
	raw, _, err := c.call(ctx, "disk.file.get", map[string]interface{}{"id": diskFileID})
	if err != nil {
		return nil, err
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, nil
	}
	out := &DiskFile{
		ID:          toInt64(m["ID"]),
		Name:        strings.TrimSpace(toString(m["NAME"])),
		Size:        toInt64(m["SIZE"]),
		FileID:      toInt64(m["FILE_ID"]),
		DownloadURL: strings.TrimSpace(toString(m["DOWNLOAD_URL"])),
		Raw:         m,
	}
	if out.ID <= 0 {
		out.ID = toInt64(m["id"])
	}
	if out.Name == "" {
		out.Name = strings.TrimSpace(toString(m["name"]))
	}
	if out.Size <= 0 {
		out.Size = toInt64(m["size"])
	}
	if out.FileID <= 0 {
		out.FileID = toInt64(m["file_id"])
	}
	if out.DownloadURL == "" {
		out.DownloadURL = strings.TrimSpace(toString(m["downloadUrl"]))
	}
	return out, nil
}

func (c *Client) DownloadByURL(ctx context.Context, url string) ([]byte, error) {
	target := strings.TrimSpace(url)
	if target == "" {
		return nil, errors.New("пустой URL для скачивания файла")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("не удалось скачать файл, статус: %d", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return content, nil
}

var timelineCommentDiskFileIDRe = regexp.MustCompile(`\[DISK FILE ID=n?(\d+)\]`)

func ExtractTimelineCommentDiskFileIDs(raw map[string]interface{}) []int64 {
	if len(raw) == 0 {
		return []int64{}
	}
	collected := make([]int64, 0, 8)

	if filesRaw, ok := raw["FILES"]; ok {
		collected = append(collected, extractDiskFileIDsFromFilesField(filesRaw)...)
	}

	comment := strings.TrimSpace(toString(raw["COMMENT"]))
	if comment != "" {
		matches := timelineCommentDiskFileIDRe.FindAllStringSubmatch(comment, -1)
		for _, m := range matches {
			if len(m) < 2 {
				continue
			}
			id, err := strconv.ParseInt(strings.TrimSpace(m[1]), 10, 64)
			if err != nil || id <= 0 {
				continue
			}
			collected = append(collected, id)
		}
	}

	return dedupeInt64(collected)
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
		return "", errors.New("пустой IBLOCK_TYPE_ID в ответе Bitrix24")
	}
}

func (c *Client) ListsElementGet(ctx context.Context, iblockTypeID string, iblockID int, start int, selectFields []string) ([]ListElement, int, error) {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"start":          start,
	}
	if len(selectFields) > 0 {
		body["SELECT"] = selectFields
	}
	raw, next, err := c.call(ctx, "lists.element.get", body)
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
			ID:         toInt64(m["ID"]),
			Name:       strings.TrimSpace(toString(m["NAME"])),
			Code:       strings.TrimSpace(toString(m["CODE"])),
			Properties: clonePropertyMap(m),
			RawJSON:    string(rawJSON),
		})
	}
	return result, next, nil
}

func (c *Client) ListsElementAdd(
	ctx context.Context,
	iblockTypeID string,
	iblockID int,
	elementCode string,
	fields map[string]interface{},
) (int64, error) {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"ELEMENT_CODE":   elementCode,
		"FIELDS":         fields,
	}
	raw, _, err := c.call(ctx, "lists.element.add", body)
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) ListsElementUpdate(
	ctx context.Context,
	iblockTypeID string,
	iblockID int,
	elementID int64,
	fields map[string]interface{},
) error {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"ELEMENT_ID":     elementID,
		"FIELDS":         fields,
	}
	_, _, err := c.call(ctx, "lists.element.update", body)
	return err
}

func (c *Client) ListsFieldGet(ctx context.Context, iblockTypeID string, iblockID int, fieldID string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"FIELD_ID":       fieldID,
	}
	raw, _, err := c.call(ctx, "lists.field.get", body)
	if err != nil {
		return nil, err
	}
	resultMap, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.New("некорректный формат ответа lists.field.get")
	}
	for _, value := range resultMap {
		fieldData, ok := value.(map[string]interface{})
		if !ok {
			continue
		}
		return fieldData, nil
	}
	return nil, errors.New("поле не найдено в ответе lists.field.get")
}

func (c *Client) UserGet(ctx context.Context, start int) ([]User, int, error) {
	raw, next, err := c.call(ctx, "user.get", map[string]interface{}{
		"start":  start,
		"filter": []map[string]interface{}{{"ACTIVE": "true"}},
		"select": []string{"ID", "NAME", "LAST_NAME", "SECOND_NAME", "EMAIL", "PERSONAL_MOBILE", "ACTIVE"},
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
			Active:     parseBitrixActive(m["ACTIVE"]),
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
		assignedBy := toInt64(m["ASSIGNED_BY_ID"])
		var assignedByPtr *int64
		if assignedBy > 0 {
			assignedByPtr = &assignedBy
		}
		modifiedBy := toInt64(m["MODIFY_BY_ID"])
		var modifiedByPtr *int64
		if modifiedBy > 0 {
			modifiedByPtr = &modifiedBy
		}
		result = append(result, Deal{
			ID:         toInt64(m["ID"]),
			OriginID:   strings.TrimSpace(toString(m["ORIGIN_ID"])),
			Originator: strings.TrimSpace(toString(m["ORIGINATOR_ID"])),
			StageID:    strings.TrimSpace(toString(m["STAGE_ID"])),
			CategoryID: toInt(m["CATEGORY_ID"]),
			Title:      strings.TrimSpace(toString(m["TITLE"])),
			AssignedBy: assignedByPtr,
			ModifiedBy: modifiedByPtr,
			Raw:        m,
		})
	}
	return result
}

func parseDeal(raw interface{}) *Deal {
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	assignedBy := toInt64(m["ASSIGNED_BY_ID"])
	var assignedByPtr *int64
	if assignedBy > 0 {
		assignedByPtr = &assignedBy
	}
	modifiedBy := toInt64(m["MODIFY_BY_ID"])
	var modifiedByPtr *int64
	if modifiedBy > 0 {
		modifiedByPtr = &modifiedBy
	}
	return &Deal{
		ID:         toInt64(m["ID"]),
		OriginID:   strings.TrimSpace(toString(m["ORIGIN_ID"])),
		Originator: strings.TrimSpace(toString(m["ORIGINATOR_ID"])),
		StageID:    strings.TrimSpace(toString(m["STAGE_ID"])),
		CategoryID: toInt(m["CATEGORY_ID"]),
		Title:      strings.TrimSpace(toString(m["TITLE"])),
		AssignedBy: assignedByPtr,
		ModifiedBy: modifiedByPtr,
		Raw:        m,
	}
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

func parseBitrixActive(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		val := strings.ToLower(strings.TrimSpace(x))
		return val == "y" || val == "yes" || val == "true" || val == "1"
	case float64:
		return int64(x) != 0
	case int:
		return x != 0
	case int64:
		return x != 0
	default:
		return false
	}
}

func clonePropertyMap(src map[string]interface{}) map[string]interface{} {
	props := make(map[string]interface{})
	for key, value := range src {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(key)), "PROPERTY_") {
			props[key] = value
		}
	}
	return props
}

func buildTimelineCommentAddFields(entityType string, entityID int64, comment string, files []FileToUpload) map[string]interface{} {
	normalizedEntityType := strings.TrimSpace(strings.ToLower(entityType))
	if normalizedEntityType == "" {
		normalizedEntityType = "deal"
	}
	fields := map[string]interface{}{
		"ENTITY_TYPE": normalizedEntityType,
		"ENTITY_ID":   entityID,
		"COMMENT":     comment,
	}
	fileField := buildTimelineCommentFilesField(files)
	if len(fileField) > 0 {
		fields["FILES"] = fileField
	}
	return fields
}

func buildTimelineCommentUpdateFields(comment string, files []FileToUpload, includeFiles bool) map[string]interface{} {
	fields := map[string]interface{}{
		"COMMENT": comment,
	}
	if includeFiles {
		fields["FILES"] = buildTimelineCommentFilesField(files)
	}
	return fields
}

func buildTimelineCommentFilesField(files []FileToUpload) [][]string {
	if len(files) == 0 {
		return [][]string{}
	}
	out := make([][]string, 0, len(files))
	for _, file := range files {
		name := strings.TrimSpace(file.Name)
		content := normalizeBase64Content(file.Base64Content)
		if name == "" || content == "" {
			continue
		}
		out = append(out, []string{name, content})
	}
	return out
}

func normalizeBase64Content(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ""
	}
	if idx := strings.Index(trimmed, ","); idx > 0 {
		prefix := strings.ToLower(strings.TrimSpace(trimmed[:idx]))
		if strings.HasPrefix(prefix, "data:") && strings.Contains(prefix, ";base64") {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		}
	}
	if trimmed == "" {
		return ""
	}
	_, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return ""
	}
	return trimmed
}

func dedupeInt64(items []int64) []int64 {
	if len(items) == 0 {
		return []int64{}
	}
	seen := make(map[int64]struct{}, len(items))
	out := make([]int64, 0, len(items))
	for _, id := range items {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func extractDiskFileIDsFromFilesField(filesRaw interface{}) []int64 {
	out := make([]int64, 0, 8)
	switch files := filesRaw.(type) {
	case []interface{}:
		for _, item := range files {
			fileMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if id := extractDiskFileIDFromMap(fileMap); id > 0 {
				out = append(out, id)
			}
		}
	case map[string]interface{}:
		for key, value := range files {
			id := toInt64(key)
			if nested, ok := value.(map[string]interface{}); ok {
				nestedID := extractDiskFileIDFromMap(nested)
				if nestedID > 0 {
					id = nestedID
				}
			}
			if id > 0 {
				out = append(out, id)
			}
		}
	}
	return out
}

func extractDiskFileIDFromMap(item map[string]interface{}) int64 {
	if len(item) == 0 {
		return 0
	}
	id := toInt64(item["ID"])
	if id <= 0 {
		id = toInt64(item["id"])
	}
	return id
}
