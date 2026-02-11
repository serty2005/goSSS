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

type UserMap struct {
	EtalonUserID uint      `json:"etalon_user_id" gorm:"primaryKey"`
	B24UserID    int64     `json:"b24_user_id" gorm:"uniqueIndex;not null"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (UserMap) TableName() string { return "user_map" }

type ServicePoint struct {
	B24ElementID int64     `json:"b24_element_id" gorm:"primaryKey"`
	Name         string    `json:"name" gorm:"type:text;not null;index"`
	Address      string    `json:"address" gorm:"type:text"`
	RawJSON      string    `json:"raw_json" gorm:"type:text"`
	UpdatedAt    time.Time `json:"updated_at"`
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
