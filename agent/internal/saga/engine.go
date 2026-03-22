package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type EngineOptions struct {
	Store    Store
	Handlers *StepRegistry
	Debug    bool
	Infof    func(string, ...any)
	Debugf   func(string, ...any)
}

type Engine struct {
	store    Store
	handlers *StepRegistry
	debug    bool
	infof    func(string, ...any)
	debugf   func(string, ...any)
}

func NewEngine(options EngineOptions) (*Engine, error) {
	switch {
	case options.Store == nil:
		return nil, fmt.Errorf("store saga не задан")
	case options.Handlers == nil:
		return nil, fmt.Errorf("registry step handlers не задан")
	default:
		return &Engine{
			store:    options.Store,
			handlers: options.Handlers,
			debug:    options.Debug,
			infof:    options.Infof,
			debugf:   options.Debugf,
		}, nil
	}
}

func (e *Engine) Execute(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return newFailedResult(request, err), err
	}

	executionCtx := ctx
	cancel := func() {}
	if request.Timeout > 0 {
		executionCtx, cancel = context.WithTimeoutCause(
			ctx,
			request.Timeout,
			fmt.Errorf("превышен таймаут саги %s", request.SagaID),
		)
	}
	defer cancel()

	journal, reuseCompleted, err := e.loadOrCreateJournal(executionCtx, request)
	if err != nil {
		return newFailedResult(request, err), err
	}
	if reuseCompleted {
		e.emitLog(journal, "info", "saga.reused", "", "Повторный запуск вернул ранее завершённый результат", map[string]any{
			"status": journal.Status,
		})
		return e.resultFromJournal(*journal), nil
	}

	execution := &Execution{
		Request: cloneRequest(request),
		Journal: journal,
	}

	e.emitLog(journal, "info", "saga.started", "", startMessage(journal.Resumed), map[string]any{
		"saga_type":      request.SagaType,
		"steps_count":    len(request.Steps),
		"request_id":     request.RequestID,
		"correlation_id": request.CorrelationID,
	})
	if e.debug {
		e.emitLog(journal, "debug", "saga.input", "", "Получен payload saga", map[string]any{
			"input":    decodeRawJSON(request.Input),
			"metadata": cloneStringMap(request.Metadata),
		})
	}
	if err := e.saveJournal(executionCtx, *journal); err != nil {
		return newFailedResult(request, err), err
	}

	for index, step := range request.Steps {
		handler, ok := e.handlers.Resolve(step.Type)
		if !ok {
			err := fmt.Errorf("неизвестный тип шага saga: %s", step.Type)
			return e.failSaga(executionCtx, journal, step, err)
		}

		if existing, ok := findTerminalStepResult(journal.Steps, step.ID); ok {
			e.emitLog(journal, "info", "step.resume_skip", step.ID, "Шаг уже был завершён в предыдущем запуске и пропущен", map[string]any{
				"step_type": existing.Type,
				"status":    existing.Status,
				"index":     index,
			})
			if err := e.saveJournal(executionCtx, *journal); err != nil {
				return newFailedResult(request, err), err
			}
			continue
		}

		stepResult := StepResult{
			ID:       step.ID,
			Name:     step.Name,
			Type:     step.Type,
			Status:   StepStatusRunning,
			Input:    cloneRawMessage(step.Input),
			Metadata: cloneStringMap(step.Metadata),
		}
		startedAt := time.Now().UTC()
		stepResult.StartedAt = &startedAt
		setStepResult(&journal.Steps, stepResult)
		e.emitLog(journal, "info", "step.started", step.ID, fmt.Sprintf("Старт шага %s", step.Name), map[string]any{
			"step_type": step.Type,
			"index":     index,
		})
		if e.debug {
			e.emitLog(journal, "debug", "step.input", step.ID, "Входные параметры шага", map[string]any{
				"input": decodeRawJSON(step.Input),
			})
		}
		if err := e.saveJournal(executionCtx, *journal); err != nil {
			return newFailedResult(request, err), err
		}

		outcome, stepErr, stepCause, attempts := e.executeStep(executionCtx, execution, step, handler, request.RetryPolicy)
		updateStepAttempts(&journal.Steps, step.ID, attempts)
		if stepErr != nil {
			if wrapped := stepExecutionError(stepErr); wrapped != nil {
				updateStepOutput(&journal.Steps, step.ID, wrapped.Output)
			}
			return e.failStepAndSaga(executionCtx, journal, step, stepErr, stepCause)
		}

		finalStepStatus := outcome.Status
		if finalStepStatus == "" {
			finalStepStatus = StepStatusCompleted
		}
		completedAt := time.Now().UTC()
		updateCompletedStep(&journal.Steps, step.ID, finalStepStatus, completedAt, outcome.Output)

		if e.debug {
			e.emitLog(journal, "debug", "step.output", step.ID, "Результат шага", map[string]any{
				"output": decodeRawJSON(outcome.Output),
			})
		}
		e.emitLog(journal, "info", "step.completed", step.ID, fmt.Sprintf("Шаг %s завершён", step.Name), map[string]any{
			"status":      finalStepStatus,
			"duration_ms": stepDuration(journal.Steps, step.ID),
		})

		if outcome.Stop {
			return e.completeSaga(executionCtx, journal, outcome.FinalStatus, chooseFinalResult(outcome.FinalResult, outcome.Output))
		}
		if err := e.saveJournal(executionCtx, *journal); err != nil {
			return newFailedResult(request, err), err
		}
	}

	return e.completeSaga(executionCtx, journal, StatusCompleted, buildDefaultFinalResult(*journal))
}

func (e *Engine) executeStep(ctx context.Context, execution *Execution, step Step, handler StepHandler, retry RetryPolicy) (StepOutcome, error, error, int) {
	maxAttempts := retry.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	backoff := retry.Backoff
	if backoff < 0 {
		backoff = 0
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		updateStepAttempts(&execution.Journal.Steps, step.ID, attempt)
		if attempt > 1 {
			e.emitLog(execution.Journal, "info", "step.retry", step.ID, "Повторная попытка шага", map[string]any{
				"attempt": attempt,
			})
			if backoff > 0 {
				if err := waitWithContext(ctx, backoff); err != nil {
					return StepOutcome{}, err, context.Cause(ctx), attempt
				}
			}
		}

		stepCtx := ctx
		cancel := func() {}
		if step.Timeout > 0 {
			stepCtx, cancel = context.WithTimeoutCause(
				ctx,
				step.Timeout,
				fmt.Errorf("превышен таймаут шага %s", step.ID),
			)
		}

		outcome, err := handler.Execute(stepCtx, execution, step)
		stepCause := context.Cause(stepCtx)
		cancel()
		if err == nil {
			return outcome, nil, stepCause, attempt
		}
		if attempt >= maxAttempts {
			return StepOutcome{}, err, stepCause, attempt
		}
	}

	return StepOutcome{}, fmt.Errorf("внутренняя ошибка цикла retry шага %s", step.ID), nil, maxAttempts
}

func (e *Engine) completeSaga(ctx context.Context, journal *Journal, status Status, finalResult json.RawMessage) (Result, error) {
	if status == "" {
		status = StatusCompleted
	}
	completedAt := time.Now().UTC()
	journal.Status = status
	journal.CompletedAt = &completedAt
	journal.FinalResult = cloneRawMessage(finalResult)
	journal.Error = ""
	journal.LastUpdatedAt = completedAt
	e.emitLog(journal, "info", "saga.completed", "", "Сага завершена", map[string]any{
		"status":      status,
		"duration_ms": completedAt.Sub(journal.StartedAt).Milliseconds(),
	})
	if e.debug {
		e.emitLog(journal, "debug", "saga.final_result", "", "Финальный результат саги", map[string]any{
			"final_result": decodeRawJSON(finalResult),
		})
	}
	if err := e.saveJournal(ctx, *journal); err != nil {
		return newFailedResult(journal.Request, err), err
	}
	return e.resultFromJournal(*journal), nil
}

func (e *Engine) failSaga(ctx context.Context, journal *Journal, step Step, err error) (Result, error) {
	return e.failStepAndSaga(ctx, journal, step, err, nil)
}

func (e *Engine) failStepAndSaga(ctx context.Context, journal *Journal, step Step, err error, cause error) (Result, error) {
	status, stepStatus := classifyFailure(err, cause)
	completedAt := time.Now().UTC()
	updateFailedStep(&journal.Steps, step.ID, stepStatus, completedAt, err)
	journal.Status = status
	journal.CompletedAt = &completedAt
	journal.Error = strings.TrimSpace(err.Error())
	journal.LastUpdatedAt = completedAt
	e.emitLog(journal, "info", "step.failed", step.ID, fmt.Sprintf("Шаг %s завершился ошибкой", step.Name), map[string]any{
		"status": stepStatus,
		"error":  err.Error(),
	})
	e.emitLog(journal, "info", "saga.failed", "", "Сага остановлена на ошибке", map[string]any{
		"status":  status,
		"error":   err.Error(),
		"step_id": step.ID,
	})
	if saveErr := e.saveJournal(ctx, *journal); saveErr != nil {
		combinedErr := errors.Join(err, journalError(saveErr))
		return newFailedResult(journal.Request, combinedErr), combinedErr
	}
	return e.resultFromJournal(*journal), err
}

func (e *Engine) loadOrCreateJournal(ctx context.Context, request Request) (*Journal, bool, error) {
	existing, err := e.store.Load(ctx, request.SagaID)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		now := time.Now().UTC()
		return &Journal{
			Request:       cloneRequest(request),
			Status:        StatusRunning,
			StartedAt:     now,
			LastUpdatedAt: now,
		}, false, nil
	}

	if err := ensureSameRequest(existing.Request, request); err != nil {
		return nil, false, err
	}

	existing.Resumed = true
	existing.LastUpdatedAt = time.Now().UTC()
	switch existing.Status {
	case StatusCompleted:
		return existing, true, nil
	default:
		existing.Status = StatusRunning
		existing.CompletedAt = nil
		existing.Error = ""
		return existing, false, nil
	}
}

func (e *Engine) resultFromJournal(journal Journal) Result {
	result := Result{
		SagaID:         journal.Request.SagaID,
		SagaType:       journal.Request.SagaType,
		RequestID:      journal.Request.RequestID,
		CorrelationID:  journal.Request.CorrelationID,
		Status:         journal.Status,
		StartedAt:      journal.StartedAt.UTC(),
		FinalResult:    cloneRawMessage(journal.FinalResult),
		Error:          journal.Error,
		Resumed:        journal.Resumed,
		IdempotencyKey: journal.Request.IdempotencyHint.Key,
	}
	if journal.CompletedAt != nil {
		completedAt := journal.CompletedAt.UTC()
		result.CompletedAt = &completedAt
		result.Duration = completedAt.Sub(journal.StartedAt)
	}
	result.Steps = make([]StepResult, 0, len(journal.Steps))
	for _, step := range journal.Steps {
		result.Steps = append(result.Steps, cloneStepResult(step))
	}
	result.ExecutionLog = make([]LogEntry, 0, len(journal.ExecutionLog))
	for _, entry := range journal.ExecutionLog {
		result.ExecutionLog = append(result.ExecutionLog, cloneLogEntry(entry))
	}
	return result
}

func (e *Engine) emitLog(journal *Journal, level, event, stepID, message string, details map[string]any) {
	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     strings.TrimSpace(level),
		Event:     strings.TrimSpace(event),
		StepID:    strings.TrimSpace(stepID),
		Message:   strings.TrimSpace(message),
		Details:   cloneDetails(details),
	}
	journal.ExecutionLog = append(journal.ExecutionLog, entry)
	journal.LastUpdatedAt = entry.Timestamp

	prefix := fmt.Sprintf("Saga %s/%s", journal.Request.SagaType, journal.Request.SagaID)
	if stepID != "" {
		prefix += " step=" + stepID
	}

	switch level {
	case "debug":
		if e.debug && e.debugf != nil {
			e.debugf("%s: %s details=%s", prefix, entry.Message, compactJSON(details))
		}
	default:
		if e.infof != nil {
			e.infof("%s: %s", prefix, entry.Message)
		}
	}
}

func (e *Engine) saveJournal(ctx context.Context, journal Journal) error {
	journal.LastUpdatedAt = time.Now().UTC()
	return e.store.Save(ctx, journal)
}

func validateRequest(request Request) error {
	switch {
	case strings.TrimSpace(request.SagaID) == "":
		return fmt.Errorf("поле saga_id обязательно")
	case strings.TrimSpace(request.SagaType) == "":
		return fmt.Errorf("поле saga_type обязательно")
	case len(request.Steps) == 0:
		return fmt.Errorf("сценарий saga не содержит шагов")
	}

	seen := make(map[string]struct{}, len(request.Steps))
	for index, step := range request.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("шаг #%d не содержит id", index+1)
		}
		if strings.TrimSpace(step.Type) == "" {
			return fmt.Errorf("шаг %s не содержит type", step.ID)
		}
		if _, exists := seen[step.ID]; exists {
			return fmt.Errorf("дублирующийся step id %s", step.ID)
		}
		seen[step.ID] = struct{}{}
	}
	return nil
}

func ensureSameRequest(existing, current Request) error {
	switch {
	case existing.SagaID != current.SagaID:
		return fmt.Errorf("обнаружен state-file с другим saga_id: %s", existing.SagaID)
	case existing.SagaType != current.SagaType:
		return fmt.Errorf("для saga_id %s уже сохранён другой saga_type: %s", current.SagaID, existing.SagaType)
	case !sameRawJSON(existing.Input, current.Input):
		return fmt.Errorf("для saga_id %s уже сохранён другой input payload", current.SagaID)
	case len(existing.Steps) != len(current.Steps):
		return fmt.Errorf("для saga_id %s уже сохранён другой план шагов", current.SagaID)
	}

	for index := range existing.Steps {
		if existing.Steps[index].ID != current.Steps[index].ID || existing.Steps[index].Type != current.Steps[index].Type || !sameRawJSON(existing.Steps[index].Input, current.Steps[index].Input) {
			return fmt.Errorf("для saga_id %s уже сохранён другой шаг %d", current.SagaID, index+1)
		}
	}
	return nil
}

func sameRawJSON(left, right json.RawMessage) bool {
	return compactJSON(decodeRawJSON(left)) == compactJSON(decodeRawJSON(right))
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeRawJSON(raw json.RawMessage) any {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}
	}

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return value
}

func stepExecutionError(err error) *StepExecutionError {
	var wrapped *StepExecutionError
	if errors.As(err, &wrapped) {
		return wrapped
	}
	return nil
}

func classifyFailure(err error, cause error) (Status, StepStatus) {
	switch {
	case errors.Is(cause, context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return StatusTimeout, StepStatusTimeout
	case errors.Is(cause, context.Canceled), errors.Is(err, context.Canceled):
		return StatusCanceled, StepStatusCanceled
	default:
		return StatusFailed, StepStatusFailed
	}
}

func findTerminalStepResult(results []StepResult, stepID string) (StepResult, bool) {
	for _, result := range results {
		if result.ID == stepID && (result.Status == StepStatusCompleted || result.Status == StepStatusSkipped) {
			return cloneStepResult(result), true
		}
	}
	return StepResult{}, false
}

func setStepResult(results *[]StepResult, value StepResult) {
	for index := range *results {
		if (*results)[index].ID == value.ID {
			(*results)[index] = cloneStepResult(value)
			return
		}
	}
	*results = append(*results, cloneStepResult(value))
}

func updateStepAttempts(results *[]StepResult, stepID string, attempts int) {
	for index := range *results {
		if (*results)[index].ID == stepID {
			(*results)[index].Attempts = attempts
			return
		}
	}
}

func updateStepOutput(results *[]StepResult, stepID string, output json.RawMessage) {
	for index := range *results {
		if (*results)[index].ID == stepID {
			(*results)[index].Output = cloneRawMessage(output)
			return
		}
	}
}

func updateCompletedStep(results *[]StepResult, stepID string, status StepStatus, completedAt time.Time, output json.RawMessage) {
	for index := range *results {
		if (*results)[index].ID != stepID {
			continue
		}
		item := &(*results)[index]
		item.Status = status
		item.CompletedAt = &completedAt
		if item.StartedAt != nil {
			item.DurationMS = completedAt.Sub(*item.StartedAt).Milliseconds()
		}
		item.Output = cloneRawMessage(output)
		item.Error = ""
		return
	}
}

func updateFailedStep(results *[]StepResult, stepID string, status StepStatus, completedAt time.Time, err error) {
	for index := range *results {
		if (*results)[index].ID != stepID {
			continue
		}
		item := &(*results)[index]
		item.Status = status
		item.CompletedAt = &completedAt
		if item.StartedAt != nil {
			item.DurationMS = completedAt.Sub(*item.StartedAt).Milliseconds()
		}
		item.Error = strings.TrimSpace(err.Error())
		return
	}
}

func stepDuration(results []StepResult, stepID string) int64 {
	for _, result := range results {
		if result.ID == stepID {
			return result.DurationMS
		}
	}
	return 0
}

func buildDefaultFinalResult(journal Journal) json.RawMessage {
	for index := len(journal.Steps) - 1; index >= 0; index-- {
		if len(strings.TrimSpace(string(journal.Steps[index].Output))) > 0 {
			return cloneRawMessage(journal.Steps[index].Output)
		}
	}

	raw, err := json.Marshal(map[string]any{
		"saga_id":         journal.Request.SagaID,
		"saga_type":       journal.Request.SagaType,
		"status":          journal.Status,
		"completed_steps": len(journal.Steps),
	})
	if err != nil {
		return nil
	}
	return raw
}

func chooseFinalResult(values ...json.RawMessage) json.RawMessage {
	for _, value := range values {
		if len(strings.TrimSpace(string(value))) > 0 {
			return cloneRawMessage(value)
		}
	}
	return nil
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func startMessage(resumed bool) string {
	if resumed {
		return "Возобновление саги после найденного state-file"
	}
	return "Старт саги"
}

func journalError(err error) error {
	return fmt.Errorf("не удалось сохранить journal saga: %w", err)
}

func newFailedResult(request Request, err error) Result {
	now := time.Now().UTC()
	return Result{
		SagaID:         request.SagaID,
		SagaType:       request.SagaType,
		RequestID:      request.RequestID,
		CorrelationID:  request.CorrelationID,
		Status:         StatusFailed,
		StartedAt:      now,
		CompletedAt:    &now,
		Error:          strings.TrimSpace(err.Error()),
		IdempotencyKey: request.IdempotencyHint.Key,
	}
}
