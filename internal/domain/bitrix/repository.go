package bitrix

import (
	"context"
	"time"
)

type Repository interface {
	UpsertDealLink(ctx context.Context, link *DealLink) error
	GetDealLinkByTicketID(ctx context.Context, ticketID string) (*DealLink, error)
	GetDealLinkByDealID(ctx context.Context, dealID int64) (*DealLink, error)
	DeleteDealLinkByTicketID(ctx context.Context, ticketID string) error
	UpsertIgnoredDeal(ctx context.Context, item *IgnoredDeal) error
	HasIgnoredDeal(ctx context.Context, dealID int64) (bool, error)

	UpsertCommentLink(ctx context.Context, link *CommentLink) error
	GetCommentLinkByEtalonID(ctx context.Context, etalonCommentID string) (*CommentLink, error)
	GetCommentLinkByB24ID(ctx context.Context, b24CommentID int64) (*CommentLink, error)
	DeleteCommentLinksByTicketID(ctx context.Context, ticketID string) error
	// InsertCommentLinkIfAbsent атомарно резервирует связь; возвращает false, если комментарий Bitrix24 или ServiceDesk уже связан.
	InsertCommentLinkIfAbsent(ctx context.Context, link *CommentLink) (bool, error)
	DeleteCommentLinkByB24ID(ctx context.Context, b24CommentID int64) error
	// MaxCommentLinkB24ID возвращает наибольший ID комментария Bitrix24 среди связанных (0, если связей нет).
	MaxCommentLinkB24ID(ctx context.Context) (int64, error)

	// ClaimCommentSend резервирует исходящую отправку комментария. Возвращает true только если вызывающий вправе обращаться к Bitrix24;
	// иначе возвращается актуальное состояние, повторять отправку нельзя.
	ClaimCommentSend(ctx context.Context, state *CommentSendState, maxAttempts int) (*CommentSendState, bool, error)
	GetCommentSendState(ctx context.Context, etalonCommentID string) (*CommentSendState, error)
	// MarkCommentSendAmbiguous и MarkCommentSendRejected переводят состояние только из sending.
	MarkCommentSendAmbiguous(ctx context.Context, etalonCommentID string, errText string) error
	MarkCommentSendRejected(ctx context.Context, etalonCommentID string, errText string) error
	// MarkCommentSendNeedsReconcile переводит устаревшее sending в ambiguous и возвращает false, если состояние уже изменилось.
	MarkCommentSendNeedsReconcile(ctx context.Context, etalonCommentID string) (bool, error)
	// ResolveCommentSendAbsent переводит ambiguous/sending в rejected после подтверждённого отсутствия комментария в Bitrix24.
	ResolveCommentSendAbsent(ctx context.Context, etalonCommentID string, errText string) (bool, error)
	// ConfirmCommentSend в одной транзакции записывает связь и подтверждает отправку. Повторное подтверждение тем же ID безопасно;
	// возвращает false, если комментарий уже подтверждён другим идентификатором Bitrix24.
	ConfirmCommentSend(ctx context.Context, etalonCommentID string, b24CommentID int64, finalizePending bool) (bool, error)
	SetCommentSendFinalizePending(ctx context.Context, etalonCommentID string, pending bool) error
	UpdateCommentSendFingerprint(ctx context.Context, etalonCommentID string, fingerprint string) error
	ListOpenCommentSendsByTicket(ctx context.Context, ticketID string) ([]CommentSendState, error)
	// ListCommentSendsForReconcile возвращает неподтверждённые отправки, не менявшиеся с openBefore, и подтверждённые с недооформленным превью.
	ListCommentSendsForReconcile(ctx context.Context, openBefore time.Time, finalizeBefore time.Time, limit int) ([]CommentSendState, error)

	UpsertUserMap(ctx context.Context, item *UserMap) error
	GetUserMapByEtalonID(ctx context.Context, etalonUserID uint) (*UserMap, error)
	GetUserMapByB24ID(ctx context.Context, b24UserID int64) (*UserMap, error)

	ReplaceServicePoints(ctx context.Context, points []ServicePoint) error
	ListServicePoints(ctx context.Context) ([]ServicePoint, error)
	SearchServicePoints(ctx context.Context, term string, limit, offset int, randomWhenEmpty bool) ([]ServicePoint, error)
	UpdateServicePointOneCData(ctx context.Context, b24ElementID int64, oneCCode string, contractOn *bool) error
	UpdateServicePointSyncData(ctx context.Context, point *ServicePoint) error

	ReplaceUserCache(ctx context.Context, users []UserCache) error
	ListUserCache(ctx context.Context) ([]UserCache, error)

	GetServicePointByID(ctx context.Context, b24ElementID int64) (*ServicePoint, error)
	ListServicePointsByIDs(ctx context.Context, ids []int64) ([]ServicePoint, error)
	DeleteServicePointsByIDs(ctx context.Context, ids []int64) error

	UpsertCompanyServicePointMapping(ctx context.Context, item *CompanyServicePointMapping) error
	ListCompanyServicePointMappings(ctx context.Context) ([]CompanyServicePointMapping, error)
	GetCompanyServicePointMappingByCompanyID(ctx context.Context, companyID string) (*CompanyServicePointMapping, error)
	GetCompanyServicePointMappingByPointID(ctx context.Context, bitrixServicePointID int64) (*CompanyServicePointMapping, error)
	ListCompanyServicePointMappingsByCompanyIDs(ctx context.Context, companyIDs []string) ([]CompanyServicePointMapping, error)
	ListCompanyServicePointMappingsByPointIDs(ctx context.Context, bitrixServicePointIDs []int64) ([]CompanyServicePointMapping, error)
	DeleteCompanyServicePointMappingByCompanyID(ctx context.Context, companyID string) error
	DeleteCompanyServicePointMappingByPointID(ctx context.Context, bitrixServicePointID int64) error

	InsertIfNotExistsByHash(ctx context.Context, event *IncomingEvent) (bool, error)
	MarkQueued(ctx context.Context, id string) error
	MarkProcessing(ctx context.Context, id string) error
	MarkDone(ctx context.Context, id string) error
	MarkFailed(ctx context.Context, id string, errText string) error
	MarkIgnored(ctx context.Context, id string, reason string) error
	ListNewOrFailedForEnqueue(ctx context.Context, limit int, maxAttempts int) ([]IncomingEvent, error)
	GetByID(ctx context.Context, id string) (*IncomingEvent, error)
}
