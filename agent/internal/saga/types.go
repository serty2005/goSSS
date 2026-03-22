package saga

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCanceled  Status = "canceled"
	StatusTimeout   Status = "timeout"
)

type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
	StepStatusFailed    StepStatus = "failed"
	StepStatusSkipped   StepStatus = "skipped"
	StepStatusCanceled  StepStatus = "canceled"
	StepStatusTimeout   StepStatus = "timeout"
)

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

type IdempotencyHint struct {
	Key  string
	Mode string
}

type Step struct {
	ID       string
	Name     string
	Type     string
	Timeout  time.Duration
	Input    json.RawMessage
	Metadata map[string]string
}

type Request struct {
	SagaID          string
	SagaType        string
	RequestID       string
	CorrelationID   string
	Timeout         time.Duration
	Input           json.RawMessage
	Steps           []Step
	RetryPolicy     RetryPolicy
	IdempotencyHint IdempotencyHint
	Metadata        map[string]string
}

type StepResult struct {
	ID          string
	Name        string
	Type        string
	Status      StepStatus
	StartedAt   *time.Time
	CompletedAt *time.Time
	DurationMS  int64
	Attempts    int
	Input       json.RawMessage
	Output      json.RawMessage
	Error       string
	Metadata    map[string]string
}

type LogEntry struct {
	Timestamp time.Time
	Level     string
	Event     string
	StepID    string
	Message   string
	Details   map[string]any
}

type Result struct {
	SagaID         string
	SagaType       string
	RequestID      string
	CorrelationID  string
	Status         Status
	StartedAt      time.Time
	CompletedAt    *time.Time
	Duration       time.Duration
	FinalResult    json.RawMessage
	Steps          []StepResult
	ExecutionLog   []LogEntry
	Error          string
	Resumed        bool
	IdempotencyKey string
}

type Journal struct {
	Request       Request
	Status        Status
	StartedAt     time.Time
	CompletedAt   *time.Time
	FinalResult   json.RawMessage
	Steps         []StepResult
	ExecutionLog  []LogEntry
	Error         string
	LastUpdatedAt time.Time
	Resumed       bool
}

type StepOutcome struct {
	Status      StepStatus
	Output      json.RawMessage
	Stop        bool
	FinalStatus Status
	FinalResult json.RawMessage
}

type Execution struct {
	Request Request
	Journal *Journal
}

func (e *Execution) Input() json.RawMessage {
	return cloneRawMessage(e.Request.Input)
}

func (e *Execution) StepResult(stepID string) (StepResult, bool) {
	for _, result := range e.Journal.Steps {
		if result.ID == stepID {
			return cloneStepResult(result), true
		}
	}
	return StepResult{}, false
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	return append(json.RawMessage(nil), value...)
}

func cloneStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func cloneDetails(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}

	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil
	}
	return cloned
}

func cloneStep(step Step) Step {
	return Step{
		ID:       step.ID,
		Name:     step.Name,
		Type:     step.Type,
		Timeout:  step.Timeout,
		Input:    cloneRawMessage(step.Input),
		Metadata: cloneStringMap(step.Metadata),
	}
}

func cloneRequest(request Request) Request {
	clonedSteps := make([]Step, 0, len(request.Steps))
	for _, step := range request.Steps {
		clonedSteps = append(clonedSteps, cloneStep(step))
	}

	return Request{
		SagaID:          request.SagaID,
		SagaType:        request.SagaType,
		RequestID:       request.RequestID,
		CorrelationID:   request.CorrelationID,
		Timeout:         request.Timeout,
		Input:           cloneRawMessage(request.Input),
		Steps:           clonedSteps,
		RetryPolicy:     request.RetryPolicy,
		IdempotencyHint: request.IdempotencyHint,
		Metadata:        cloneStringMap(request.Metadata),
	}
}

func cloneStepResult(value StepResult) StepResult {
	cloned := StepResult{
		ID:         value.ID,
		Name:       value.Name,
		Type:       value.Type,
		Status:     value.Status,
		DurationMS: value.DurationMS,
		Attempts:   value.Attempts,
		Input:      cloneRawMessage(value.Input),
		Output:     cloneRawMessage(value.Output),
		Error:      value.Error,
		Metadata:   cloneStringMap(value.Metadata),
	}
	if value.StartedAt != nil {
		startedAt := value.StartedAt.UTC()
		cloned.StartedAt = &startedAt
	}
	if value.CompletedAt != nil {
		completedAt := value.CompletedAt.UTC()
		cloned.CompletedAt = &completedAt
	}
	return cloned
}

func cloneLogEntry(value LogEntry) LogEntry {
	return LogEntry{
		Timestamp: value.Timestamp.UTC(),
		Level:     value.Level,
		Event:     value.Event,
		StepID:    value.StepID,
		Message:   value.Message,
		Details:   cloneDetails(value.Details),
	}
}

func cloneJournal(journal Journal) Journal {
	clonedSteps := make([]StepResult, 0, len(journal.Steps))
	for _, step := range journal.Steps {
		clonedSteps = append(clonedSteps, cloneStepResult(step))
	}

	clonedLog := make([]LogEntry, 0, len(journal.ExecutionLog))
	for _, entry := range journal.ExecutionLog {
		clonedLog = append(clonedLog, cloneLogEntry(entry))
	}

	cloned := Journal{
		Request:       cloneRequest(journal.Request),
		Status:        journal.Status,
		StartedAt:     journal.StartedAt.UTC(),
		FinalResult:   cloneRawMessage(journal.FinalResult),
		Steps:         clonedSteps,
		ExecutionLog:  clonedLog,
		Error:         journal.Error,
		LastUpdatedAt: journal.LastUpdatedAt.UTC(),
		Resumed:       journal.Resumed,
	}
	if journal.CompletedAt != nil {
		completedAt := journal.CompletedAt.UTC()
		cloned.CompletedAt = &completedAt
	}
	return cloned
}
