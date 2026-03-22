package saga

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"etalon-agent/internal/protocol"
)

const AgentSelfUpdateSagaType = "agent_self_update"

type AgentSelfUpdateDefinition struct{}

func NewAgentSelfUpdateDefinition() *AgentSelfUpdateDefinition {
	return &AgentSelfUpdateDefinition{}
}

func (d *AgentSelfUpdateDefinition) Type() string {
	return AgentSelfUpdateSagaType
}

func (d *AgentSelfUpdateDefinition) BuildPlan(task protocol.SagaRunTaskPayload) (Request, error) {
	input, normalizedInput, err := decodeSelfUpdateInput(task.Input)
	if err != nil {
		return Request{}, err
	}

	steps, err := buildSelfUpdateSteps(normalizedInput, task.Steps)
	if err != nil {
		return Request{}, err
	}

	timeout, err := resolveDuration(task.Timeout, task.TimeoutSeconds)
	if err != nil {
		return Request{}, fmt.Errorf("невалидный timeout saga: %w", err)
	}

	backoff, err := resolveDuration(task.RetryPolicy.Backoff, task.RetryPolicy.BackoffSeconds)
	if err != nil {
		return Request{}, fmt.Errorf("невалидный retry_policy.backoff: %w", err)
	}

	requestID := strings.TrimSpace(task.RequestID)
	if requestID == "" {
		requestID = strings.TrimSpace(task.SagaID)
	}

	idempotencyKey := strings.TrimSpace(task.IdempotencyHint.Key)
	if idempotencyKey == "" {
		idempotencyKey = strings.TrimSpace(task.SagaID)
	}

	if input.FileName != "" {
		input.FileName = sanitizeOptionalFileName(input.FileName)
	}

	return Request{
		SagaID:        strings.TrimSpace(task.SagaID),
		SagaType:      strings.TrimSpace(task.SagaType),
		RequestID:     requestID,
		CorrelationID: strings.TrimSpace(task.CorrelationID),
		Timeout:       timeout,
		Input:         normalizedInput,
		Steps:         steps,
		RetryPolicy: RetryPolicy{
			MaxAttempts: max(task.RetryPolicy.MaxAttempts, 1),
			Backoff:     backoff,
		},
		IdempotencyHint: IdempotencyHint{
			Key:  idempotencyKey,
			Mode: strings.TrimSpace(task.IdempotencyHint.Mode),
		},
		Metadata: cloneStringMap(task.Metadata),
	}, nil
}

func decodeSelfUpdateInput(raw json.RawMessage) (protocol.AgentSelfUpdateSagaInput, json.RawMessage, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage(`{}`)
	}

	var input protocol.AgentSelfUpdateSagaInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return protocol.AgentSelfUpdateSagaInput{}, nil, fmt.Errorf("невалидный input agent_self_update: %w", err)
	}

	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	input.DownloadURL = strings.TrimSpace(input.DownloadURL)
	input.SHA256 = strings.TrimSpace(strings.ToLower(input.SHA256))
	input.FileName = sanitizeOptionalFileName(input.FileName)
	input.RestartPolicy = normalizeRestartPolicy(input.RestartPolicy)

	normalizedRaw, err := json.Marshal(input)
	if err != nil {
		return protocol.AgentSelfUpdateSagaInput{}, nil, fmt.Errorf("не удалось сериализовать input agent_self_update: %w", err)
	}
	return input, normalizedRaw, nil
}

func buildSelfUpdateSteps(defaultInput json.RawMessage, steps []protocol.SagaStepDefinition) ([]Step, error) {
	if len(steps) == 0 {
		return []Step{
			{
				ID:    "preflight",
				Name:  "Предварительная валидация",
				Type:  "runner.self_update_preflight",
				Input: cloneRawMessage(defaultInput),
			},
			{
				ID:    "version_check",
				Name:  "Проверка целевой версии",
				Type:  "runner.self_update_target_version_check",
				Input: cloneRawMessage(defaultInput),
			},
			{
				ID:    "metadata_check",
				Name:  "Проверка метаданных загрузки",
				Type:  "runner.self_update_download_metadata_check",
				Input: cloneRawMessage(defaultInput),
			},
			{
				ID:    "apply_update",
				Name:  "Нативное самообновление",
				Type:  "native.agent_self_update",
				Input: cloneRawMessage(defaultInput),
			},
		}, nil
	}

	out := make([]Step, 0, len(steps))
	for index, item := range steps {
		timeout, err := resolveDuration(item.Timeout, item.TimeoutSeconds)
		if err != nil {
			return nil, fmt.Errorf("шаг %s: невалидный timeout: %w", defaultStepID(item, index), err)
		}

		stepInput := item.Input
		if len(strings.TrimSpace(string(stepInput))) == 0 {
			stepInput = cloneRawMessage(defaultInput)
		}

		out = append(out, Step{
			ID:       defaultStepID(item, index),
			Name:     defaultStepName(item, index),
			Type:     strings.TrimSpace(item.Type),
			Timeout:  timeout,
			Input:    cloneRawMessage(stepInput),
			Metadata: cloneStringMap(item.Metadata),
		})
	}
	return out, nil
}

func resolveDuration(raw string, seconds int) (time.Duration, error) {
	if seconds > 0 {
		return time.Duration(seconds) * time.Second, nil
	}
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, err
	}
	if duration <= 0 {
		return 0, fmt.Errorf("duration должна быть больше нуля")
	}
	return duration, nil
}

func defaultStepID(item protocol.SagaStepDefinition, index int) string {
	if id := strings.TrimSpace(item.ID); id != "" {
		return id
	}
	if stepType := strings.TrimSpace(item.Type); stepType != "" {
		return fmt.Sprintf("step_%d_%s", index+1, strings.ReplaceAll(stepType, ".", "_"))
	}
	return fmt.Sprintf("step_%d", index+1)
}

func defaultStepName(item protocol.SagaStepDefinition, index int) string {
	if name := strings.TrimSpace(item.Name); name != "" {
		return name
	}
	if stepType := strings.TrimSpace(item.Type); stepType != "" {
		return stepType
	}
	return fmt.Sprintf("step_%d", index+1)
}

func normalizeRestartPolicy(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "immediate":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func sanitizeOptionalFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(value)
	if base == "." {
		return ""
	}
	return base
}

func ValidateSelfUpdateDownloadMetadata(input protocol.AgentSelfUpdateSagaInput) error {
	switch {
	case strings.TrimSpace(input.DownloadURL) == "":
		return fmt.Errorf("в input agent_self_update отсутствует download_url")
	}

	if _, err := url.ParseRequestURI(strings.TrimSpace(input.DownloadURL)); err != nil {
		return fmt.Errorf("download_url не является корректным URL: %w", err)
	}
	if sha := strings.TrimSpace(input.SHA256); sha != "" {
		if len(sha) != 64 {
			return fmt.Errorf("sha256 должен содержать 64 hex-символа")
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return fmt.Errorf("sha256 содержит неhex-символы: %w", err)
		}
	}
	return nil
}
