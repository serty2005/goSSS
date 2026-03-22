package protocol

import (
	"encoding/json"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/inventory"
)

type AgentDataDTO struct {
	Hostname        string               `json:"hostname"`
	URLRms          string               `json:"url_rms,omitempty"`
	TeamviewerID    string               `json:"teamviewer_id,omitempty"`
	AnydeskID       string               `json:"anydesk_id,omitempty"`
	LitemanagerID   string               `json:"litemanager_id,omitempty"`
	RustdeskID      string               `json:"rustdesk_id,omitempty"`
	CurrentTime     string               `json:"current_time"`
	AgentVersion    string               `json:"agent_version"`
	AgentUUID       string               `json:"uuid,omitempty"`
	AgentType       string               `json:"agent_type,omitempty"`
	Inventory       *inventory.Snapshot  `json:"inventory,omitempty"`
	AdapterStatuses []adapters.Status    `json:"adapter_statuses,omitempty"`
	TaskResults     []AgentTaskResultDTO `json:"task_results,omitempty"`
}

type RegistrationRequestDTO struct {
	AgentUUID          string                 `json:"agent_uuid"`
	Hostname           string                 `json:"hostname"`
	AgentVersion       string                 `json:"agent_version"`
	InitialData        AgentDataDTO           `json:"initial_data"`
	MachineFingerprint string                 `json:"machine_fingerprint,omitempty"`
	SystemInfo         map[string]interface{} `json:"system_info,omitempty"`
}

type AgentTaskDTO struct {
	ID        uint            `json:"id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type TaskExecutionResult struct {
	Status      string          `json:"status"`
	AdapterID   string          `json:"adapter_id,omitempty"`
	Command     string          `json:"command,omitempty"`
	Operation   string          `json:"operation,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	DurationMS  int64           `json:"duration_ms,omitempty"`
	ExitCode    *int            `json:"exit_code,omitempty"`
	Stdout      string          `json:"stdout,omitempty"`
	Stderr      string          `json:"stderr,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
}

type AgentTaskResultDTO struct {
	ID   uint   `json:"id"`
	Type string `json:"type,omitempty"`
	TaskExecutionResult
}

type HeartbeatResponseDTO struct {
	Status           string                  `json:"status"`
	Tasks            []AgentTaskDTO          `json:"tasks,omitempty"`
	AdapterManifests []adapters.ManifestItem `json:"adapter_manifests,omitempty"`
}

type AgentRegistrationResponseDTO struct {
	Status                string    `json:"status"`
	Message               string    `json:"message,omitempty"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token,omitempty"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token,omitempty"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

type AgentTokenRefreshRequestDTO struct {
	AgentUUID    string `json:"agent_uuid"`
	RefreshToken string `json:"refresh_token"`
}

type AgentTokenRefreshResponseDTO struct {
	Status                string    `json:"status"`
	AgentUUID             string    `json:"agent_uuid"`
	AccessToken           string    `json:"access_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshToken          string    `json:"refresh_token"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
}

type SelfUpdateTaskPayload struct {
	Version     string   `json:"version"`
	DownloadURL string   `json:"download_url"`
	SHA256      string   `json:"sha256,omitempty"`
	FileName    string   `json:"file_name,omitempty"`
	Restart     *bool    `json:"restart,omitempty"`
	Args        []string `json:"args,omitempty"`
}

type AdapterRunTaskPayload struct {
	AdapterID       string          `json:"adapter_id"`
	Command         string          `json:"command,omitempty"`
	Operation       string          `json:"operation,omitempty"`
	Timeout         string          `json:"timeout,omitempty"`
	TimeoutSeconds  int             `json:"timeout_seconds,omitempty"`
	ProtocolVersion string          `json:"protocol_version,omitempty"`
	RequestID       string          `json:"request_id,omitempty"`
	DeviceParams    json.RawMessage `json:"device_params,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

type AdapterCommandInputDTO struct {
	ProtocolVersion string          `json:"protocol_version"`
	RequestID       string          `json:"request_id,omitempty"`
	TaskType        string          `json:"task_type,omitempty"`
	TimeoutSeconds  int             `json:"timeout_seconds,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}
