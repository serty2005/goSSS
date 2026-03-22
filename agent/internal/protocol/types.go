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
	Status         string                  `json:"status"`
	AdapterID      string                  `json:"adapter_id,omitempty"`
	Command        string                  `json:"command,omitempty"`
	Operation      string                  `json:"operation,omitempty"`
	SagaID         string                  `json:"saga_id,omitempty"`
	SagaType       string                  `json:"saga_type,omitempty"`
	RequestID      string                  `json:"request_id,omitempty"`
	CorrelationID  string                  `json:"correlation_id,omitempty"`
	CompletedAt    *time.Time              `json:"completed_at,omitempty"`
	DurationMS     int64                   `json:"duration_ms,omitempty"`
	ExitCode       *int                    `json:"exit_code,omitempty"`
	Stdout         string                  `json:"stdout,omitempty"`
	Stderr         string                  `json:"stderr,omitempty"`
	Result         json.RawMessage         `json:"result,omitempty"`
	FinalResult    json.RawMessage         `json:"final_result,omitempty"`
	Steps          []SagaStepResult        `json:"steps,omitempty"`
	ExecutionLog   []SagaExecutionLogEntry `json:"execution_log,omitempty"`
	Resumed        bool                    `json:"resumed,omitempty"`
	IdempotencyKey string                  `json:"idempotency_key,omitempty"`
	Error          string                  `json:"error,omitempty"`
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

type SagaRunTaskPayload struct {
	SagaID          string               `json:"saga_id"`
	SagaType        string               `json:"saga_type"`
	RequestID       string               `json:"request_id,omitempty"`
	CorrelationID   string               `json:"correlation_id,omitempty"`
	Timeout         string               `json:"timeout,omitempty"`
	TimeoutSeconds  int                  `json:"timeout_seconds,omitempty"`
	Input           json.RawMessage      `json:"input,omitempty"`
	Steps           []SagaStepDefinition `json:"steps,omitempty"`
	RetryPolicy     SagaRetryPolicy      `json:"retry_policy,omitempty"`
	IdempotencyHint SagaIdempotencyHint  `json:"idempotency_hint,omitempty"`
	Metadata        map[string]string    `json:"metadata,omitempty"`
}

type SagaStepDefinition struct {
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Type           string            `json:"type"`
	Timeout        string            `json:"timeout,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Input          json.RawMessage   `json:"input,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

type SagaRetryPolicy struct {
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	Backoff        string `json:"backoff,omitempty"`
	BackoffSeconds int    `json:"backoff_seconds,omitempty"`
}

type SagaIdempotencyHint struct {
	Key  string `json:"key,omitempty"`
	Mode string `json:"mode,omitempty"`
}

type AgentSelfUpdateSagaInput struct {
	TargetVersion string   `json:"target_version"`
	DownloadURL   string   `json:"download_url"`
	SHA256        string   `json:"sha256,omitempty"`
	FileName      string   `json:"file_name,omitempty"`
	RestartPolicy string   `json:"restart_policy,omitempty"`
	Args          []string `json:"args,omitempty"`
}

type SagaAdapterStepInput struct {
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

type SagaExternalCommandStepInput struct {
	Executable     string            `json:"executable"`
	Args           []string          `json:"args,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	Timeout        string            `json:"timeout,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Stdin          json.RawMessage   `json:"stdin,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
}

type SagaStepResult struct {
	ID          string            `json:"id,omitempty"`
	Name        string            `json:"name,omitempty"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CompletedAt *time.Time        `json:"completed_at,omitempty"`
	DurationMS  int64             `json:"duration_ms,omitempty"`
	Attempts    int               `json:"attempts,omitempty"`
	Input       json.RawMessage   `json:"input,omitempty"`
	Output      json.RawMessage   `json:"output,omitempty"`
	Error       string            `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type SagaExecutionLogEntry struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Event     string         `json:"event"`
	StepID    string         `json:"step_id,omitempty"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitempty"`
}
