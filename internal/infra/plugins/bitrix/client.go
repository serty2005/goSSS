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
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

type Deal struct {
	ID         int64
	OriginID   string
	Originator string
	StageID    string
	Closed     string
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
	CreatedAt  *time.Time
	Properties map[string]interface{}
	RawJSON    string
}

type ListElementBatchAction string

const (
	ListElementBatchActionAdd    ListElementBatchAction = "add"
	ListElementBatchActionUpdate ListElementBatchAction = "update"
	ListElementBatchActionDelete ListElementBatchAction = "delete"
)

type ListElementBatchCommand struct {
	Key          string
	Action       ListElementBatchAction
	IBlockTypeID string
	IBlockID     int
	ElementID    int64
	ElementCode  string
	Fields       map[string]interface{}
}

type ListElementBatchResult struct {
	CreatedIDs map[string]int64
	Errors     map[string]error
}

type ListElementBatchGetResult struct {
	Items  map[int64]ListElement
	Errors map[int64]error
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

const (
	bitrixBatchLimit             = 50
	bitrixListPageSize           = 50
	bitrixDefaultRateLimitBurst  = 50
	bitrixDefaultRateLimitPerMin = 120
	bitrixMaxAttempts            = 5
)

type batchCommand struct {
	ID     string
	Method string
	Params map[string]any
}

type batchResult struct {
	Results map[string]any
	Errors  map[string]error
	Next    map[string]int
	Total   map[string]int
}

type Client struct {
	baseURL            string
	httpClient         *http.Client
	logger             logger.LoggerInterface
	limiter            *rate.Limiter
	rateLimitPerSecond float64
	rateLimitBurst     int
}

func NewClient(cfg *config.Config, log logger.LoggerInterface) *Client {
	baseURL := ""
	timeout := 15 * time.Second
	limitPerMinute := bitrixDefaultRateLimitPerMin
	limitBurst := bitrixDefaultRateLimitBurst
	if cfg != nil {
		baseURL = strings.TrimRight(strings.TrimSpace(cfg.BitrixBaseURL), "/") + "/"
		if cfg.RequestTimeout > 0 {
			timeout = cfg.RequestTimeout
		}
		if cfg.BitrixRateLimitPerMin > 0 {
			limitPerMinute = cfg.BitrixRateLimitPerMin
		}
		if cfg.BitrixRateLimitBurst > 0 {
			limitBurst = cfg.BitrixRateLimitBurst
		}
	}
	rateLimitPerSecond := max(float64(limitPerMinute)/60.0, 1.0/60.0)

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger:             log,
		limiter:            rate.NewLimiter(rate.Limit(rateLimitPerSecond), limitBurst),
		rateLimitPerSecond: rateLimitPerSecond,
		rateLimitBurst:     limitBurst,
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
		"select": []string{"ID", "ORIGINATOR_ID", "ORIGIN_ID", "STAGE_ID", "CLOSED", "CATEGORY_ID", "TITLE"},
	}
	raw, _, _, err := c.call(ctx, "crm.deal.list", body)
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
		"select": []string{"ID", "ORIGINATOR_ID", "ORIGIN_ID", "STAGE_ID", "CLOSED", "CATEGORY_ID", "TITLE"},
		"start":  start,
	}
	raw, next, _, err := c.call(ctx, "crm.deal.list", body)
	if err != nil {
		return nil, 0, err
	}
	return parseDeals(raw), next, nil
}

func (c *Client) DealAdd(ctx context.Context, fields map[string]interface{}) (int64, error) {
	raw, _, _, err := c.call(ctx, "crm.deal.add", map[string]interface{}{"fields": fields})
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) DealGet(ctx context.Context, dealID int64) (*Deal, error) {
	raw, _, _, err := c.call(ctx, "crm.deal.get", map[string]interface{}{
		"id": dealID,
		"select": []string{
			"ID",
			"ORIGINATOR_ID",
			"ORIGIN_ID",
			"STAGE_ID",
			"CLOSED",
			"CATEGORY_ID",
			"TITLE",
			"ASSIGNED_BY_ID",
			"MODIFY_BY_ID",
			"UF_CRM_1766060620",
			"COMMENTS",
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
	_, _, _, err := c.call(ctx, "crm.deal.update", map[string]interface{}{
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
	raw, _, _, err := c.call(ctx, "crm.timeline.comment.add", body)
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
	raw, _, _, err := c.call(ctx, "crm.timeline.comment.add", body)
	if err != nil {
		return 0, err
	}
	return toInt64(raw), nil
}

func (c *Client) TimelineCommentUpdateWithFiles(ctx context.Context, commentID int64, comment string, files []FileToUpload) error {
	_, _, _, err := c.call(ctx, "crm.timeline.comment.update", map[string]interface{}{
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
	raw, next, _, err := c.call(ctx, "crm.timeline.comment.list", body)
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
	raw, _, _, err := c.call(ctx, "crm.timeline.comment.get", map[string]interface{}{"id": commentID})
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
	raw, _, _, err := c.call(ctx, "disk.file.get", map[string]interface{}{"id": diskFileID})
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
	raw, _, _, err := c.call(ctx, "lists.get.iblock.type.id", map[string]interface{}{"IBLOCK_ID": iblockID})
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
	items, next, _, err := c.listsElementGetPage(ctx, iblockTypeID, iblockID, start, selectFields)
	if err != nil {
		return nil, 0, err
	}
	return items, next, nil
}

func (c *Client) ListsElementGetAll(ctx context.Context, iblockTypeID string, iblockID int, selectFields []string) ([]ListElement, error) {
	items, next, total, err := c.listsElementGetPage(ctx, iblockTypeID, iblockID, 0, selectFields)
	if err != nil {
		return nil, err
	}
	all := append([]ListElement{}, items...)
	if next <= 0 {
		return all, nil
	}
	if total <= 0 {
		start := next
		for start > 0 {
			page, nextPage, _, pageErr := c.listsElementGetPage(ctx, iblockTypeID, iblockID, start, selectFields)
			if pageErr != nil {
				return nil, pageErr
			}
			all = append(all, page...)
			start = nextPage
		}
		return all, nil
	}
	starts := buildListStarts(next, total)
	for offset := 0; offset < len(starts); offset += bitrixBatchLimit {
		chunk := starts[offset:min(offset+bitrixBatchLimit, len(starts))]
		pages, pageErr := c.listsElementGetBatchPages(ctx, iblockTypeID, iblockID, chunk, selectFields)
		if pageErr != nil {
			return nil, pageErr
		}
		for _, start := range chunk {
			all = append(all, pages[start]...)
		}
	}
	return all, nil
}

func (c *Client) listsElementGetPage(ctx context.Context, iblockTypeID string, iblockID int, start int, selectFields []string) ([]ListElement, int, int, error) {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"start":          start,
	}
	if len(selectFields) > 0 {
		body["SELECT"] = selectFields
	}
	raw, next, total, err := c.call(ctx, "lists.element.get", body)
	if err != nil {
		return nil, 0, 0, err
	}
	return parseListElements(raw), next, total, nil
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
	raw, _, _, err := c.call(ctx, "lists.element.add", body)
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
	_, _, _, err := c.call(ctx, "lists.element.update", body)
	return err
}

func (c *Client) ListsElementDelete(
	ctx context.Context,
	iblockTypeID string,
	iblockID int,
	elementID int64,
) error {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"ELEMENT_ID":     elementID,
	}
	_, _, _, err := c.call(ctx, "lists.element.delete", body)
	return err
}

func (c *Client) ListsElementBatchGetByIDs(
	ctx context.Context,
	iblockTypeID string,
	iblockID int,
	elementIDs []int64,
	selectFields []string,
) (*ListElementBatchGetResult, error) {
	result := &ListElementBatchGetResult{
		Items:  make(map[int64]ListElement),
		Errors: make(map[int64]error),
	}
	if len(elementIDs) == 0 {
		return result, nil
	}

	uniqueIDs := make([]int64, 0, len(elementIDs))
	seen := make(map[int64]struct{}, len(elementIDs))
	for _, elementID := range elementIDs {
		if elementID <= 0 {
			continue
		}
		if _, exists := seen[elementID]; exists {
			continue
		}
		seen[elementID] = struct{}{}
		uniqueIDs = append(uniqueIDs, elementID)
	}
	if len(uniqueIDs) == 0 {
		return result, nil
	}

	for offset := 0; offset < len(uniqueIDs); offset += bitrixBatchLimit {
		chunk := uniqueIDs[offset:min(offset+bitrixBatchLimit, len(uniqueIDs))]
		batchCommands := make([]batchCommand, 0, len(chunk))
		keysByID := make(map[int64]string, len(chunk))

		for _, elementID := range chunk {
			key := fmt.Sprintf("element_%d", elementID)
			keysByID[elementID] = key
			params := map[string]any{
				"IBLOCK_TYPE_ID": iblockTypeID,
				"IBLOCK_ID":      iblockID,
				"ELEMENT_ID":     elementID,
			}
			if len(selectFields) > 0 {
				params["SELECT"] = selectFields
			}
			batchCommands = append(batchCommands, batchCommand{
				ID:     key,
				Method: "lists.element.get",
				Params: params,
			})
		}

		batchResult, err := c.callBatch(ctx, false, batchCommands)
		if err != nil {
			return nil, err
		}

		for _, elementID := range chunk {
			key := keysByID[elementID]
			if cmdErr, failed := batchResult.Errors[key]; failed {
				result.Errors[elementID] = cmdErr
				continue
			}

			items := parseListElements(batchResult.Results[key])
			if len(items) == 0 {
				result.Errors[elementID] = fmt.Errorf("элемент %d не найден в ответе Bitrix24", elementID)
				continue
			}

			result.Items[elementID] = items[0]
		}
	}

	return result, nil
}

func (c *Client) ListsElementBatch(ctx context.Context, commands []ListElementBatchCommand) (*ListElementBatchResult, error) {
	result := &ListElementBatchResult{
		CreatedIDs: make(map[string]int64),
		Errors:     make(map[string]error),
	}
	if len(commands) == 0 {
		return result, nil
	}

	for offset := 0; offset < len(commands); offset += bitrixBatchLimit {
		chunk := commands[offset:min(offset+bitrixBatchLimit, len(commands))]
		batchCommands := make([]batchCommand, 0, len(chunk))
		for _, item := range chunk {
			key := strings.TrimSpace(item.Key)
			if key == "" {
				return nil, errors.New("batch-команда списков должна содержать ключ")
			}

			var (
				method string
				params map[string]any
			)

			switch item.Action {
			case ListElementBatchActionAdd:
				method = "lists.element.add"
				params = map[string]any{
					"IBLOCK_TYPE_ID": item.IBlockTypeID,
					"IBLOCK_ID":      item.IBlockID,
					"ELEMENT_CODE":   item.ElementCode,
					"FIELDS":         item.Fields,
				}
			case ListElementBatchActionUpdate:
				method = "lists.element.update"
				params = map[string]any{
					"IBLOCK_TYPE_ID": item.IBlockTypeID,
					"IBLOCK_ID":      item.IBlockID,
					"ELEMENT_ID":     item.ElementID,
					"FIELDS":         item.Fields,
				}
			case ListElementBatchActionDelete:
				method = "lists.element.delete"
				params = map[string]any{
					"IBLOCK_TYPE_ID": item.IBlockTypeID,
					"IBLOCK_ID":      item.IBlockID,
					"ELEMENT_ID":     item.ElementID,
				}
			default:
				return nil, fmt.Errorf("неподдерживаемое batch-действие для списков: %s", item.Action)
			}

			batchCommands = append(batchCommands, batchCommand{
				ID:     key,
				Method: method,
				Params: params,
			})
		}

		batchResult, err := c.callBatch(ctx, false, batchCommands)
		if err != nil {
			return nil, err
		}
		for key, batchErr := range batchResult.Errors {
			result.Errors[key] = batchErr
		}
		for _, item := range chunk {
			if _, failed := result.Errors[item.Key]; failed {
				continue
			}
			if item.Action == ListElementBatchActionAdd {
				result.CreatedIDs[item.Key] = toInt64(batchResult.Results[item.Key])
			}
		}
	}

	return result, nil
}

func (c *Client) ListsFieldGet(ctx context.Context, iblockTypeID string, iblockID int, fieldID string) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"IBLOCK_TYPE_ID": iblockTypeID,
		"IBLOCK_ID":      iblockID,
		"FIELD_ID":       fieldID,
	}
	raw, _, _, err := c.call(ctx, "lists.field.get", body)
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
	items, next, _, err := c.userGetPage(ctx, start)
	if err != nil {
		return nil, 0, err
	}
	return items, next, nil
}

func (c *Client) UserGetAll(ctx context.Context) ([]User, error) {
	users, next, total, err := c.userGetPage(ctx, 0)
	if err != nil {
		return nil, err
	}
	all := append([]User{}, users...)
	if next <= 0 {
		return all, nil
	}
	if total <= 0 {
		start := next
		for start > 0 {
			page, nextPage, _, pageErr := c.userGetPage(ctx, start)
			if pageErr != nil {
				return nil, pageErr
			}
			all = append(all, page...)
			start = nextPage
		}
		return all, nil
	}
	starts := buildListStarts(next, total)
	for offset := 0; offset < len(starts); offset += bitrixBatchLimit {
		chunk := starts[offset:min(offset+bitrixBatchLimit, len(starts))]
		pages, pageErr := c.userGetBatchPages(ctx, chunk)
		if pageErr != nil {
			return nil, pageErr
		}
		for _, start := range chunk {
			all = append(all, pages[start]...)
		}
	}
	return all, nil
}

func (c *Client) userGetPage(ctx context.Context, start int) ([]User, int, int, error) {
	raw, next, total, err := c.call(ctx, "user.get", map[string]interface{}{
		"start":  start,
		"filter": map[string]interface{}{"ACTIVE": "true"},
		"select": []string{"ID", "NAME", "LAST_NAME", "SECOND_NAME", "EMAIL", "PERSONAL_MOBILE", "ACTIVE"},
	})
	if err != nil {
		return nil, 0, 0, err
	}
	return parseUsers(raw), next, total, nil
}

type bitrixEnvelope struct {
	Result           interface{} `json:"result"`
	Error            string      `json:"error"`
	ErrorDescription string      `json:"error_description"`
	Next             interface{} `json:"next"`
	Total            interface{} `json:"total"`
}

var bitrixWebhookKeyRe = regexp.MustCompile(`(/rest/[^/]+/)([^/]+)(/)`)

func (c *Client) call(ctx context.Context, method string, body map[string]interface{}) (interface{}, int, int, error) {
	if !c.IsConfigured() {
		return nil, 0, 0, errors.New("не настроен BITRIX_BASE_URL")
	}
	url := c.baseURL + method + ".json"
	redactedURL := redactWebhookURL(url)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, 0, 0, err
	}
	payloadText := string(payload)

	var lastErr error
	limited := false
	for attempt := 0; attempt < bitrixMaxAttempts; attempt++ {
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx); err != nil {
				return nil, 0, 0, fmt.Errorf("ожидание лимита Bitrix24 прервано: %w", err)
			}
		}
		if attempt > 0 {
			if err := waitBitrixRetry(ctx, bitrixRetryDelay(attempt, limited)); err != nil {
				return nil, 0, 0, fmt.Errorf("ожидание повторного вызова Bitrix24 прервано: %w", err)
			}
		}
		limited = false
		c.logger.Info("Bitrix24 запрос", "http_method", http.MethodPost, "method", method, "url", redactedURL, "attempt", attempt+1, "body", payloadText)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, 0, 0, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = redactWebhookError(err)
			c.logger.Error("Bitrix24 ошибка HTTP-запроса", "method", method, "url", redactedURL, "attempt", attempt+1, "error", lastErr)
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

		if strings.TrimSpace(env.Error) != "" {
			errText := fmt.Sprintf("%s: %s", strings.TrimSpace(env.Error), strings.TrimSpace(env.ErrorDescription))
			if strings.Contains(strings.ToUpper(env.Error), "QUERY_LIMIT_EXCEEDED") {
				lastErr = errors.New(errText)
				limited = true
				c.logger.Warn("Bitrix24 ограничение запросов", "method", method, "url", redactedURL, "attempt", attempt+1, "error", env.Error, "error_description", env.ErrorDescription)
				continue
			}
			c.logger.Error("Bitrix24 вернул бизнес-ошибку", "method", method, "url", redactedURL, "error", env.Error, "error_description", env.ErrorDescription)
			return nil, 0, 0, errors.New(errText)
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			lastErr = fmt.Errorf("временная ошибка Bitrix24 (%s): %d", method, resp.StatusCode)
			limited = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable
			continue
		}

		return env.Result, toInt(env.Next), toInt(env.Total), nil
	}

	return nil, 0, 0, fmt.Errorf("вызов Bitrix24 %s завершился ошибкой: %w", method, lastErr)
}

func bitrixRetryDelay(attempt int, limited bool) time.Duration {
	if limited {
		delay := time.Duration(1<<max(attempt-1, 0)) * time.Second
		return min(delay, 8*time.Second)
	}
	delay := time.Duration(attempt*attempt) * 200 * time.Millisecond
	return min(delay, 3*time.Second)
}

func waitBitrixRetry(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) listsElementGetBatchPages(
	ctx context.Context,
	iblockTypeID string,
	iblockID int,
	starts []int,
	selectFields []string,
) (map[int][]ListElement, error) {
	commands := make([]batchCommand, 0, len(starts))
	keysByStart := make(map[int]string, len(starts))
	for _, start := range starts {
		key := fmt.Sprintf("page_%d", start)
		keysByStart[start] = key
		params := map[string]any{
			"IBLOCK_TYPE_ID": iblockTypeID,
			"IBLOCK_ID":      iblockID,
			"start":          start,
		}
		if len(selectFields) > 0 {
			params["SELECT"] = selectFields
		}
		commands = append(commands, batchCommand{
			ID:     key,
			Method: "lists.element.get",
			Params: params,
		})
	}
	result, err := c.callBatch(ctx, false, commands)
	if err != nil {
		return nil, err
	}
	pages := make(map[int][]ListElement, len(starts))
	for _, start := range starts {
		key := keysByStart[start]
		if cmdErr, ok := result.Errors[key]; ok {
			return nil, fmt.Errorf("ошибка batch-запроса lists.element.get для start=%d: %w", start, cmdErr)
		}
		pages[start] = parseListElements(result.Results[key])
	}
	return pages, nil
}

func (c *Client) userGetBatchPages(ctx context.Context, starts []int) (map[int][]User, error) {
	commands := make([]batchCommand, 0, len(starts))
	keysByStart := make(map[int]string, len(starts))
	for _, start := range starts {
		key := fmt.Sprintf("page_%d", start)
		keysByStart[start] = key
		commands = append(commands, batchCommand{
			ID:     key,
			Method: "user.get",
			Params: map[string]any{
				"start":  start,
				"filter": map[string]any{"ACTIVE": "true"},
				"select": []string{"ID", "NAME", "LAST_NAME", "SECOND_NAME", "EMAIL", "PERSONAL_MOBILE", "ACTIVE"},
			},
		})
	}
	result, err := c.callBatch(ctx, false, commands)
	if err != nil {
		return nil, err
	}
	pages := make(map[int][]User, len(starts))
	for _, start := range starts {
		key := keysByStart[start]
		if cmdErr, ok := result.Errors[key]; ok {
			return nil, fmt.Errorf("ошибка batch-запроса user.get для start=%d: %w", start, cmdErr)
		}
		pages[start] = parseUsers(result.Results[key])
	}
	return pages, nil
}

func (c *Client) callBatch(ctx context.Context, halt bool, commands []batchCommand) (*batchResult, error) {
	result := &batchResult{
		Results: make(map[string]any),
		Errors:  make(map[string]error),
		Next:    make(map[string]int),
		Total:   make(map[string]int),
	}
	if len(commands) == 0 {
		return result, nil
	}
	if len(commands) > bitrixBatchLimit {
		return nil, fmt.Errorf("пакет Bitrix24 не может содержать более %d команд", bitrixBatchLimit)
	}
	cmd := make(map[string]string, len(commands))
	for _, item := range commands {
		key := strings.TrimSpace(item.ID)
		method := strings.TrimSpace(item.Method)
		if key == "" || method == "" {
			return nil, errors.New("batch-команда Bitrix24 должна содержать ключ и метод")
		}
		if _, exists := cmd[key]; exists {
			return nil, fmt.Errorf("дублирующийся ключ batch-команды Bitrix24: %s", key)
		}
		cmd[key] = buildBatchMethod(method, item.Params)
	}
	raw, _, _, err := c.call(ctx, "batch", map[string]any{
		"halt": halt,
		"cmd":  cmd,
	})
	if err != nil {
		return nil, err
	}
	rawMap, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("некорректный формат ответа batch Bitrix24")
	}
	if results, ok := rawMap["result"].(map[string]any); ok {
		result.Results = results
	}
	if totals, ok := rawMap["result_total"].(map[string]any); ok {
		for key, value := range totals {
			result.Total[key] = toInt(value)
		}
	}
	if nexts, ok := rawMap["result_next"].(map[string]any); ok {
		for key, value := range nexts {
			result.Next[key] = toInt(value)
		}
	}
	if rawErrors, ok := rawMap["result_error"].(map[string]any); ok {
		for key, value := range rawErrors {
			result.Errors[key] = parseBatchError(value)
		}
	}
	return result, nil
}

func buildBatchMethod(method string, params map[string]any) string {
	normalizedMethod := strings.TrimSpace(method)
	if len(params) == 0 {
		return normalizedMethod
	}
	values := url.Values{}
	appendBatchParams(values, "", params)
	encoded := values.Encode()
	if encoded == "" {
		return normalizedMethod
	}
	return normalizedMethod + "?" + encoded
}

func appendBatchParams(values url.Values, prefix string, raw any) {
	switch value := raw.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			name := key
			if prefix != "" {
				name = prefix + "[" + key + "]"
			}
			appendBatchParams(values, name, value[key])
		}
	case []string:
		for _, item := range value {
			values.Add(prefix+"[]", item)
		}
	case []any:
		for _, item := range value {
			appendBatchParams(values, prefix+"[]", item)
		}
	case nil:
		return
	default:
		values.Add(prefix, formatBatchValue(value))
	}
}

func formatBatchValue(value any) string {
	switch item := value.(type) {
	case bool:
		return strconv.FormatBool(item)
	case time.Time:
		return item.Format(time.RFC3339)
	default:
		return fmt.Sprint(item)
	}
}

func parseBatchError(raw any) error {
	if m, ok := raw.(map[string]any); ok {
		code := strings.TrimSpace(toString(m["error"]))
		description := strings.TrimSpace(toString(m["error_description"]))
		switch {
		case code != "" && description != "":
			return fmt.Errorf("%s: %s", code, description)
		case code != "":
			return errors.New(code)
		case description != "":
			return errors.New(description)
		}
	}
	if text := strings.TrimSpace(toString(raw)); text != "" {
		return errors.New(text)
	}
	return errors.New("неизвестная ошибка batch Bitrix24")
}

func buildListStarts(next int, total int) []int {
	if next <= 0 || total <= next {
		return nil
	}
	starts := make([]int, 0, (total-next+bitrixListPageSize-1)/bitrixListPageSize)
	for start := next; start < total; start += bitrixListPageSize {
		starts = append(starts, start)
	}
	return starts
}

func redactWebhookURL(url string) string {
	if strings.TrimSpace(url) == "" {
		return url
	}
	return bitrixWebhookKeyRe.ReplaceAllString(url, "${1}REDACTED${3}")
}

func redactWebhookError(err error) error {
	if err == nil {
		return nil
	}
	redacted := redactWebhookURL(err.Error())
	if redacted == err.Error() {
		return err
	}
	return errors.New(redacted)
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
			Closed:     strings.TrimSpace(toString(m["CLOSED"])),
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
		Closed:     strings.TrimSpace(toString(m["CLOSED"])),
		CategoryID: toInt(m["CATEGORY_ID"]),
		Title:      strings.TrimSpace(toString(m["TITLE"])),
		AssignedBy: assignedByPtr,
		ModifiedBy: modifiedByPtr,
		Raw:        m,
	}
}

func parseListElements(raw interface{}) []ListElement {
	items, ok := raw.([]interface{})
	if !ok {
		return []ListElement{}
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
			CreatedAt:  parseBitrixListElementTime(m["DATE_CREATE"]),
			Properties: clonePropertyMap(m),
			RawJSON:    string(rawJSON),
		})
	}
	return result
}

func parseBitrixListElementTime(value interface{}) *time.Time {
	raw := strings.TrimSpace(toString(value))
	if raw == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"02.01.2006 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, raw)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func parseUsers(raw interface{}) []User {
	items, ok := raw.([]interface{})
	if !ok {
		return []User{}
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
	return out
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
