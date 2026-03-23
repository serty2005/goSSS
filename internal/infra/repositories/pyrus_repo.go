package repositories

import (
	"context"
	"etalon-server/internal/domain/pyrus"
	infraDB "etalon-server/internal/infra/db"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type pyrusRepo struct {
	db *gorm.DB
}

func NewPyrusRepo(db *gorm.DB) pyrus.Repository {
	return &pyrusRepo{db: db}
}

func (r *pyrusRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *pyrusRepo) UpsertTicketLink(ctx context.Context, link *pyrus.TicketLink) error {
	if link == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ticket_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"pyrus_task_id", "pyrus_form_id", "last_incoming_at", "last_outgoing_at", "updated_at"}),
	}).Create(link).Error
}

func (r *pyrusRepo) GetTicketLinkByTicketID(ctx context.Context, ticketID string) (*pyrus.TicketLink, error) {
	var item pyrus.TicketLink
	err := r.getDB(ctx).WithContext(ctx).Where("ticket_id = ?", strings.TrimSpace(ticketID)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) GetTicketLinkByTaskID(ctx context.Context, taskID int64) (*pyrus.TicketLink, error) {
	var item pyrus.TicketLink
	err := r.getDB(ctx).WithContext(ctx).Where("pyrus_task_id = ?", taskID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) UpsertCommentLink(ctx context.Context, link *pyrus.CommentLink) error {
	if link == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "etalon_comment_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"pyrus_comment_id", "pyrus_task_id", "direction", "fingerprint", "updated_at"}),
	}).Create(link).Error
}

func (r *pyrusRepo) GetCommentLinkByEtalonID(ctx context.Context, etalonCommentID string) (*pyrus.CommentLink, error) {
	var item pyrus.CommentLink
	err := r.getDB(ctx).WithContext(ctx).Where("etalon_comment_id = ?", strings.TrimSpace(etalonCommentID)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) GetCommentLinkByPyrusCommentID(ctx context.Context, pyrusCommentID int64) (*pyrus.CommentLink, error) {
	var item pyrus.CommentLink
	err := r.getDB(ctx).WithContext(ctx).Where("pyrus_comment_id = ?", pyrusCommentID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) UpsertFileLink(ctx context.Context, link *pyrus.FileLink) error {
	if link == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "local_file_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"pyrus_guid", "pyrus_attachment_id", "ticket_id", "comment_id", "updated_at"}),
	}).Create(link).Error
}

func (r *pyrusRepo) GetFileLinkByLocalFileID(ctx context.Context, localFileID string) (*pyrus.FileLink, error) {
	var item pyrus.FileLink
	err := r.getDB(ctx).WithContext(ctx).Where("local_file_id = ?", strings.TrimSpace(localFileID)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) GetFileLinkByPyrusAttachmentID(ctx context.Context, pyrusAttachmentID int64) (*pyrus.FileLink, error) {
	var item pyrus.FileLink
	err := r.getDB(ctx).WithContext(ctx).Where("pyrus_attachment_id = ?", pyrusAttachmentID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) UpsertUserMap(ctx context.Context, item *pyrus.UserMap) error {
	if item == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "etalon_user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"pyrus_user_id", "updated_at"}),
	}).Create(item).Error
}

func (r *pyrusRepo) GetUserMapByEtalonID(ctx context.Context, etalonUserID uint) (*pyrus.UserMap, error) {
	var item pyrus.UserMap
	err := r.getDB(ctx).WithContext(ctx).Where("etalon_user_id = ?", etalonUserID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) GetUserMapByPyrusID(ctx context.Context, pyrusUserID int64) (*pyrus.UserMap, error) {
	var item pyrus.UserMap
	err := r.getDB(ctx).WithContext(ctx).Where("pyrus_user_id = ?", pyrusUserID).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) InsertIncomingEventIfNotExists(ctx context.Context, event *pyrus.IncomingEvent) (bool, error) {
	if event == nil {
		return false, gorm.ErrInvalidData
	}
	tx := r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "payload_hash"}},
		DoNothing: true,
	}).Create(event)
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

func (r *pyrusRepo) ResetIncomingEventForReplay(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       pyrus.IncomingEventStatusNew,
			"attempts":     0,
			"last_error":   nil,
			"processed_at": nil,
			"updated_at":   time.Now(),
		}).Error
}

func (r *pyrusRepo) MarkIncomingQueued(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{"status": pyrus.IncomingEventStatusQueued, "updated_at": time.Now()}).Error
}

func (r *pyrusRepo) MarkIncomingProcessing(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     pyrus.IncomingEventStatusProcessing,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": nil,
			"updated_at": time.Now(),
		}).Error
}

func (r *pyrusRepo) MarkIncomingDone(ctx context.Context, id string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       pyrus.IncomingEventStatusDone,
			"last_error":   nil,
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *pyrusRepo) MarkIncomingFailed(ctx context.Context, id string, errText string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     pyrus.IncomingEventStatusFailed,
			"last_error": strings.TrimSpace(errText),
			"updated_at": time.Now(),
		}).Error
}

func (r *pyrusRepo) MarkIncomingIgnored(ctx context.Context, id string, reason string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       pyrus.IncomingEventStatusIgnored,
			"last_error":   strings.TrimSpace(reason),
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *pyrusRepo) ListIncomingNewOrFailedForEnqueue(ctx context.Context, limit int, maxAttempts int) ([]pyrus.IncomingEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	items := make([]pyrus.IncomingEvent, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Where(
			"(status = ?) OR (status = ? AND attempts < ?)",
			pyrus.IncomingEventStatusNew,
			pyrus.IncomingEventStatusFailed,
			maxAttempts,
		).
		Order("received_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *pyrusRepo) ListIncomingEvents(ctx context.Context, filter pyrus.IncomingEventListFilter) ([]pyrus.IncomingEvent, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := r.getDB(ctx).WithContext(ctx).Model(&pyrus.IncomingEvent{})
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]pyrus.IncomingEvent, 0, filter.Limit)
	err := query.Order("received_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&items).Error
	return items, total, err
}

func (r *pyrusRepo) GetIncomingEventByID(ctx context.Context, id string) (*pyrus.IncomingEvent, error) {
	var item pyrus.IncomingEvent
	err := r.getDB(ctx).WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *pyrusRepo) InsertOutgoingEvent(ctx context.Context, event *pyrus.OutgoingEvent) error {
	if event == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Create(event).Error
}

func (r *pyrusRepo) MarkOutgoingProcessing(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.OutgoingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     pyrus.OutgoingEventStatusProcessing,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": nil,
			"updated_at": time.Now(),
		}).Error
}

func (r *pyrusRepo) MarkOutgoingDone(ctx context.Context, id string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.OutgoingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       pyrus.OutgoingEventStatusDone,
			"last_error":   nil,
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *pyrusRepo) MarkOutgoingFailed(ctx context.Context, id string, errText string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.OutgoingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     pyrus.OutgoingEventStatusFailed,
			"last_error": strings.TrimSpace(errText),
			"updated_at": time.Now(),
		}).Error
}

func (r *pyrusRepo) MarkOutgoingIgnored(ctx context.Context, id string, reason string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&pyrus.OutgoingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       pyrus.OutgoingEventStatusIgnored,
			"last_error":   strings.TrimSpace(reason),
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *pyrusRepo) ListOutgoingEventsForRetry(ctx context.Context, limit int, maxAttempts int) ([]pyrus.OutgoingEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	items := make([]pyrus.OutgoingEvent, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Where(
			"(status = ?) OR (status = ? AND attempts < ?)",
			pyrus.OutgoingEventStatusNew,
			pyrus.OutgoingEventStatusFailed,
			maxAttempts,
		).
		Order("queued_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *pyrusRepo) ListOutgoingEvents(ctx context.Context, filter pyrus.OutgoingEventListFilter) ([]pyrus.OutgoingEvent, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := r.getDB(ctx).WithContext(ctx).Model(&pyrus.OutgoingEvent{})
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]pyrus.OutgoingEvent, 0, filter.Limit)
	err := query.Order("queued_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&items).Error
	return items, total, err
}

func (r *pyrusRepo) GetOutgoingEventByID(ctx context.Context, id string) (*pyrus.OutgoingEvent, error) {
	var item pyrus.OutgoingEvent
	err := r.getDB(ctx).WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}
