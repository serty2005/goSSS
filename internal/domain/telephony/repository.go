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

type CallListFilter struct {
	Provider          string
	EmployeeUserID    *uint
	ClientPhone       string
	Statuses          []string
	GroupNames        []string
	StartedFrom       *time.Time
	StartedTo         *time.Time
	OnlyMissed        bool
	OnlyWithoutTicket bool
	Limit             int
	Offset            int
}

type Repository interface {
	ReplaceProviderEmployees(ctx context.Context, provider string, items []ProviderEmployee) error
	ListProviderEmployees(ctx context.Context, provider string) ([]ProviderEmployee, error)
	GetProviderEmployee(ctx context.Context, provider string, login string) (*ProviderEmployee, error)
	UpdateProviderEmployeeStatus(ctx context.Context, provider string, login string, status string, seenAt time.Time) error

	GetCallByExternalID(ctx context.Context, provider string, externalCallID string) (*Call, error)
	GetCallByAnyExternalID(ctx context.Context, provider string, externalCallID string) (*Call, error)
	GetCallByID(ctx context.Context, id string) (*Call, error)
	UpsertCall(ctx context.Context, call *Call) error
	SyncCallEmployeeUser(ctx context.Context, provider string, login string, userID *uint) error
	IsCallHistoryRangeCovered(ctx context.Context, provider string, employeeLogin *string, startedFrom time.Time, startedTo time.Time) (bool, error)
	MarkCallHistoryRangeCovered(ctx context.Context, provider string, employeeLogin *string, startedFrom time.Time, startedTo time.Time, syncedAt time.Time) error
	ListCalls(ctx context.Context, filter CallListFilter) ([]Call, int64, error)
	AddCallEvent(ctx context.Context, event *CallEvent) error
	GetCallArtifact(ctx context.Context, callID string, artifactType string) (*CallArtifact, error)
	UpsertCallArtifact(ctx context.Context, artifact *CallArtifact) error
	DeleteCallArtifact(ctx context.Context, callID string, artifactType string) error
	ListExpiredCallArtifacts(ctx context.Context, artifactType string, olderThan time.Time, limit int) ([]CallArtifact, error)
	ClearCallRecording(ctx context.Context, callID string) error
	MergeCalls(ctx context.Context, target *Call, sourceCallID string) error
	UpsertCallTicketLink(ctx context.Context, link *CallTicketLink) error
	GetCallTicketLink(ctx context.Context, callID string) (*CallTicketLink, error)
	ListCallTicketLinks(ctx context.Context, callIDs []string) ([]CallTicketLink, error)
	ListCallsByTicketID(ctx context.Context, ticketID string) ([]Call, error)
	DeleteCallTicketLink(ctx context.Context, callID string, ticketID string) error

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

	EnsureContact(ctx context.Context, normalizedPhone string, displayPhone string) (*Contact, error)
	UpsertContact(ctx context.Context, input ContactUpsert) (*Contact, error)
	GetContactByID(ctx context.Context, id uint) (*Contact, error)
	GetContactByPhone(ctx context.Context, normalizedPhone string) (*Contact, error)
	UpsertContactCompanyLink(ctx context.Context, contactID uint, companyID string, lastSeenAt time.Time) error
	ListContactCompanyLinks(ctx context.Context, contactID uint) ([]ContactCompanyLink, error)

	GetPendingContextByID(ctx context.Context, id string) (*PendingContext, error)
	GetPendingContextByExternalCallID(ctx context.Context, externalCallID string) (*PendingContext, error)
	GetActivePendingContextByUserID(ctx context.Context, userID uint, now time.Time) (*PendingContext, error)
	UpsertPendingContext(ctx context.Context, item *PendingContext) error
	UpdatePendingContext(ctx context.Context, id string, status string, linkedTicketID *string, decisionReason *string) error
}
