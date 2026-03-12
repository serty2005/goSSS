package bitrix

import "time"

type DealLink struct {
	TicketID   string    `json:"ticket_id" gorm:"primaryKey;type:text"`
	B24DealID  int64     `json:"b24_deal_id" gorm:"uniqueIndex;not null"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastSyncAt time.Time `json:"last_sync_at" gorm:"index"`
}

func (DealLink) TableName() string { return "deal_link" }

type CommentLink struct {
	EtalonCommentID string    `json:"etalon_comment_id" gorm:"primaryKey;type:text"`
	B24CommentID    int64     `json:"b24_comment_id" gorm:"uniqueIndex;not null"`
	TicketID        string    `json:"ticket_id" gorm:"type:text;index;not null"`
	Direction       string    `json:"direction" gorm:"type:varchar(20);not null;default:'etalon_to_b24'"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (CommentLink) TableName() string { return "comment_link" }

type IgnoredDeal struct {
	B24DealID int64     `json:"b24_deal_id" gorm:"primaryKey"`
	TicketID  string    `json:"ticket_id" gorm:"type:text;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (IgnoredDeal) TableName() string { return "bitrix_ignored_deals" }

type UserMap struct {
	EtalonUserID uint      `json:"etalon_user_id" gorm:"primaryKey"`
	B24UserID    int64     `json:"b24_user_id" gorm:"uniqueIndex;not null"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (UserMap) TableName() string { return "user_map" }

type ServicePoint struct {
	B24ElementID  int64      `json:"b24_element_id" gorm:"primaryKey"`
	Name          string     `json:"name" gorm:"type:text;not null;index"`
	Address       string     `json:"address" gorm:"type:text"`
	OneCCode      *string    `json:"one_c_code,omitempty" gorm:"type:varchar(128);index"`
	ContractOn    *bool      `json:"contract_on,omitempty" gorm:"column:one_c_contract_on"`
	ContractType  *string    `json:"contract_type,omitempty" gorm:"type:varchar(64);index"`
	ContractStart *time.Time `json:"contract_start,omitempty"`
	ContractEnd   *time.Time `json:"contract_end,omitempty"`
	ClientOrder   *string    `json:"client_order,omitempty" gorm:"type:text"`
	RawJSON       string     `json:"raw_json" gorm:"type:text"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (ServicePoint) TableName() string { return "bitrix_service_points" }

type UserCache struct {
	B24UserID  int64      `json:"b24_user_id" gorm:"primaryKey"`
	Name       string     `json:"name" gorm:"type:text"`
	Active     bool       `json:"active" gorm:"index"`
	LastName   string     `json:"last_name" gorm:"type:varchar(255)"`
	FirstName  string     `json:"first_name" gorm:"type:varchar(255)"`
	SecondName string     `json:"second_name" gorm:"type:varchar(255)"`
	Email      string     `json:"email" gorm:"type:varchar(255)"`
	Phone      string     `json:"phone" gorm:"type:varchar(100)"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserCache) TableName() string { return "bitrix_users_cache" }

type CompanyServicePointMapping struct {
	CompanyID            string    `json:"company_id" gorm:"primaryKey;type:text"`
	BitrixServicePointID int64     `json:"bitrix_service_point_id" gorm:"uniqueIndex;not null;index"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (CompanyServicePointMapping) TableName() string {
	return "bitrix_company_service_point_mappings"
}

const (
	IncomingEventStatusNew        = "new"
	IncomingEventStatusQueued     = "queued"
	IncomingEventStatusProcessing = "processing"
	IncomingEventStatusDone       = "done"
	IncomingEventStatusFailed     = "failed"
	IncomingEventStatusIgnored    = "ignored"
)

type IncomingEvent struct {
	ID             string     `json:"id" gorm:"primaryKey;type:uuid"`
	EventName      string     `json:"event_name" gorm:"type:text;not null;index:idx_b24_incoming_event_lookup"`
	EntityID       *string    `json:"entity_id" gorm:"type:text;index:idx_b24_incoming_event_lookup"`
	EventTS        *int64     `json:"event_ts" gorm:"index:idx_b24_incoming_event_lookup"`
	EventHandlerID *int64     `json:"event_handler_id"`
	PayloadRaw     string     `json:"payload_raw" gorm:"type:text;not null"`
	PayloadHash    string     `json:"payload_hash" gorm:"type:char(64);uniqueIndex;not null"`
	Status         string     `json:"status" gorm:"type:varchar(20);not null;default:'new';index:idx_b24_incoming_status_received"`
	Attempts       int        `json:"attempts" gorm:"not null;default:0"`
	LastError      *string    `json:"last_error" gorm:"type:text"`
	ReceivedAt     time.Time  `json:"received_at" gorm:"not null;autoCreateTime;index:idx_b24_incoming_status_received"`
	ProcessedAt    *time.Time `json:"processed_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (IncomingEvent) TableName() string { return "bitrix_incoming_events" }
