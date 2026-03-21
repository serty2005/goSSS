package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"etalon-agent/internal/fiscalmitsu/domain"
)

const (
	ProtocolVersion = "1"
	AdapterID       = "fiscal-mitsu"
	AdapterType     = "fiscal-mitsu"
	TaskTypeCollect = "collect"
)

type DescribeRequest struct {
	ProtocolVersion string `json:"protocol_version"`
	RequestID       string `json:"request_id,omitempty"`
}

type DescribeResponse struct {
	AdapterID       string   `json:"adapter_id"`
	AdapterType     string   `json:"adapter_type"`
	Version         string   `json:"version"`
	TargetOS        string   `json:"target_os"`
	TargetArch      string   `json:"target_arch"`
	ProtocolVersion string   `json:"protocol_version"`
	Capabilities    []string `json:"capabilities"`
}

func NewDescribeResponse(version string) DescribeResponse {
	return DescribeResponse{
		AdapterID:       AdapterID,
		AdapterType:     AdapterType,
		Version:         strings.TrimSpace(version),
		TargetOS:        runtime.GOOS,
		TargetArch:      runtime.GOARCH,
		ProtocolVersion: ProtocolVersion,
		Capabilities: []string{
			"run-task",
			"collect",
			"multi-device",
		},
	}
}

type HealthRequest struct {
	ProtocolVersion string `json:"protocol_version"`
	RequestID       string `json:"request_id,omitempty"`
	TimeoutSeconds  int    `json:"timeout_seconds,omitempty"`
}

type HealthResponse struct {
	Status    string         `json:"status"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"`
}

type RunRequest struct {
	ProtocolVersion string          `json:"protocol_version"`
	RequestID       string          `json:"request_id,omitempty"`
	TaskType        string          `json:"task_type"`
	Payload         RunPayloadInput `json:"payload"`
}

type RunPayloadInput struct {
	Devices []EndpointInput `json:"devices"`
}

type EndpointInput struct {
	Transport string      `json:"transport"`
	COMPort   string      `json:"com_port,omitempty"`
	BaudRate  rawJSONText `json:"baudrate,omitempty"`
	IP        string      `json:"ip,omitempty"`
	Port      rawJSONText `json:"port,omitempty"`
}

type RunResponse struct {
	Status    string                `json:"status"`
	Message   string                `json:"message,omitempty"`
	Result    *domain.CollectResult `json:"result,omitempty"`
	Warnings  []string              `json:"warnings,omitempty"`
	ErrorCode string                `json:"error_code,omitempty"`
}

type ErrorResponse struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code,omitempty"`
}

func EnsureProtocol(version string) error {
	if strings.TrimSpace(version) != ProtocolVersion {
		return fmt.Errorf("неподдерживаемая версия протокола %q, ожидается %q", strings.TrimSpace(version), ProtocolVersion)
	}
	return nil
}

func (in EndpointInput) ToDomain() (domain.Endpoint, error) {
	endpoint := domain.Endpoint{
		Transport: domain.Transport(strings.ToLower(strings.TrimSpace(in.Transport))),
		COMPort:   strings.TrimSpace(in.COMPort),
		BaudRate:  strings.TrimSpace(in.BaudRate.String()),
		IP:        strings.TrimSpace(in.IP),
	}

	port, err := in.Port.Int()
	if err != nil {
		return endpoint, fmt.Errorf("поле port: %w", err)
	}
	endpoint.Port = port

	if endpoint.Transport == domain.TransportCOM && endpoint.BaudRate == "" {
		endpoint.BaudRate = "115200"
	}
	if endpoint.Transport == domain.TransportTCP && endpoint.Port == 0 {
		endpoint.Port = 8200
	}
	if err := endpoint.Validate(); err != nil {
		return endpoint, err
	}
	return endpoint, nil
}

type rawJSONText []byte

func (v *rawJSONText) UnmarshalJSON(data []byte) error {
	*v = rawJSONText(bytes.Clone(data))
	return nil
}

func (v rawJSONText) String() string {
	value, err := v.stringValue()
	if err != nil {
		return ""
	}
	return value
}

func (v rawJSONText) Int() (int, error) {
	value, err := v.stringValue()
	if err != nil {
		return 0, err
	}
	if value == "" {
		return 0, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("ожидалось целое число, получено %q", value)
	}
	return number, nil
}

func (v rawJSONText) stringValue() (string, error) {
	if len(v) == 0 {
		return "", nil
	}

	trimmed := strings.TrimSpace(string(v))
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", nil
	}

	if strings.HasPrefix(trimmed, "\"") {
		var text string
		if err := json.Unmarshal(v, &text); err != nil {
			return "", err
		}
		return strings.TrimSpace(text), nil
	}

	return trimmed, nil
}
