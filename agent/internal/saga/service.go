package saga

import (
	"context"
	"fmt"
	"strings"
	"time"

	"etalon-agent/internal/protocol"
)

type ServiceOptions struct {
	Definitions *DefinitionRegistry
	Engine      *Engine
}

type Service struct {
	definitions *DefinitionRegistry
	engine      *Engine
}

func NewService(options ServiceOptions) (*Service, error) {
	switch {
	case options.Definitions == nil:
		return nil, fmt.Errorf("registry definitions saga не задан")
	case options.Engine == nil:
		return nil, fmt.Errorf("engine saga не задан")
	default:
		return &Service{
			definitions: options.Definitions,
			engine:      options.Engine,
		}, nil
	}
}

func (s *Service) Execute(ctx context.Context, task protocol.SagaRunTaskPayload) (protocol.TaskExecutionResult, error) {
	definition, ok := s.definitions.Resolve(task.SagaType)
	if !ok {
		err := fmt.Errorf("неизвестный saga_type: %s", strings.TrimSpace(task.SagaType))
		return failedTaskExecutionResult(task, err), err
	}

	request, err := definition.BuildPlan(task)
	if err != nil {
		return failedTaskExecutionResult(task, err), err
	}

	result, err := s.engine.Execute(ctx, request)
	return taskExecutionResultFromResult(result), err
}

func taskExecutionResultFromResult(result Result) protocol.TaskExecutionResult {
	taskResult := protocol.TaskExecutionResult{
		Status:         string(result.Status),
		SagaID:         result.SagaID,
		SagaType:       result.SagaType,
		RequestID:      result.RequestID,
		CorrelationID:  result.CorrelationID,
		Result:         cloneRawMessage(result.FinalResult),
		FinalResult:    cloneRawMessage(result.FinalResult),
		Steps:          make([]protocol.SagaStepResult, 0, len(result.Steps)),
		ExecutionLog:   make([]protocol.SagaExecutionLogEntry, 0, len(result.ExecutionLog)),
		Resumed:        result.Resumed,
		IdempotencyKey: result.IdempotencyKey,
		Error:          result.Error,
	}
	if result.CompletedAt != nil {
		completedAt := result.CompletedAt.UTC()
		taskResult.CompletedAt = &completedAt
		taskResult.DurationMS = result.Duration.Milliseconds()
	}

	for _, step := range result.Steps {
		taskResult.Steps = append(taskResult.Steps, protocol.SagaStepResult{
			ID:          step.ID,
			Name:        step.Name,
			Type:        step.Type,
			Status:      string(step.Status),
			StartedAt:   cloneTimePointer(step.StartedAt),
			CompletedAt: cloneTimePointer(step.CompletedAt),
			DurationMS:  step.DurationMS,
			Attempts:    step.Attempts,
			Input:       cloneRawMessage(step.Input),
			Output:      cloneRawMessage(step.Output),
			Error:       step.Error,
			Metadata:    cloneStringMap(step.Metadata),
		})
	}

	for _, entry := range result.ExecutionLog {
		taskResult.ExecutionLog = append(taskResult.ExecutionLog, protocol.SagaExecutionLogEntry{
			Timestamp: entry.Timestamp.UTC(),
			Level:     entry.Level,
			Event:     entry.Event,
			StepID:    entry.StepID,
			Message:   entry.Message,
			Details:   cloneDetails(entry.Details),
		})
	}
	return taskResult
}

func failedTaskExecutionResult(task protocol.SagaRunTaskPayload, err error) protocol.TaskExecutionResult {
	completedAt := time.Now().UTC()
	return protocol.TaskExecutionResult{
		Status:         "failed",
		SagaID:         strings.TrimSpace(task.SagaID),
		SagaType:       strings.TrimSpace(task.SagaType),
		RequestID:      strings.TrimSpace(task.RequestID),
		CorrelationID:  strings.TrimSpace(task.CorrelationID),
		CompletedAt:    &completedAt,
		Error:          strings.TrimSpace(err.Error()),
		IdempotencyKey: strings.TrimSpace(task.IdempotencyHint.Key),
	}
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := value.UTC()
	return &copyValue
}
