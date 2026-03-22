package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/models"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"strings"

	"gorm.io/datatypes"
)

const (
	agentCommandTypeSagaRun      = "saga_run"
	agentSelfUpdateSagaType      = "agent_self_update"
	defaultSagaRequestTimeoutSec = 300
)

func (s *agentOperatorFlowService) EnqueueSagaRun(ctx context.Context, agentUUID string, req api.EnqueueAgentSagaRunRequestDTO, actor string) error {
	agentUUID = strings.TrimSpace(agentUUID)
	if agentUUID == "" {
		return errors.New("uuid агента обязателен")
	}
	_ = actor

	var agent models.Agent
	if err := s.db.WithContext(ctx).Where("uuid = ?", agentUUID).First(&agent).Error; err != nil {
		return err
	}
	return s.enqueueSagaRunLocked(ctx, &agent, req.Payload, false)
}

func BuildAgentSelfUpdateSagaPayload(sagaID string, input api.AgentSelfUpdateSagaInputDTO) (api.AgentSagaRunCommandPayloadDTO, error) {
	sagaID = strings.TrimSpace(sagaID)
	if sagaID == "" {
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("saga_id обязателен")
	}

	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	input.DownloadURL = strings.TrimSpace(input.DownloadURL)
	input.SHA256 = strings.TrimSpace(strings.ToLower(input.SHA256))
	input.FileName = strings.TrimSpace(input.FileName)
	input.RestartPolicy = strings.TrimSpace(input.RestartPolicy)
	if input.RestartPolicy == "" {
		input.RestartPolicy = "immediate"
	}

	rawInput, err := json.Marshal(input)
	if err != nil {
		return api.AgentSagaRunCommandPayloadDTO{}, fmt.Errorf("не удалось сериализовать input agent_self_update: %w", err)
	}

	return api.AgentSagaRunCommandPayloadDTO{
		SagaID:         sagaID,
		SagaType:       agentSelfUpdateSagaType,
		RequestID:      sagaID,
		TimeoutSeconds: defaultSagaRequestTimeoutSec,
		Input:          rawInput,
		IdempotencyHint: api.AgentSagaIdempotencyHintDTO{
			Key: sagaID,
		},
	}, nil
}

func (s *agentOperatorFlowService) enqueueSagaRunLocked(ctx context.Context, agent *models.Agent, payload api.AgentSagaRunCommandPayloadDTO, skipIfPending bool) error {
	if agent == nil {
		return errors.New("агент не найден")
	}

	normalized, err := normalizeSagaRunPayload(payload)
	if err != nil {
		return err
	}

	pending, err := s.hasPendingSagaRunCommand(ctx, agent.UUID, normalized.SagaID)
	if err != nil {
		return err
	}
	if pending {
		if skipIfPending {
			return nil
		}
		return fmt.Errorf("для saga %s уже есть незавершённая команда", normalized.SagaID)
	}

	raw, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать payload saga_run: %w", err)
	}

	return s.db.WithContext(ctx).Create(&models.AgentCommand{
		AgentUUID: agent.UUID,
		Type:      agentCommandTypeSagaRun,
		Payload:   datatypes.JSON(raw),
		Status:    "new",
	}).Error
}

func (s *agentOperatorFlowService) hasPendingSagaRunCommand(ctx context.Context, agentUUID, sagaID string) (bool, error) {
	var commands []models.AgentCommand
	if err := s.db.WithContext(ctx).
		Where("agent_uuid = ? AND status IN ?", agentUUID, []string{"new", "sent"}).
		Where("type = ?", agentCommandTypeSagaRun).
		Find(&commands).Error; err != nil {
		return false, err
	}

	for _, command := range commands {
		var payload api.AgentSagaRunCommandPayloadDTO
		if err := json.Unmarshal(command.Payload, &payload); err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(payload.SagaID), strings.TrimSpace(sagaID)) {
			return true, nil
		}
	}
	return false, nil
}

func normalizeSagaRunPayload(payload api.AgentSagaRunCommandPayloadDTO) (api.AgentSagaRunCommandPayloadDTO, error) {
	payload.SagaID = strings.TrimSpace(payload.SagaID)
	payload.SagaType = strings.TrimSpace(payload.SagaType)
	payload.RequestID = strings.TrimSpace(payload.RequestID)
	payload.CorrelationID = strings.TrimSpace(payload.CorrelationID)
	payload.Timeout = strings.TrimSpace(payload.Timeout)
	payload.IdempotencyHint.Key = strings.TrimSpace(payload.IdempotencyHint.Key)
	payload.IdempotencyHint.Mode = strings.TrimSpace(payload.IdempotencyHint.Mode)

	switch {
	case payload.SagaID == "":
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("payload saga_run должен содержать saga_id")
	case payload.SagaType == "":
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("payload saga_run должен содержать saga_type")
	}

	if payload.RequestID == "" {
		payload.RequestID = payload.SagaID
	}
	if payload.IdempotencyHint.Key == "" {
		payload.IdempotencyHint.Key = payload.SagaID
	}
	if payload.TimeoutSeconds < 0 {
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("timeout_seconds не может быть отрицательным")
	}
	if payload.RetryPolicy.MaxAttempts < 0 {
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("retry_policy.max_attempts не может быть отрицательным")
	}
	if payload.RetryPolicy.BackoffSeconds < 0 {
		return api.AgentSagaRunCommandPayloadDTO{}, errors.New("retry_policy.backoff_seconds не может быть отрицательным")
	}
	return payload, nil
}
