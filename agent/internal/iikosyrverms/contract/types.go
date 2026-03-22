package contract

import (
	"encoding/json"
	"fmt"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"
)

const (
	ProtocolVersion = "1"
	AdapterID       = "iiko-syrve-rms"
	AdapterType     = "iiko-syrve-rms"
	TargetOS        = "windows"
	TargetArch      = "amd64"
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
		TargetOS:        TargetOS,
		TargetArch:      TargetArch,
		ProtocolVersion: ProtocolVersion,
		Capabilities: []string{
			"inventory",
			"run-task",
			"collect",
			"detect-rms",
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
	Payload         json.RawMessage `json:"payload,omitempty"`
}

type RunResult struct {
	RMSURL       string              `json:"rms_url,omitempty"`
	SoftwareType domain.SoftwareType `json:"software_type"`
}

type RunResponse struct {
	Status    string         `json:"status"`
	Message   string         `json:"message,omitempty"`
	Result    RunResult      `json:"result"`
	Details   map[string]any `json:"details,omitempty"`
	Warnings  []string       `json:"warnings,omitempty"`
	ErrorCode string         `json:"error_code,omitempty"`
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
