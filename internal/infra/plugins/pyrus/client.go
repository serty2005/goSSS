package pyrus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pyrusAuthURL = "https://accounts.pyrus.com/api/v4/auth"
const pyrusClientLogPreviewLimit = 240

type Person struct {
	ID        int64      `json:"id"`
	FirstName string     `json:"first_name"`
	LastName  string     `json:"last_name"`
	Email     string     `json:"email"`
	Position  string     `json:"position"`
	Type      string     `json:"type"`
	Messenger *Messenger `json:"messenger,omitzero"`
}

func (p *Person) DisplayName() string {
	if p == nil {
		return ""
	}
	fullName := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(p.FirstName), strings.TrimSpace(p.LastName)}, " "))
	if fullName != "" {
		return fullName
	}
	return strings.TrimSpace(p.Email)
}

type Messenger struct {
	Type     string `json:"type"`
	Nickname string `json:"nickname"`
}

type UserReference struct {
	ID    *int64 `json:"id,omitzero"`
	Email string `json:"email,omitempty"`
}

type Member struct {
	ID              int64      `json:"id"`
	FirstName       string     `json:"first_name"`
	LastName        string     `json:"last_name"`
	Email           string     `json:"email"`
	Position        string     `json:"position"`
	Type            string     `json:"type"`
	Status          string     `json:"status"`
	Banned          bool       `json:"banned"`
	Fired           bool       `json:"fired"`
	Messenger       *Messenger `json:"messenger,omitzero"`
	MobilePhone     string     `json:"mobile_phone"`
	Phone           string     `json:"phone"`
	Location        string     `json:"location"`
	Personality     string     `json:"personality"`
	PersonnelNumber string     `json:"personnel_number"`
	VacationDays    string     `json:"vacation_days"`
}

func (m *Member) DisplayName() string {
	if m == nil {
		return ""
	}
	fullName := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(m.FirstName), strings.TrimSpace(m.LastName)}, " "))
	if fullName != "" {
		return fullName
	}
	return strings.TrimSpace(m.Email)
}

type ChannelParty struct {
	Email string `json:"email,omitzero"`
	Name  string `json:"name,omitzero"`
}

type Channel struct {
	Type string        `json:"type"`
	From *ChannelParty `json:"from,omitzero"`
	To   *ChannelParty `json:"to,omitzero"`
}

type CommentRole struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Attachment struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MD5     string `json:"md5"`
	URL     string `json:"url"`
	Version int64  `json:"version"`
	RootID  int64  `json:"root_id"`
}

type Field struct {
	ID     int64  `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Value  any    `json:"value"`
	Text   string `json:"text"`
	Number any    `json:"number"`
}

type Comment struct {
	ID             int64         `json:"id"`
	Text           string        `json:"text"`
	CreateDate     time.Time     `json:"create_date"`
	Author         *Person       `json:"author"`
	ReassignedTo   *Person       `json:"reassigned_to,omitzero"`
	FieldUpdates   []Field       `json:"field_updates,omitzero"`
	Attachments    []Attachment  `json:"attachments,omitzero"`
	Action         string        `json:"action"`
	Channel        *Channel      `json:"channel,omitzero"`
	CommentAsRoles []CommentRole `json:"comment_as_roles,omitzero"`
	EditCommentID  *int64        `json:"edit_comment_id,omitzero"`
}

type Task struct {
	ID               int64        `json:"id"`
	Text             string       `json:"text"`
	CreateDate       time.Time    `json:"create_date"`
	LastModifiedDate time.Time    `json:"last_modified_date"`
	CloseDate        *time.Time   `json:"close_date,omitzero"`
	CurrentStep      *int         `json:"current_step,omitzero"`
	FormID           int64        `json:"form_id"`
	Author           *Person      `json:"author,omitzero"`
	Responsible      *Person      `json:"responsible,omitzero"`
	Fields           []Field      `json:"fields,omitzero"`
	Comments         []Comment    `json:"comments,omitzero"`
	Attachments      []Attachment `json:"attachments,omitzero"`
}

type WebhookPayload struct {
	Event       string `json:"event"`
	AccessToken string `json:"access_token"`
	TaskID      int64  `json:"task_id"`
	UserID      *int64 `json:"user_id,omitzero"`
	Task        Task   `json:"task"`
}

type FieldUpdateRequest struct {
	ID    *int64 `json:"id,omitzero"`
	Code  string `json:"code,omitempty"`
	Value any    `json:"value"`
}

type CommentRequest struct {
	Text          string               `json:"text,omitempty"`
	FormattedText string               `json:"formatted_text,omitempty"`
	Action        string               `json:"action,omitempty"`
	ReassignTo    *UserReference       `json:"reassign_to,omitzero"`
	FieldUpdates  []FieldUpdateRequest `json:"field_updates,omitzero"`
	Attachments   []string             `json:"attachments,omitzero"`
	Channel       *Channel             `json:"channel,omitzero"`
	EditCommentID *int64               `json:"edit_comment_id,omitzero"`
}

type HTTPError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	if strings.TrimSpace(e.Message) != "" {
		return fmt.Sprintf("Pyrus API вернул %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("Pyrus API вернул HTTP %d", e.StatusCode)
}

type DownloadedFile struct {
	FileName string
	MimeType string
	Content  []byte
}

type Client struct {
	configured    bool
	configBaseURL string
	login         string
	securityKey   string
	httpClient    *http.Client
	logger        logger.LoggerInterface

	mu          sync.Mutex
	accessToken string
	apiURL      string
	filesURL    string
}

func NewClient(cfg *config.Config, log logger.LoggerInterface) *Client {
	timeout := 15 * time.Second
	configBaseURL := ""
	login := ""
	securityKey := ""
	if cfg != nil {
		timeout = cfg.RequestTimeout
		configBaseURL = normalizeAPIBaseURL(cfg.PyrusAPIBaseURL)
		login = strings.TrimSpace(cfg.PyrusLogin)
		securityKey = strings.TrimSpace(cfg.PyrusSecurityKey)
	}
	if configBaseURL == "" {
		configBaseURL = "https://api.pyrus.com/v4/"
	}
	return &Client{
		configured:    login != "" && securityKey != "",
		configBaseURL: configBaseURL,
		login:         login,
		securityKey:   securityKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: log,
	}
}

func (c *Client) IsConfigured() bool {
	return c != nil && c.configured
}

func ParseWebhookPayload(rawBody []byte) (*WebhookPayload, error) {
	payload := &WebhookPayload{}
	if err := json.Unmarshal(rawBody, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *Client) GetTask(ctx context.Context, taskID int64) (*Task, error) {
	if taskID <= 0 {
		return nil, fmt.Errorf("некорректный task_id")
	}
	var response struct {
		Task Task `json:"task"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "tasks/"+strconv.FormatInt(taskID, 10), nil, &response); err != nil {
		return nil, err
	}
	return &response.Task, nil
}

func (c *Client) AddComment(ctx context.Context, taskID int64, req CommentRequest) (*Task, error) {
	if taskID <= 0 {
		return nil, fmt.Errorf("некорректный task_id")
	}
	var response struct {
		Task Task `json:"task"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "tasks/"+strconv.FormatInt(taskID, 10)+"/comments", req, &response); err != nil {
		return nil, err
	}
	return &response.Task, nil
}

func (c *Client) ListMembers(ctx context.Context) ([]Member, error) {
	var response struct {
		Members []Member `json:"members"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "members", nil, &response); err != nil {
		return nil, err
	}
	return response.Members, nil
}

func (c *Client) UpdateTaskExtID(ctx context.Context, taskID int64, extID string) (*Task, error) {
	text := "От XenionDesk: записан локальный идентификатор заявки."
	return c.AddComment(ctx, taskID, CommentRequest{
		Text: text,
		FieldUpdates: []FieldUpdateRequest{
			{
				Code:  "ext_id",
				Value: strings.TrimSpace(extID),
			},
		},
	})
}

func (c *Client) UploadFile(ctx context.Context, fileName string, mimeType string, content []byte) (string, error) {
	if len(content) == 0 {
		return "", errors.New("не передано содержимое файла для загрузки в Pyrus")
	}
	token, apiURL, err := c.ensureAuthorized(ctx)
	if err != nil {
		return "", err
	}
	guid, status, err := c.doUploadWithToken(ctx, token, apiURL, fileName, mimeType, content)
	if err == nil {
		return guid, nil
	}
	if status != http.StatusUnauthorized {
		return "", err
	}

	c.clearAuth()
	token, apiURL, err = c.ensureAuthorized(ctx)
	if err != nil {
		return "", err
	}
	guid, _, err = c.doUploadWithToken(ctx, token, apiURL, fileName, mimeType, content)
	if err != nil {
		return "", err
	}
	return guid, nil
}

func (c *Client) DownloadFile(ctx context.Context, fileID int64) (*DownloadedFile, error) {
	if fileID <= 0 {
		return nil, fmt.Errorf("некорректный file_id")
	}
	body, fileName, mimeType, err := c.doDownload(ctx, "files/download/"+strconv.FormatInt(fileID, 10))
	if err != nil {
		return nil, err
	}
	return &DownloadedFile{
		FileName: fileName,
		MimeType: mimeType,
		Content:  body,
	}, nil
}

func (c *Client) doUploadWithToken(
	ctx context.Context,
	token string,
	apiURL string,
	fileName string,
	mimeType string,
	content []byte,
) (string, int, error) {
	safeFileName := strings.TrimSpace(fileName)
	if safeFileName == "" {
		safeFileName = "attachment.bin"
	}
	safeMimeType := strings.TrimSpace(mimeType)
	if safeMimeType == "" {
		safeMimeType = "application/octet-stream"
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, safeFileName))
	header.Set("Content-Type", safeMimeType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", 0, err
	}
	if _, err = part.Write(content); err != nil {
		return "", 0, err
	}
	if err = writer.Close(); err != nil {
		return "", 0, err
	}

	startedAt := time.Now()
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: исходящая загрузка файла",
			"method", http.MethodPost,
			"path", "files/upload",
			"file_name", safeFileName,
			"mime_type", safeMimeType,
			"content_bytes", len(content),
		)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinAPIURL(apiURL, "files/upload"), &body)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, err
	}
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: ответ загрузки файла",
			"method", http.MethodPost,
			"path", "files/upload",
			"file_name", safeFileName,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_preview", truncateForPyrusClientLog(string(respBody), pyrusClientLogPreviewLimit),
		)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", resp.StatusCode, formatPyrusHTTPError(resp.StatusCode, respBody)
	}

	var result struct {
		GUID string `json:"guid"`
	}
	if err = json.Unmarshal(respBody, &result); err != nil {
		return "", resp.StatusCode, err
	}
	result.GUID = strings.TrimSpace(result.GUID)
	if result.GUID == "" {
		return "", resp.StatusCode, errors.New("Pyrus не вернул guid загруженного файла")
	}
	return result.GUID, resp.StatusCode, nil
}

func (c *Client) doDownload(ctx context.Context, apiPath string) ([]byte, string, string, error) {
	token, apiURL, err := c.ensureAuthorized(ctx)
	if err != nil {
		return nil, "", "", err
	}
	body, fileName, mimeType, status, err := c.doDownloadWithToken(ctx, token, apiURL, apiPath)
	if err == nil {
		return body, fileName, mimeType, nil
	}
	if status != http.StatusUnauthorized {
		return nil, "", "", err
	}

	c.clearAuth()
	token, apiURL, err = c.ensureAuthorized(ctx)
	if err != nil {
		return nil, "", "", err
	}
	body, fileName, mimeType, _, err = c.doDownloadWithToken(ctx, token, apiURL, apiPath)
	if err != nil {
		return nil, "", "", err
	}
	return body, fileName, mimeType, nil
}

func (c *Client) doDownloadWithToken(ctx context.Context, token string, apiURL string, apiPath string) ([]byte, string, string, int, error) {
	startedAt := time.Now()
	if c.logger != nil {
		c.logger.Debug("Pyrus API: исходящий download-запрос", "method", http.MethodGet, "path", apiPath)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinAPIURL(apiURL, apiPath), nil)
	if err != nil {
		return nil, "", "", 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/octet-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, "", "", 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", "", resp.StatusCode, err
	}
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: ответ download-запроса",
			"method", http.MethodGet,
			"path", apiPath,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_bytes", len(body),
		)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", resp.StatusCode, formatPyrusHTTPError(resp.StatusCode, body)
	}

	fileName := fileNameFromHeader(resp.Header.Get("Content-Disposition"))
	mimeType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	return body, fileName, mimeType, resp.StatusCode, nil
}

func (c *Client) doJSON(ctx context.Context, method string, apiPath string, requestBody any, responseBody any) error {
	token, apiURL, err := c.ensureAuthorized(ctx)
	if err != nil {
		return err
	}
	status, err := c.doJSONWithToken(ctx, method, token, apiURL, apiPath, requestBody, responseBody)
	if err == nil {
		return nil
	}
	if status != http.StatusUnauthorized {
		return err
	}

	c.clearAuth()
	token, apiURL, err = c.ensureAuthorized(ctx)
	if err != nil {
		return err
	}
	_, err = c.doJSONWithToken(ctx, method, token, apiURL, apiPath, requestBody, responseBody)
	return err
}

func (c *Client) doJSONWithToken(
	ctx context.Context,
	method string,
	token string,
	apiURL string,
	apiPath string,
	requestBody any,
	responseBody any,
) (int, error) {
	var bodyReader io.Reader
	requestPreview := ""
	if requestBody != nil {
		payload, err := json.Marshal(requestBody)
		if err != nil {
			return 0, err
		}
		bodyReader = bytes.NewReader(payload)
		requestPreview = truncateForPyrusClientLog(string(payload), pyrusClientLogPreviewLimit)
	}
	startedAt := time.Now()
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: исходящий JSON-запрос",
			"method", method,
			"path", apiPath,
			"has_body", requestBody != nil,
			"request_preview", requestPreview,
		)
	}

	req, err := http.NewRequestWithContext(ctx, method, joinAPIURL(apiURL, apiPath), bodyReader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if requestBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: ответ JSON-запроса",
			"method", method,
			"path", apiPath,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_preview", truncateForPyrusClientLog(string(responseBytes), pyrusClientLogPreviewLimit),
		)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, formatPyrusHTTPError(resp.StatusCode, responseBytes)
	}
	if responseBody == nil || len(bytes.TrimSpace(responseBytes)) == 0 {
		return resp.StatusCode, nil
	}
	if err := json.Unmarshal(responseBytes, responseBody); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func (c *Client) ensureAuthorized(ctx context.Context) (string, string, error) {
	if !c.IsConfigured() {
		return "", "", errors.New("клиент Pyrus не настроен")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if strings.TrimSpace(c.accessToken) != "" && strings.TrimSpace(c.apiURL) != "" {
		return c.accessToken, c.apiURL, nil
	}

	reqBody := map[string]string{
		"login":        c.login,
		"security_key": c.securityKey,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pyrusAuthURL, bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	startedAt := time.Now()
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: запрос авторизации",
			"url", pyrusAuthURL,
			"login", c.login,
			"security_key_present", strings.TrimSpace(c.securityKey) != "",
		)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	if c.logger != nil {
		c.logger.Debug(
			"Pyrus API: ответ авторизации",
			"url", pyrusAuthURL,
			"status", resp.StatusCode,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"response_preview", truncateForPyrusClientLog(string(respBody), pyrusClientLogPreviewLimit),
		)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", formatPyrusHTTPError(resp.StatusCode, respBody)
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
		APIURL      string `json:"api_url"`
		FilesURL    string `json:"files_url"`
	}
	if err := json.Unmarshal(respBody, &authResp); err != nil {
		return "", "", err
	}
	authResp.AccessToken = strings.TrimSpace(authResp.AccessToken)
	if authResp.AccessToken == "" {
		return "", "", errors.New("Pyrus не вернул access_token")
	}

	c.accessToken = authResp.AccessToken
	c.apiURL = normalizeAPIBaseURL(authResp.APIURL)
	if c.apiURL == "" {
		c.apiURL = c.configBaseURL
	}
	c.filesURL = strings.TrimSpace(authResp.FilesURL)
	if c.logger != nil {
		c.logger.Debug("Pyrus: получен новый access_token", "api_url", c.apiURL)
	}
	return c.accessToken, c.apiURL, nil
}

func (c *Client) clearAuth() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = ""
	c.apiURL = ""
	c.filesURL = ""
}

func normalizeAPIBaseURL(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.HasSuffix(value, "/") {
		value += "/"
	}
	return value
}

func joinAPIURL(base string, apiPath string) string {
	baseValue := normalizeAPIBaseURL(base)
	pathValue := strings.TrimLeft(strings.TrimSpace(apiPath), "/")
	if baseValue == "" {
		return pathValue
	}
	u, err := url.Parse(baseValue)
	if err != nil {
		return baseValue + pathValue
	}
	u.Path = path.Join(u.Path, pathValue)
	if !strings.HasSuffix(u.Path, pathValue) {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + pathValue
	}
	return u.String()
}

func formatPyrusHTTPError(statusCode int, responseBody []byte) error {
	type pyrusErrorResponse struct {
		Error      string `json:"error"`
		Message    string `json:"message"`
		ErrorCode  string `json:"error_code"`
		ErrorTitle string `json:"error_title"`
	}
	var payload pyrusErrorResponse
	if err := json.Unmarshal(responseBody, &payload); err == nil {
		message := strings.TrimSpace(payload.Error)
		if message == "" {
			message = strings.TrimSpace(payload.Message)
		}
		if message == "" {
			message = strings.TrimSpace(payload.ErrorTitle)
		}
		if message != "" {
			return &HTTPError{
				StatusCode: statusCode,
				Code:       strings.TrimSpace(payload.ErrorCode),
				Message:    message,
			}
		}
	}

	bodyText := strings.TrimSpace(string(responseBody))
	if bodyText == "" {
		return &HTTPError{StatusCode: statusCode}
	}
	return &HTTPError{
		StatusCode: statusCode,
		Message:    bodyText,
	}
}

func fileNameFromHeader(contentDisposition string) string {
	value := strings.TrimSpace(contentDisposition)
	if value == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(params["filename"])
}

func truncateForPyrusClientLog(value string, limit int) string {
	trimmed := strings.TrimSpace(value)
	if limit <= 0 || len(trimmed) <= limit {
		return trimmed
	}
	return trimmed[:limit] + "...(truncated)"
}
