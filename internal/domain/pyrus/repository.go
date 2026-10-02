package pyrus

import (
	"context"
	"time"
)

type IncomingEventListFilter struct {
	Status []string
	// TaskID ограничивает список событиями одной задачи Pyrus; 0 - без ограничения.
	TaskID int64
	Limit  int
	Offset int
}

// IncomingTaskGroupFilter описывает выборку задач Pyrus, сгруппированных по входящим событиям.
type IncomingTaskGroupFilter struct {
	// OnlyProblem оставляет задачи, у которых есть события в статусах failed или waiting.
	OnlyProblem bool
	Limit       int
	Offset      int
}

// IncomingTaskGroup - сводка входящих событий по одной задаче Pyrus.
type IncomingTaskGroup struct {
	PyrusTaskID     int64      `gorm:"column:pyrus_task_id"`
	TicketID        *string    `gorm:"column:ticket_id"`
	EventsTotal     int64      `gorm:"column:events_total"`
	NewCount        int64      `gorm:"column:new_count"`
	QueuedCount     int64      `gorm:"column:queued_count"`
	ProcessingCount int64      `gorm:"column:processing_count"`
	WaitingCount    int64      `gorm:"column:waiting_count"`
	FailedCount     int64      `gorm:"column:failed_count"`
	DoneCount       int64      `gorm:"column:done_count"`
	IgnoredCount    int64      `gorm:"column:ignored_count"`
	FirstReceivedAt time.Time  `gorm:"column:first_received_at"`
	LastReceivedAt  time.Time  `gorm:"column:last_received_at"`
	LastEventID     string     `gorm:"column:last_event_id"`
	LastError       *string    `gorm:"column:last_error"`
	NextRetryAt     *time.Time `gorm:"column:next_retry_at"`
}

type OutgoingEventListFilter struct {
	Status []string
	Limit  int
	Offset int
}

type Repository interface {
	UpsertTicketLink(ctx context.Context, link *TicketLink) error
	GetTicketLinkByTicketID(ctx context.Context, ticketID string) (*TicketLink, error)
	GetTicketLinksByTicketIDs(ctx context.Context, ticketIDs []string) (map[string]TicketLink, error)
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

	UpsertTicketContext(ctx context.Context, item *TicketContext) error
	GetTicketContextByTicketID(ctx context.Context, ticketID string) (*TicketContext, error)
	GetTicketContextByTaskID(ctx context.Context, taskID int64) (*TicketContext, error)

	InsertIncomingEventIfNotExists(ctx context.Context, event *IncomingEvent) (bool, error)
	ResetIncomingEventForReplay(ctx context.Context, id string) error
	MarkIncomingQueued(ctx context.Context, id string) error
	MarkIncomingProcessing(ctx context.Context, id string) error
	MarkIncomingDone(ctx context.Context, id string) error
	MarkIncomingFailed(ctx context.Context, id string, errText string) error
	// MarkIncomingExpired окончательно переводит событие в failed и выставляет счётчик попыток, чтобы автоповтор его больше не брал.
	MarkIncomingExpired(ctx context.Context, id string, errText string, attempts int) error
	// MarkIncomingWaiting переводит событие в ожидание данных и не засчитывает текущую попытку в лимит повторов.
	MarkIncomingWaiting(ctx context.Context, id string, reason string, waitStartedAt time.Time, nextRetryAt time.Time) error
	MarkIncomingIgnored(ctx context.Context, id string, reason string) error
	// ListIncomingDueForProcessing возвращает новые события, неисчерпавшие попыток failed и waiting-события, чей срок повтора наступил.
	ListIncomingDueForProcessing(ctx context.Context, limit int, maxAttempts int) ([]IncomingEvent, error)
	ListIncomingEvents(ctx context.Context, filter IncomingEventListFilter) ([]IncomingEvent, int64, error)
	GetIncomingEventByID(ctx context.Context, id string) (*IncomingEvent, error)
	GetIncomingEventsByIDs(ctx context.Context, ids []string) ([]IncomingEvent, error)
	// ListReplayableIncomingEventsByTask возвращает события задачи в статусах failed и waiting в хронологическом порядке.
	ListReplayableIncomingEventsByTask(ctx context.Context, taskID int64) ([]IncomingEvent, error)
	// ListProblemTaskIDs возвращает задачи, у которых есть события в статусах failed или waiting.
	ListProblemTaskIDs(ctx context.Context) ([]int64, error)
	ListIncomingTaskGroups(ctx context.Context, filter IncomingTaskGroupFilter) ([]IncomingTaskGroup, int64, error)

	InsertOutgoingEvent(ctx context.Context, event *OutgoingEvent) error
	MarkOutgoingProcessing(ctx context.Context, id string) error
	MarkOutgoingDone(ctx context.Context, id string) error
	MarkOutgoingFailed(ctx context.Context, id string, errText string) error
	MarkOutgoingIgnored(ctx context.Context, id string, reason string) error
	ListOutgoingEventsForRetry(ctx context.Context, limit int, maxAttempts int) ([]OutgoingEvent, error)
	ListOutgoingEvents(ctx context.Context, filter OutgoingEventListFilter) ([]OutgoingEvent, int64, error)
	GetOutgoingEventByID(ctx context.Context, id string) (*OutgoingEvent, error)
}
