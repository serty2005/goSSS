package telephony

import (
	"context"
	"time"
)

type IncomingEventListFilter struct {
	Cmd    []string
	Status []string
	Limit  int
	Offset int
}

type Repository interface {
	ReplaceProviderEmployees(ctx context.Context, provider string, items []ProviderEmployee) error
	ListProviderEmployees(ctx context.Context, provider string) ([]ProviderEmployee, error)
	GetProviderEmployee(ctx context.Context, provider string, login string) (*ProviderEmployee, error)

	GetCallByExternalID(ctx context.Context, provider string, externalCallID string) (*Call, error)
	GetCallByAnyExternalID(ctx context.Context, provider string, externalCallID string) (*Call, error)
	UpsertCall(ctx context.Context, call *Call) error
	AddCallEvent(ctx context.Context, event *CallEvent) error
	MergeCalls(ctx context.Context, target *Call, sourceCallID string) error

	InsertIncomingEventIfNotExists(ctx context.Context, event *IncomingEvent) (bool, error)
	ResetIncomingEventForReplay(ctx context.Context, id string) error
	MarkIncomingQueued(ctx context.Context, id string) error
	TryMarkIncomingProcessing(ctx context.Context, id string) (bool, error)
	MarkIncomingDone(ctx context.Context, id string) error
	MarkIncomingFailed(ctx context.Context, id string, errText string) error
	MarkIncomingIgnored(ctx context.Context, id string, reason string) error
	ListIncomingNewOrFailedForEnqueue(ctx context.Context, limit int, maxAttempts int) ([]IncomingEvent, error)
	ListIncomingQueuedForRecovery(ctx context.Context, limit int, queuedBefore time.Time) ([]IncomingEvent, error)
	ListIncomingEvents(ctx context.Context, filter IncomingEventListFilter) ([]IncomingEvent, int64, error)
	GetIncomingEventByID(ctx context.Context, id string) (*IncomingEvent, error)
}
