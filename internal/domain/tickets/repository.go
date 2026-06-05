package tickets

import (
	"context"
	"time"
)

type ResolvedByAssigneeStat struct {
	UserID      uint   `json:"user_id"`
	UserName    string `json:"user_name"`
	TodayCount  int64  `json:"today_count"`
	Days7Count  int64  `json:"days_7_count" gorm:"column:days_7_count"`
	Days30Count int64  `json:"days_30_count" gorm:"column:days_30_count"`
}

type AcceptedCallsByEmployeeStat struct {
	UserID   uint   `json:"user_id"`
	UserName string `json:"user_name"`
	Count    int64  `json:"count"`
}

type ServerStatusStat struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type DashboardStats struct {
	ResolvedByAssignee  []ResolvedByAssigneeStat      `json:"resolved_by_assignee"`
	AcceptedCallsByUser []AcceptedCallsByEmployeeStat `json:"accepted_calls_by_employee"`
	ServerStatuses      []ServerStatusStat            `json:"server_statuses"`
	TotalTickets        int64                         `json:"total_tickets"`
	PolledServers24h    int64                         `json:"polled_servers_24h"`
	AcceptedCalls24h    int64                         `json:"accepted_calls_24h"`
}

type TicketContactUpsertInput struct {
	TicketID           string
	ContactType        string
	TelephonyContactID *uint
	Value              string
	DisplayValue       string
	Name               string
	IsPrimary          bool
	Source             string
}

type TicketContactUpdateByIDInput struct {
	ContactType        string
	TelephonyContactID *uint
	Value              string
	DisplayValue       string
	Name               string
	IsPrimary          bool
	Source             string
}

// TicketFilter содержит параметры для поиска заявок.
type TicketFilter struct {
	CompanyID       string
	CompanyIDs      []string
	ContactID       *uint
	AssetID         *string
	Statuses        []string
	ExcludeStatuses []string
	ArchiveMode     string
	CreatedFrom     *time.Time
	CreatedTo       *time.Time
	ResolvedFrom    *time.Time
	ResolvedTo      *time.Time
	UpdatedFrom     *time.Time
	UpdatedTo       *time.Time
	AssigneeIDs     []uint
	AssigneeID      *uint
	ReporterID      *uint
	SearchQuery     string
	Limit           int
	Offset          int
	SortBy          string
}

// CompanyFilterItem описывает агрегированные данные по компаниям для фильтра.
type CompanyFilterItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ParentName string `json:"parent_name,omitempty"`
	Count      int64  `json:"count"`
}

// TicketRepository определяет методы для работы с хранилищем заявок.
type TicketRepository interface {
	Create(ctx context.Context, ticket *Ticket) error
	Update(ctx context.Context, ticket *Ticket) error
	GetByID(ctx context.Context, id string) (*Ticket, error)
	GetByNumber(ctx context.Context, number int) (*Ticket, error)
	GetByServiceDeskUUID(ctx context.Context, sdUUID string) (*Ticket, error)
	Delete(ctx context.Context, ticketID string) error

	Find(ctx context.Context, filter TicketFilter) ([]Ticket, error)
	Count(ctx context.Context, filter TicketFilter) (int64, error)

	AddHistory(ctx context.Context, history *TicketHistory) error
	GetHistory(ctx context.Context, ticketID string) ([]TicketHistory, error)

	AddAttachment(ctx context.Context, attachment *Attachment) error
	GetAttachments(ctx context.Context, ticketID string) ([]Attachment, error)
	UpsertFileAsset(ctx context.Context, file *FileAsset) (*FileAsset, error)
	GetFileAssetByID(ctx context.Context, id string) (*FileAsset, error)
	GetFileAssetByStorageKey(ctx context.Context, storageKey string) (*FileAsset, error)
	UpsertTicketFileLink(ctx context.Context, link *TicketFileLink) error
	GetTicketFileLinksByRelation(ctx context.Context, ticketID string, relationTypes []string) ([]TicketFileLink, error)

	AddComments(ctx context.Context, comments []TicketComment) error
	GetComments(ctx context.Context, ticketID string) ([]TicketComment, error)
	GetCommentByUUID(ctx context.Context, ticketID string, commentUUID string) (*TicketComment, error)
	UpdateComment(ctx context.Context, ticketID string, commentUUID string, text string, replyToClient bool) (*TicketComment, error)
	SoftDeleteComment(ctx context.Context, ticketID string, commentUUID string, deletedAt time.Time) (*TicketComment, error)
	HardDeleteComment(ctx context.Context, ticketID string, commentUUID string) (*TicketComment, error)
	UpdateCommentFromBitrix(ctx context.Context, commentID string, text string, authorName string) error
	MarkCommentDeletedInBitrix(ctx context.Context, commentID string, deletedAt time.Time) error
	GetLastComments(ctx context.Context, ticketIDs []string) (map[string]LastCommentInfo, error)
	GetCompanyFilters(ctx context.Context, filter TicketFilter) ([]CompanyFilterItem, error)
	GetDashboardStats(ctx context.Context) (*DashboardStats, error)
	ListResolvedForAutoClose(ctx context.Context, threshold time.Duration) ([]Ticket, error)
	ListExpiredDeferred(ctx context.Context, now time.Time, limit int) ([]Ticket, error)
	ArchiveStale(ctx context.Context, threshold time.Duration) (int64, error)
	RebindBitrixServicePoint(ctx context.Context, fromID, toID int64) (int64, error)

	UpsertTicketContact(ctx context.Context, input TicketContactUpsertInput) (*TicketContact, error)
	UpdateTicketContact(ctx context.Context, ticketID string, contactID uint, input TicketContactUpdateByIDInput) (*TicketContact, error)
	ListTicketContacts(ctx context.Context, ticketID string) ([]TicketContact, error)
	DeleteTicketContact(ctx context.Context, ticketID string, contactID uint) error
	DeleteTicketContacts(ctx context.Context, ticketID string) error
	SetPrimaryTicketContact(ctx context.Context, ticketID string, contactID uint, manual bool) (*TicketContact, error)

	AssociateAsset(ctx context.Context, ticketID, assetID, assetType string) error
}
