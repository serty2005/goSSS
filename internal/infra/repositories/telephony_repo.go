package repositories

import (
	"context"
	"etalon-server/internal/domain/telephony"
	infraDB "etalon-server/internal/infra/db"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type telephonyRepo struct {
	db *gorm.DB
}

func NewTelephonyRepo(db *gorm.DB) telephony.Repository {
	return &telephonyRepo{db: db}
}

func (r *telephonyRepo) getDB(ctx context.Context) *gorm.DB {
	return infraDB.ExtractDB(ctx, r.db)
}

func (r *telephonyRepo) ReplaceProviderEmployees(ctx context.Context, provider string, items []telephony.ProviderEmployee) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return gorm.ErrInvalidData
	}

	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(items) == 0 {
			return tx.Where("provider = ?", provider).Delete(&telephony.ProviderEmployee{}).Error
		}

		logins := make([]string, 0, len(items))
		for i := range items {
			items[i].Provider = provider
			items[i].EmployeeLogin = strings.TrimSpace(items[i].EmployeeLogin)
			logins = append(logins, items[i].EmployeeLogin)
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "provider"},
				{Name: "employee_login"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"employee_name",
				"ext",
				"telnum",
				"status",
				"raw_json",
				"last_seen_at",
				"updated_at",
			}),
		}).Create(&items).Error; err != nil {
			return err
		}

		uniqueLogins := make([]string, 0, len(logins))
		seenLogins := make(map[string]struct{}, len(logins))
		for _, login := range logins {
			if _, exists := seenLogins[login]; exists {
				continue
			}
			seenLogins[login] = struct{}{}
			uniqueLogins = append(uniqueLogins, login)
		}
		return tx.Where("provider = ? AND employee_login NOT IN ?", provider, uniqueLogins).
			Delete(&telephony.ProviderEmployee{}).Error
	})
}

func (r *telephonyRepo) ListProviderEmployees(ctx context.Context, provider string) ([]telephony.ProviderEmployee, error) {
	items := make([]telephony.ProviderEmployee, 0)
	err := r.getDB(ctx).WithContext(ctx).
		Where("provider = ?", strings.TrimSpace(provider)).
		Order("employee_name asc, employee_login asc").
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) GetProviderEmployee(ctx context.Context, provider string, login string) (*telephony.ProviderEmployee, error) {
	var item telephony.ProviderEmployee
	err := r.getDB(ctx).WithContext(ctx).
		Where("provider = ? AND employee_login = ?", strings.TrimSpace(provider), strings.TrimSpace(login)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) GetCallByExternalID(ctx context.Context, provider string, externalCallID string) (*telephony.Call, error) {
	var item telephony.Call
	err := r.getDB(ctx).WithContext(ctx).
		Where("provider = ? AND external_call_id = ?", strings.TrimSpace(provider), strings.TrimSpace(externalCallID)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) GetCallByAnyExternalID(ctx context.Context, provider string, externalCallID string) (*telephony.Call, error) {
	normalizedProvider := strings.TrimSpace(provider)
	normalizedCallID := strings.TrimSpace(externalCallID)
	if normalizedProvider == "" || normalizedCallID == "" {
		return nil, nil
	}

	item, err := r.GetCallByExternalID(ctx, normalizedProvider, normalizedCallID)
	if err != nil || item != nil {
		return item, err
	}

	var callEvent telephony.CallEvent
	err = r.getDB(ctx).WithContext(ctx).
		Table("telephony_call_events").
		Select("telephony_call_events.*").
		Joins("JOIN telephony_calls ON telephony_calls.id = telephony_call_events.telephony_call_id").
		Where(
			"telephony_call_events.external_call_id = ? OR telephony_call_events.second_call_id = ?",
			normalizedCallID,
			normalizedCallID,
		).
		Where("telephony_calls.provider = ?", normalizedProvider).
		Order("telephony_call_events.id desc").
		First(&callEvent).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var call telephony.Call
	err = r.getDB(ctx).WithContext(ctx).
		Where("id = ? AND provider = ?", callEvent.TelephonyCallID, normalizedProvider).
		First(&call).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &call, nil
}

func (r *telephonyRepo) UpsertCall(ctx context.Context, call *telephony.Call) error {
	if call == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "provider"},
			{Name: "external_call_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"direction",
			"status",
			"missed_status",
			"client_phone",
			"vat_number",
			"employee_login",
			"employee_user_id",
			"group_name",
			"started_at",
			"answered_at",
			"completed_at",
			"wait_seconds",
			"duration_seconds",
			"recording_url",
			"has_recording",
			"last_event_type",
			"raw_snapshot",
			"updated_at",
		}),
	}).Create(call).Error
}

func (r *telephonyRepo) AddCallEvent(ctx context.Context, event *telephony.CallEvent) error {
	if event == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "incoming_payload_hash"}},
		DoNothing: true,
	}).Create(event).Error
}

func (r *telephonyRepo) MergeCalls(ctx context.Context, target *telephony.Call, sourceCallID string) error {
	if target == nil {
		return gorm.ErrInvalidData
	}
	sourceCallID = strings.TrimSpace(sourceCallID)
	if sourceCallID == "" || sourceCallID == target.ID {
		return r.UpsertCall(ctx, target)
	}

	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "provider"},
				{Name: "external_call_id"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"direction",
				"status",
				"missed_status",
				"client_phone",
				"vat_number",
				"employee_login",
				"employee_user_id",
				"group_name",
				"started_at",
				"answered_at",
				"completed_at",
				"wait_seconds",
				"duration_seconds",
				"recording_url",
				"has_recording",
				"last_event_type",
				"raw_snapshot",
				"updated_at",
			}),
		}).Create(target).Error; err != nil {
			return err
		}
		if err := tx.Model(&telephony.CallEvent{}).
			Where("telephony_call_id = ?", sourceCallID).
			Update("telephony_call_id", target.ID).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", sourceCallID).Delete(&telephony.Call{}).Error
	})
}

func (r *telephonyRepo) InsertIncomingEventIfNotExists(ctx context.Context, event *telephony.IncomingEvent) (bool, error) {
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

func (r *telephonyRepo) ResetIncomingEventForReplay(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       telephony.IncomingEventStatusNew,
			"attempts":     0,
			"last_error":   nil,
			"processed_at": nil,
			"updated_at":   time.Now(),
		}).Error
}

func (r *telephonyRepo) MarkIncomingQueued(ctx context.Context, id string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     telephony.IncomingEventStatusQueued,
			"updated_at": time.Now(),
		}).Error
}

func (r *telephonyRepo) TryMarkIncomingProcessing(ctx context.Context, id string) (bool, error) {
	tx := r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where(
			"id = ? AND status IN ?",
			strings.TrimSpace(id),
			[]string{
				telephony.IncomingEventStatusNew,
				telephony.IncomingEventStatusQueued,
				telephony.IncomingEventStatusFailed,
			},
		).
		Updates(map[string]any{
			"status":     telephony.IncomingEventStatusProcessing,
			"attempts":   gorm.Expr("attempts + 1"),
			"last_error": nil,
			"updated_at": time.Now(),
		})
	if tx.Error != nil {
		return false, tx.Error
	}
	return tx.RowsAffected > 0, nil
}

func (r *telephonyRepo) MarkIncomingDone(ctx context.Context, id string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       telephony.IncomingEventStatusDone,
			"last_error":   nil,
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *telephonyRepo) MarkIncomingFailed(ctx context.Context, id string, errText string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":     telephony.IncomingEventStatusFailed,
			"last_error": strings.TrimSpace(errText),
			"updated_at": time.Now(),
		}).Error
}

func (r *telephonyRepo) MarkIncomingIgnored(ctx context.Context, id string, reason string) error {
	now := time.Now()
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":       telephony.IncomingEventStatusIgnored,
			"last_error":   strings.TrimSpace(reason),
			"processed_at": &now,
			"updated_at":   now,
		}).Error
}

func (r *telephonyRepo) ListIncomingNewOrFailedForEnqueue(ctx context.Context, limit int, maxAttempts int) ([]telephony.IncomingEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if maxAttempts <= 0 {
		maxAttempts = 10
	}
	items := make([]telephony.IncomingEvent, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Where(
			"(status = ?) OR (status = ? AND attempts < ?)",
			telephony.IncomingEventStatusNew,
			telephony.IncomingEventStatusFailed,
			maxAttempts,
		).
		Order("received_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) ListIncomingQueuedForRecovery(ctx context.Context, limit int, queuedBefore time.Time) ([]telephony.IncomingEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	items := make([]telephony.IncomingEvent, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Where("status = ? AND updated_at <= ?", telephony.IncomingEventStatusQueued, queuedBefore).
		Order("received_at asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) ListIncomingEvents(ctx context.Context, filter telephony.IncomingEventListFilter) ([]telephony.IncomingEvent, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	query := r.getDB(ctx).WithContext(ctx).Model(&telephony.IncomingEvent{})
	if len(filter.Cmd) > 0 {
		query = query.Where("cmd IN ?", filter.Cmd)
	}
	if len(filter.Status) > 0 {
		query = query.Where("status IN ?", filter.Status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]telephony.IncomingEvent, 0, filter.Limit)
	err := query.Order("received_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&items).Error
	return items, total, err
}

func (r *telephonyRepo) GetIncomingEventByID(ctx context.Context, id string) (*telephony.IncomingEvent, error) {
	var item telephony.IncomingEvent
	err := r.getDB(ctx).WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}
