package dtos

import (
	"encoding/json"
	"time"
)

type SagaStepResultDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Status      string            `json:"status"`
	StartedAt   *time.Time        `json:"started_at,omitzero"`
	CompletedAt *time.Time        `json:"completed_at,omitzero"`
	DurationMS  int64             `json:"duration_ms,omitzero"`
	Attempts    int               `json:"attempts,omitzero"`
	Input       json.RawMessage   `json:"input,omitempty"`
	Output      json.RawMessage   `json:"output,omitempty"`
	Error       string            `json:"error,omitempty"`
	Metadata    map[string]string `json:"metadata,omitzero"`
}

type SagaExecutionLogEntryDTO struct {
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Event     string         `json:"event"`
	StepID    string         `json:"step_id,omitempty"`
	Message   string         `json:"message,omitempty"`
	Details   map[string]any `json:"details,omitzero"`
}

type AgentSagaStepDefinitionDTO struct {
	ID             string            `json:"id,omitempty"`
	Name           string            `json:"name,omitempty"`
	Type           string            `json:"type"`
	Timeout        string            `json:"timeout,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Input          json.RawMessage   `json:"input,omitempty"`
	Metadata       map[string]string `json:"metadata,omitzero"`
}

type AgentSagaRetryPolicyDTO struct {
	MaxAttempts    int    `json:"max_attempts,omitempty"`
	Backoff        string `json:"backoff,omitempty"`
	BackoffSeconds int    `json:"backoff_seconds,omitempty"`
}

type AgentSagaIdempotencyHintDTO struct {
	Key  string `json:"key,omitempty"`
	Mode string `json:"mode,omitempty"`
}

type AgentSagaRunCommandPayloadDTO struct {
	SagaID          string                       `json:"saga_id"`
	SagaType        string                       `json:"saga_type"`
	RequestID       string                       `json:"request_id,omitempty"`
	CorrelationID   string                       `json:"correlation_id,omitempty"`
	Timeout         string                       `json:"timeout,omitempty"`
	TimeoutSeconds  int                          `json:"timeout_seconds,omitempty"`
	Input           json.RawMessage              `json:"input,omitempty"`
	Steps           []AgentSagaStepDefinitionDTO `json:"steps,omitzero"`
	RetryPolicy     AgentSagaRetryPolicyDTO      `json:"retry_policy"`
	IdempotencyHint AgentSagaIdempotencyHintDTO  `json:"idempotency_hint"`
	Metadata        map[string]string            `json:"metadata,omitzero"`
}

type AgentSelfUpdateSagaInputDTO struct {
	TargetVersion string   `json:"target_version"`
	DownloadURL   string   `json:"download_url"`
	SHA256        string   `json:"sha256,omitempty"`
	FileName      string   `json:"file_name,omitempty"`
	RestartPolicy string   `json:"restart_policy,omitempty"`
	Args          []string `json:"args,omitzero"`
}

type EnqueueAgentSagaRunRequestDTO struct {
	Payload AgentSagaRunCommandPayloadDTO `json:"payload"`
}
