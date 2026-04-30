package contract

import (
	"encoding/json"
	"fmt"
	"strings"

	"etalon-agent/internal/iikosyrverms/domain"
)

const (
	ProtocolVersion           = "1"
	AdapterID                 = "iiko-syrve-rms"
	AdapterType               = "iiko-syrve-rms"
	TargetOS                  = "windows"
	TargetArch                = "amd64"
	TaskTypeCollect           = "collect"
	TaskTypeSoftShutdownFront = "soft_shutdown_front"
	TaskTypeInspectAutorun    = "inspect_autorun"
	TaskTypeEnsureAutorun     = "ensure_autorun"
	TaskTypeReadFrontConfig   = "read_front_config"

	AutorunMethodStartupUser   = "startup_user"
	AutorunMethodStartupCommon = "startup_common"
	AutorunMethodScheduler     = "scheduler"
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
			"read-crm-id",
			"list-plugins",
			"soft-shutdown-front",
			"inspect-autorun",
			"ensure-autorun",
			"read-front-config",
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
	SoftwareType  domain.SoftwareType    `json:"software_type"`
	RMSURL        string                 `json:"rms_url,omitempty"`
	CRMID         string                 `json:"crm_id,omitempty"`
	Plugins       []domain.PluginInfo    `json:"plugins,omitempty"`
	ProcessName   string                 `json:"process_name,omitempty"`
	MatchedPIDs   []uint32               `json:"matched_pids,omitempty"`
	WindowsClosed int                    `json:"windows_closed,omitempty"`
	CloseSent     bool                   `json:"close_sent,omitempty"`
	Entries       []domain.AutorunEntry  `json:"entries,omitempty"`
	Method        string                 `json:"method,omitempty"`
	Created       bool                   `json:"created,omitempty"`
	Updated       bool                   `json:"updated,omitempty"`
	Path          string                 `json:"path,omitempty"`
	TaskName      string                 `json:"task_name,omitempty"`
	SourceFile    string                 `json:"source_file,omitempty"`
	Settings      []domain.ConfigSetting `json:"settings,omitempty"`
}

type AutorunEnsurePayload struct {
	Method       string              `json:"method"`
	SoftwareType domain.SoftwareType `json:"software_type,omitempty"`
	Arguments    string              `json:"arguments,omitempty"`
	TaskName     string              `json:"task_name,omitempty"`
	ShortcutName string              `json:"shortcut_name,omitempty"`
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

func NormalizeTaskType(taskType string) string {
	return strings.ToLower(strings.TrimSpace(taskType))
}

func SupportedTaskTypes() []string {
	return []string{
		TaskTypeCollect,
		TaskTypeSoftShutdownFront,
		TaskTypeInspectAutorun,
		TaskTypeEnsureAutorun,
		TaskTypeReadFrontConfig,
	}
}

func IsSupportedTaskType(taskType string) bool {
	switch NormalizeTaskType(taskType) {
	case TaskTypeCollect, TaskTypeSoftShutdownFront, TaskTypeInspectAutorun, TaskTypeEnsureAutorun, TaskTypeReadFrontConfig:
		return true
	default:
		return false
	}
}
