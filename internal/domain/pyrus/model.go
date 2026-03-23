package pyrus

import "time"

type TicketLink struct {
	TicketID        string     `json:"ticket_id" gorm:"primaryKey;type:text"`
	PyrusTaskID     int64      `json:"pyrus_task_id" gorm:"uniqueIndex;not null"`
	PyrusFormID     int64      `json:"pyrus_form_id"`
	LastIncomingAt  *time.Time `json:"last_incoming_at"`
	LastOutgoingAt  *time.Time `json:"last_outgoing_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (TicketLink) TableName() string { return "pyrus_ticket_links" }

type CommentLink struct {
	EtalonCommentID string    `json:"etalon_comment_id" gorm:"primaryKey;type:text"`
	PyrusCommentID  *int64    `json:"pyrus_comment_id" gorm:"uniqueIndex"`
	PyrusTaskID     int64     `json:"pyrus_task_id" gorm:"index;not null"`
	Direction       string    `json:"direction" gorm:"type:varchar(32);not null;default:'pyrus_to_local'"`
	Fingerprint     string    `json:"fingerprint" gorm:"type:char(64);index"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (CommentLink) TableName() string { return "pyrus_comment_links" }

type FileLink struct {
	LocalFileID        string     `json:"local_file_id" gorm:"primaryKey;type:text"`
	PyrusGUID          *string    `json:"pyrus_guid" gorm:"type:text;index"`
	PyrusAttachmentID  *int64     `json:"pyrus_attachment_id" gorm:"index"`
	TicketID           string     `json:"ticket_id" gorm:"type:text;index;not null"`
	CommentID          *string    `json:"comment_id" gorm:"type:text;index"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (FileLink) TableName() string { return "pyrus_file_links" }

type UserMap struct {
	EtalonUserID uint      `json:"etalon_user_id" gorm:"primaryKey"`
	PyrusUserID  int64     `json:"pyrus_user_id" gorm:"uniqueIndex;not null"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (UserMap) TableName() string { return "pyrus_user_maps" }

const (
	IncomingEventStatusNew        = "new"
	IncomingEventStatusQueued     = "queued"
	IncomingEventStatusProcessing = "processing"
	IncomingEventStatusDone       = "done"
	IncomingEventStatusFailed     = "failed"
	IncomingEventStatusIgnored    = "ignored"
)

type IncomingEvent struct {
	ID          string     `json:"id" gorm:"primaryKey;type:uuid"`
	EventName   string     `json:"event_name" gorm:"type:text;not null;index:idx_pyrus_incoming_lookup"`
	PyrusTaskID *int64     `json:"pyrus_task_id" gorm:"index:idx_pyrus_incoming_lookup"`
	PayloadHash string     `json:"payload_hash" gorm:"type:char(64);uniqueIndex;not null"`
	PayloadRaw  string     `json:"payload_raw" gorm:"type:text;not null"`
	Status      string     `json:"status" gorm:"type:varchar(20);not null;default:'new';index:idx_pyrus_incoming_status_received"`
	Attempts    int        `json:"attempts" gorm:"not null;default:0"`
	LastError   *string    `json:"last_error" gorm:"type:text"`
	ReceivedAt  time.Time  `json:"received_at" gorm:"not null;autoCreateTime;index:idx_pyrus_incoming_status_received"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (IncomingEvent) TableName() string { return "pyrus_incoming_events" }

const (
	OutgoingEventStatusNew        = "new"
	OutgoingEventStatusProcessing = "processing"
	OutgoingEventStatusDone       = "done"
	OutgoingEventStatusFailed     = "failed"
	OutgoingEventStatusIgnored    = "ignored"
)

type OutgoingEvent struct {
	ID          string     `json:"id" gorm:"primaryKey;type:uuid"`
	EventName   string     `json:"event_name" gorm:"type:text;not null;index:idx_pyrus_outgoing_lookup"`
	TicketID    *string    `json:"ticket_id" gorm:"type:text;index:idx_pyrus_outgoing_lookup"`
	PyrusTaskID *int64     `json:"pyrus_task_id" gorm:"index:idx_pyrus_outgoing_lookup"`
	PayloadJSON string     `json:"payload_json" gorm:"type:text;not null"`
	Status      string     `json:"status" gorm:"type:varchar(20);not null;default:'new';index:idx_pyrus_outgoing_status_queued"`
	Attempts    int        `json:"attempts" gorm:"not null;default:0"`
	LastError   *string    `json:"last_error" gorm:"type:text"`
	QueuedAt    time.Time  `json:"queued_at" gorm:"not null;autoCreateTime;index:idx_pyrus_outgoing_status_queued"`
	ProcessedAt *time.Time `json:"processed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (OutgoingEvent) TableName() string { return "pyrus_outgoing_events" }
