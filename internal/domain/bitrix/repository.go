package bitrix

import "context"

type Repository interface {
	UpsertDealLink(ctx context.Context, link *DealLink) error
	GetDealLinkByTicketID(ctx context.Context, ticketID string) (*DealLink, error)
	GetDealLinkByDealID(ctx context.Context, dealID int64) (*DealLink, error)
	DeleteDealLinkByTicketID(ctx context.Context, ticketID string) error

	UpsertCommentLink(ctx context.Context, link *CommentLink) error
	GetCommentLinkByEtalonID(ctx context.Context, etalonCommentID string) (*CommentLink, error)
	GetCommentLinkByB24ID(ctx context.Context, b24CommentID int64) (*CommentLink, error)

	UpsertUserMap(ctx context.Context, item *UserMap) error
	GetUserMapByEtalonID(ctx context.Context, etalonUserID uint) (*UserMap, error)
	GetUserMapByB24ID(ctx context.Context, b24UserID int64) (*UserMap, error)

	ReplaceServicePoints(ctx context.Context, points []ServicePoint) error
	ListServicePoints(ctx context.Context) ([]ServicePoint, error)
	UpdateServicePointOneCData(ctx context.Context, b24ElementID int64, oneCCode string, contractOn *bool) error

	ReplaceUserCache(ctx context.Context, users []UserCache) error
	ListUserCache(ctx context.Context) ([]UserCache, error)
}
