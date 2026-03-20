package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"etalon-agent/internal/protocol"
)

type HTTPError struct {
	StatusCode int
	Body       string
	Method     string
	URL        string
	RetryAfter string
}

func (e *HTTPError) Error() string {
	if strings.TrimSpace(e.Method) == "" && strings.TrimSpace(e.URL) == "" {
		return fmt.Sprintf("http %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("%s %s: http %d: %s", e.Method, e.URL, e.StatusCode, e.Body)
}

type RequestBuildError struct {
	Method string
	URL    string
	Err    error
}

func (e *RequestBuildError) Error() string {
	return fmt.Sprintf("не удалось создать запрос %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *RequestBuildError) Unwrap() error {
	return e.Err
}

type RequestEncodeError struct {
	Method string
	URL    string
	Err    error
}

func (e *RequestEncodeError) Error() string {
	return fmt.Sprintf("не удалось сериализовать запрос %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *RequestEncodeError) Unwrap() error {
	return e.Err
}

type ResponseDecodeError struct {
	Method string
	URL    string
	Err    error
}

func (e *ResponseDecodeError) Error() string {
	return fmt.Sprintf("не удалось прочитать JSON-ответ %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *ResponseDecodeError) Unwrap() error {
	return e.Err
}

type TransportError struct {
	Method string
	URL    string
	Err    error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("ошибка запроса %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *TransportError) Unwrap() error {
	return e.Err
}

type ServiceDeskClient struct {
	baseURL string
	client  *http.Client
}

func NewServiceDeskClient(baseURL string) *ServiceDeskClient {
	return &ServiceDeskClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ServiceDeskClient) Register(ctx context.Context, bootstrapAPIKey string, req protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
	var resp protocol.AgentRegistrationResponseDTO
	if err := c.doJSON(ctx, http.MethodPost, "/api/agents/register", req, &resp, authHeader{Mode: authBearer, Token: bootstrapAPIKey}, http.StatusOK); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ServiceDeskClient) RefreshTokens(ctx context.Context, req protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error) {
	var resp protocol.AgentTokenRefreshResponseDTO
	if err := c.doJSON(ctx, http.MethodPost, "/api/agents/auth/refresh", req, &resp, authHeader{}, http.StatusOK); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ServiceDeskClient) SendHeartbeat(ctx context.Context, agentUUID string, data protocol.AgentDataDTO, accessToken string) (*protocol.HeartbeatResponseDTO, error) {
	var resp protocol.HeartbeatResponseDTO
	path := fmt.Sprintf("/api/agents/%s/data", agentUUID)
	if err := c.doJSON(ctx, http.MethodPost, path, data, &resp, authHeader{Mode: authBearer, Token: accessToken}, http.StatusOK); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *ServiceDeskClient) DownloadFile(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, &RequestBuildError{Method: http.MethodGet, URL: url, Err: err}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, &TransportError{Method: http.MethodGet, URL: url, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Method:     http.MethodGet,
			URL:        url,
			RetryAfter: strings.TrimSpace(resp.Header.Get("Retry-After")),
		}
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл обновления: %w", err)
	}
	return content, nil
}

type authMode int

const (
	authNone authMode = iota
	authBearer
)

type authHeader struct {
	Mode  authMode
	Token string
}

func (c *ServiceDeskClient) doJSON(ctx context.Context, method, path string, bodyIn any, bodyOut any, auth authHeader, okStatuses ...int) error {
	fullURL := c.baseURL + path
	var bodyReader io.Reader
	if bodyIn != nil {
		raw, err := json.Marshal(bodyIn)
		if err != nil {
			return &RequestEncodeError{Method: method, URL: fullURL, Err: err}
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return &RequestBuildError{Method: method, URL: fullURL, Err: err}
	}
	if bodyIn != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth.Mode == authBearer {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(auth.Token))
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return &TransportError{Method: method, URL: fullURL, Err: err}
	}
	defer resp.Body.Close()

	if !statusAllowed(resp.StatusCode, okStatuses) {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(body),
			Method:     method,
			URL:        fullURL,
			RetryAfter: strings.TrimSpace(resp.Header.Get("Retry-After")),
		}
	}
	if bodyOut == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(bodyOut); err != nil {
		return &ResponseDecodeError{Method: method, URL: fullURL, Err: err}
	}
	return nil
}

func statusAllowed(actual int, allowed []int) bool {
	for _, code := range allowed {
		if actual == code {
			return true
		}
	}
	return false
}
