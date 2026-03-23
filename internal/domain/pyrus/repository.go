package pyrus

import "context"

type IncomingEventListFilter struct {
	Status []string
	Limit  int
	Offset int
}

type OutgoingEventListFilter struct {
	Status []string
	Limit  int
	Offset int
}

type Repository interface {
	UpsertTicketLink(ctx context.Context, link *TicketLink) error
	GetTicketLinkByTicketID(ctx context.Context, ticketID string) (*TicketLink, error)
	GetTicketLinkByTaskID(ctx context.Context, taskID int64) (*TicketLink, error)

	UpsertCommentLink(ctx context.Context, link *CommentLink) error
	GetCommentLinkByEtalonID(ctx context.Context, etalonCommentID string) (*CommentLink, error)
	GetCommentLinkByPyrusCommentID(ctx context.Context, pyrusCommentID int64) (*CommentLink, error)

	UpsertFileLink(ctx context.Context, link *FileLink) error
	GetFileLinkByLocalFileID(ctx context.Context, localFileID string) (*FileLink, error)
	GetFileLinkByPyrusAttachmentID(ctx context.Context, pyrusAttachmentID int64) (*FileLink, error)

	UpsertUserMap(ctx context.Context, item *UserMap) error
	GetUserMapByEtalonID(ctx context.Context, etalonUserID uint) (*UserMap, error)
	GetUserMapByPyrusID(ctx context.Context, pyrusUserID int64) (*UserMap, error)

	InsertIncomingEventIfNotExists(ctx context.Context, event *IncomingEvent) (bool, error)
	ResetIncomingEventForReplay(ctx context.Context, id string) error
	MarkIncomingQueued(ctx context.Context, id string) error
	MarkIncomingProcessing(ctx context.Context, id string) error
	MarkIncomingDone(ctx context.Context, id string) error
	MarkIncomingFailed(ctx context.Context, id string, errText string) error
	MarkIncomingIgnored(ctx context.Context, id string, reason string) error
	ListIncomingNewOrFailedForEnqueue(ctx context.Context, limit int, maxAttempts int) ([]IncomingEvent, error)
	ListIncomingEvents(ctx context.Context, filter IncomingEventListFilter) ([]IncomingEvent, int64, error)
	GetIncomingEventByID(ctx context.Context, id string) (*IncomingEvent, error)

	InsertOutgoingEvent(ctx context.Context, event *OutgoingEvent) error
	MarkOutgoingProcessing(ctx context.Context, id string) error
	MarkOutgoingDone(ctx context.Context, id string) error
	MarkOutgoingFailed(ctx context.Context, id string, errText string) error
	MarkOutgoingIgnored(ctx context.Context, id string, reason string) error
	ListOutgoingEventsForRetry(ctx context.Context, limit int, maxAttempts int) ([]OutgoingEvent, error)
	ListOutgoingEvents(ctx context.Context, filter OutgoingEventListFilter) ([]OutgoingEvent, int64, error)
	GetOutgoingEventByID(ctx context.Context, id string) (*OutgoingEvent, error)
}
