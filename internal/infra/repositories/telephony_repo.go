package repositories

import (
	"context"
	"etalon-server/internal/domain/telephony"
	infraDB "etalon-server/internal/infra/db"
	"sort"
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

func (r *telephonyRepo) UpdateProviderEmployeeStatus(ctx context.Context, provider string, login string, status string, seenAt time.Time) error {
	provider = strings.TrimSpace(provider)
	login = strings.TrimSpace(login)
	status = strings.TrimSpace(status)
	if provider == "" || login == "" || status == "" {
		return nil
	}
	if seenAt.IsZero() {
		seenAt = time.Now()
	}
	return r.getDB(ctx).WithContext(ctx).
		Model(&telephony.ProviderEmployee{}).
		Where("provider = ? AND employee_login = ?", provider, login).
		Updates(map[string]any{
			"status":       status,
			"last_seen_at": seenAt,
			"updated_at":   time.Now(),
		}).Error
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

func (r *telephonyRepo) GetCallByID(ctx context.Context, id string) (*telephony.Call, error) {
	var item telephony.Call
	err := r.getDB(ctx).WithContext(ctx).
		Where("id = ?", strings.TrimSpace(id)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
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

func (r *telephonyRepo) SyncCallEmployeeUser(ctx context.Context, provider string, login string, userID *uint) error {
	provider = strings.TrimSpace(provider)
	login = strings.TrimSpace(login)
	if provider == "" || login == "" {
		return nil
	}

	return r.getDB(ctx).WithContext(ctx).
		Model(&telephony.Call{}).
		Where("provider = ? AND employee_login = ?", provider, login).
		Updates(map[string]any{
			"employee_user_id": userID,
			"updated_at":       time.Now(),
		}).Error
}

func (r *telephonyRepo) IsCallHistoryRangeCovered(
	ctx context.Context,
	provider string,
	employeeLogin *string,
	startedFrom time.Time,
	startedTo time.Time,
) (bool, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return false, nil
	}
	if startedTo.Before(startedFrom) {
		startedFrom, startedTo = startedTo, startedFrom
	}

	windows := make([]telephony.CallHistorySyncWindow, 0)
	query := r.getDB(ctx).WithContext(ctx).
		Model(&telephony.CallHistorySyncWindow{}).
		Where("provider = ?", provider).
		Where("started_from <= ? AND started_to >= ?", startedTo, startedFrom).
		Order("started_from asc, started_to asc")
	query = applyTelephonyHistoryCoverageScope(query, employeeLogin)
	if err := query.Find(&windows).Error; err != nil {
		return false, err
	}
	return isTelephonyHistoryRangeCovered(windows, startedFrom, startedTo), nil
}

func (r *telephonyRepo) MarkCallHistoryRangeCovered(
	ctx context.Context,
	provider string,
	employeeLogin *string,
	startedFrom time.Time,
	startedTo time.Time,
	syncedAt time.Time,
) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return gorm.ErrInvalidData
	}
	if startedTo.Before(startedFrom) {
		startedFrom, startedTo = startedTo, startedFrom
	}
	if syncedAt.IsZero() {
		syncedAt = time.Now()
	}

	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		mergedFrom := startedFrom
		mergedTo := startedTo
		windows := make([]telephony.CallHistorySyncWindow, 0)
		query := tx.Model(&telephony.CallHistorySyncWindow{}).
			Where("provider = ?", provider).
			Where(
				"started_from <= ? AND started_to >= ?",
				startedTo.Add(time.Second),
				startedFrom.Add(-time.Second),
			).
			Order("started_from asc, started_to asc")
		query = applyTelephonyHistoryExactScope(query, employeeLogin)
		if err := query.Find(&windows).Error; err != nil {
			return err
		}

		ids := make([]uint, 0, len(windows))
		for i := range windows {
			if windows[i].StartedFrom.Before(mergedFrom) {
				mergedFrom = windows[i].StartedFrom
			}
			if windows[i].StartedTo.After(mergedTo) {
				mergedTo = windows[i].StartedTo
			}
			ids = append(ids, windows[i].ID)
		}
		if len(ids) > 0 {
			if err := tx.Where("id IN ?", ids).Delete(&telephony.CallHistorySyncWindow{}).Error; err != nil {
				return err
			}
		}

		window := telephony.CallHistorySyncWindow{
			Provider:    provider,
			StartedFrom: mergedFrom,
			StartedTo:   mergedTo,
			SyncedAt:    syncedAt,
		}
		if login := normalizeTelephonyHistoryEmployeeLogin(employeeLogin); login != "" {
			window.EmployeeLogin = &login
		}
		return tx.Create(&window).Error
	})
}

func (r *telephonyRepo) ListCalls(ctx context.Context, filter telephony.CallListFilter) ([]telephony.Call, int64, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}

	baseQuery := r.applyCallListFilter(r.getDB(ctx).WithContext(ctx).Model(&telephony.Call{}), filter)

	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]telephony.Call, 0, filter.Limit)
	err := r.applyCallListFilter(r.getDB(ctx).WithContext(ctx).Model(&telephony.Call{}), filter).
		Order("COALESCE(telephony_calls.started_at, telephony_calls.created_at) desc, telephony_calls.created_at desc").
		Limit(filter.Limit).
		Offset(filter.Offset).
		Find(&items).Error
	return items, total, err
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

func (r *telephonyRepo) GetCallArtifact(ctx context.Context, callID string, artifactType string) (*telephony.CallArtifact, error) {
	var item telephony.CallArtifact
	err := r.getDB(ctx).WithContext(ctx).
		Where(
			"telephony_call_id = ? AND artifact_type = ?",
			strings.TrimSpace(callID),
			strings.TrimSpace(artifactType),
		).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) UpsertCallArtifact(ctx context.Context, artifact *telephony.CallArtifact) error {
	if artifact == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "telephony_call_id"},
			{Name: "artifact_type"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"url",
			"storage_key",
			"mime_type",
			"updated_at",
		}),
	}).Create(artifact).Error
}

func (r *telephonyRepo) DeleteCallArtifact(ctx context.Context, callID string, artifactType string) error {
	return r.getDB(ctx).WithContext(ctx).
		Where(
			"telephony_call_id = ? AND artifact_type = ?",
			strings.TrimSpace(callID),
			strings.TrimSpace(artifactType),
		).
		Delete(&telephony.CallArtifact{}).Error
}

func (r *telephonyRepo) ListExpiredCallArtifacts(
	ctx context.Context,
	artifactType string,
	olderThan time.Time,
	limit int,
) ([]telephony.CallArtifact, error) {
	if limit <= 0 {
		limit = 100
	}
	items := make([]telephony.CallArtifact, 0, limit)
	err := r.getDB(ctx).WithContext(ctx).
		Table("telephony_call_artifacts").
		Select("telephony_call_artifacts.*").
		Joins("JOIN telephony_calls ON telephony_calls.id = telephony_call_artifacts.telephony_call_id").
		Where("telephony_call_artifacts.artifact_type = ?", strings.TrimSpace(artifactType)).
		Where("COALESCE(telephony_calls.completed_at, telephony_calls.started_at, telephony_calls.created_at) < ?", olderThan).
		Order("COALESCE(telephony_calls.completed_at, telephony_calls.started_at, telephony_calls.created_at) asc, telephony_call_artifacts.id asc").
		Limit(limit).
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) ClearCallRecording(ctx context.Context, callID string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.Call{}).
		Where("id = ?", strings.TrimSpace(callID)).
		Updates(map[string]any{
			"recording_url": nil,
			"has_recording": false,
			"updated_at":    time.Now(),
		}).Error
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
		sourceArtifacts := make([]telephony.CallArtifact, 0)
		if err := tx.Where("telephony_call_id = ?", sourceCallID).Find(&sourceArtifacts).Error; err != nil {
			return err
		}
		for i := range sourceArtifacts {
			sourceArtifacts[i].TelephonyCallID = target.ID
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{
					{Name: "telephony_call_id"},
					{Name: "artifact_type"},
				},
				DoUpdates: clause.AssignmentColumns([]string{
					"url",
					"storage_key",
					"mime_type",
					"updated_at",
				}),
			}).Create(&sourceArtifacts[i]).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("telephony_call_id = ?", sourceCallID).Delete(&telephony.CallArtifact{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", sourceCallID).Delete(&telephony.Call{}).Error
	})
}

func (r *telephonyRepo) UpsertCallTicketLink(ctx context.Context, link *telephony.CallTicketLink) error {
	if link == nil {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("telephony_call_id = ?", link.TelephonyCallID).Delete(&telephony.CallTicketLink{}).Error; err != nil {
			return err
		}
		return tx.Create(link).Error
	})
}

func (r *telephonyRepo) GetCallTicketLink(ctx context.Context, callID string) (*telephony.CallTicketLink, error) {
	var item telephony.CallTicketLink
	err := r.getDB(ctx).WithContext(ctx).
		Where("telephony_call_id = ?", strings.TrimSpace(callID)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) ListCallTicketLinks(ctx context.Context, callIDs []string) ([]telephony.CallTicketLink, error) {
	normalizedIDs := make([]string, 0, len(callIDs))
	for _, item := range callIDs {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		normalizedIDs = append(normalizedIDs, item)
	}
	if len(normalizedIDs) == 0 {
		return []telephony.CallTicketLink{}, nil
	}

	items := make([]telephony.CallTicketLink, 0, len(normalizedIDs))
	err := r.getDB(ctx).WithContext(ctx).
		Where("telephony_call_id IN ?", normalizedIDs).
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) ListCallsByTicketID(ctx context.Context, ticketID string) ([]telephony.Call, error) {
	ticketID = strings.TrimSpace(ticketID)
	if ticketID == "" {
		return []telephony.Call{}, nil
	}

	items := make([]telephony.Call, 0)
	err := r.getDB(ctx).WithContext(ctx).
		Table("telephony_calls").
		Select("telephony_calls.*").
		Joins("JOIN telephony_call_ticket_links ON telephony_call_ticket_links.telephony_call_id = telephony_calls.id").
		Where("telephony_call_ticket_links.ticket_id = ?", ticketID).
		Order("COALESCE(telephony_calls.started_at, telephony_calls.created_at) desc, telephony_calls.created_at desc").
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) DeleteCallTicketLink(ctx context.Context, callID string, ticketID string) error {
	callID = strings.TrimSpace(callID)
	ticketID = strings.TrimSpace(ticketID)
	if callID == "" || ticketID == "" {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).
		Where("telephony_call_id = ? AND ticket_id = ?", callID, ticketID).
		Delete(&telephony.CallTicketLink{}).Error
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

func (r *telephonyRepo) EnsureContact(ctx context.Context, normalizedPhone string, displayPhone string) (*telephony.Contact, error) {
	return r.UpsertContact(ctx, telephony.ContactUpsert{
		PhoneNormalized: normalizedPhone,
		PhoneDisplay:    displayPhone,
	})
}

func (r *telephonyRepo) UpsertContact(ctx context.Context, input telephony.ContactUpsert) (*telephony.Contact, error) {
	normalizedPhone := strings.TrimSpace(input.PhoneNormalized)
	if normalizedPhone == "" {
		return nil, nil
	}

	displayPhone := strings.TrimSpace(input.PhoneDisplay)
	if displayPhone == "" {
		displayPhone = normalizedPhone
	}

	updates := map[string]any{
		"phone_display": displayPhone,
		"updated_at":    time.Now(),
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name != "" {
			updates["name"] = &name
		}
	}

	if input.BitrixContactID != nil {
		bitrixContactID := strings.TrimSpace(*input.BitrixContactID)
		if bitrixContactID != "" {
			updates["bitrix_contact_id"] = &bitrixContactID
		}
	}

	normalizedPhone = strings.TrimSpace(normalizedPhone)
	item := &telephony.Contact{
		PhoneNormalized: normalizedPhone,
		PhoneDisplay:    displayPhone,
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name != "" {
			item.Name = &name
		}
	}
	if input.BitrixContactID != nil {
		bitrixContactID := strings.TrimSpace(*input.BitrixContactID)
		if bitrixContactID != "" {
			item.BitrixContactID = &bitrixContactID
		}
	}
	if err := r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "phone_normalized"}},
		DoUpdates: clause.Assignments(updates),
	}).Create(item).Error; err != nil {
		return nil, err
	}
	return r.GetContactByPhone(ctx, normalizedPhone)
}

func (r *telephonyRepo) GetContactByID(ctx context.Context, id uint) (*telephony.Contact, error) {
	if id == 0 {
		return nil, nil
	}
	var item telephony.Contact
	err := r.getDB(ctx).WithContext(ctx).
		Where("id = ?", id).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) GetContactByPhone(ctx context.Context, normalizedPhone string) (*telephony.Contact, error) {
	normalizedPhone = strings.TrimSpace(normalizedPhone)
	if normalizedPhone == "" {
		return nil, nil
	}
	var item telephony.Contact
	err := r.getDB(ctx).WithContext(ctx).
		Where("phone_normalized = ?", normalizedPhone).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) UpsertContactCompanyLink(ctx context.Context, contactID uint, companyID string, lastSeenAt time.Time) error {
	companyID = strings.TrimSpace(companyID)
	if contactID == 0 || companyID == "" {
		return nil
	}
	return r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "contact_id"},
			{Name: "company_id"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen_at", "updated_at"}),
	}).Create(&telephony.ContactCompanyLink{
		ContactID:  contactID,
		CompanyID:  companyID,
		LastSeenAt: lastSeenAt,
	}).Error
}

func (r *telephonyRepo) ListContactCompanyLinks(ctx context.Context, contactID uint) ([]telephony.ContactCompanyLink, error) {
	if contactID == 0 {
		return []telephony.ContactCompanyLink{}, nil
	}
	items := make([]telephony.ContactCompanyLink, 0)
	err := r.getDB(ctx).WithContext(ctx).
		Where("contact_id = ?", contactID).
		Order("last_seen_at desc, company_id asc").
		Find(&items).Error
	return items, err
}

func (r *telephonyRepo) GetPendingContextByID(ctx context.Context, id string) (*telephony.PendingContext, error) {
	var item telephony.PendingContext
	err := r.getDB(ctx).WithContext(ctx).
		Where("id = ?", strings.TrimSpace(id)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) GetPendingContextByExternalCallID(ctx context.Context, externalCallID string) (*telephony.PendingContext, error) {
	var item telephony.PendingContext
	err := r.getDB(ctx).WithContext(ctx).
		Where("external_call_id = ?", strings.TrimSpace(externalCallID)).
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) GetActivePendingContextByUserID(ctx context.Context, userID uint, now time.Time) (*telephony.PendingContext, error) {
	if userID == 0 {
		return nil, nil
	}
	var item telephony.PendingContext
	err := r.getDB(ctx).WithContext(ctx).
		Where("employee_user_id = ? AND status = ? AND expires_at > ?", userID, telephony.PendingContextStatusNew, now).
		Order("created_at desc").
		First(&item).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &item, err
}

func (r *telephonyRepo) UpsertPendingContext(ctx context.Context, item *telephony.PendingContext) error {
	if item == nil {
		return nil
	}
	if strings.TrimSpace(item.ExternalCallID) == "" {
		return gorm.ErrInvalidData
	}

	existing, err := r.GetPendingContextByExternalCallID(ctx, item.ExternalCallID)
	if err != nil {
		return err
	}
	if existing != nil {
		item.ID = existing.ID
		item.CreatedAt = existing.CreatedAt
		return r.getDB(ctx).WithContext(ctx).
			Model(&telephony.PendingContext{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"employee_user_id": item.EmployeeUserID,
				"client_phone":     item.ClientPhone,
				"status":           item.Status,
				"expires_at":       item.ExpiresAt,
				"linked_ticket_id": item.LinkedTicketID,
				"decision_reason":  item.DecisionReason,
				"updated_at":       time.Now(),
			}).Error
	}
	return r.getDB(ctx).WithContext(ctx).Create(item).Error
}

func (r *telephonyRepo) UpdatePendingContext(ctx context.Context, id string, status string, linkedTicketID *string, decisionReason *string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&telephony.PendingContext{}).
		Where("id = ?", strings.TrimSpace(id)).
		Updates(map[string]any{
			"status":           strings.TrimSpace(status),
			"linked_ticket_id": linkedTicketID,
			"decision_reason":  decisionReason,
			"updated_at":       time.Now(),
		}).Error
}

func (r *telephonyRepo) applyCallListFilter(query *gorm.DB, filter telephony.CallListFilter) *gorm.DB {
	provider := strings.TrimSpace(filter.Provider)
	if provider != "" {
		query = query.Where("telephony_calls.provider = ?", provider)
	}
	if filter.EmployeeUserID != nil && *filter.EmployeeUserID > 0 {
		query = query.Where("telephony_calls.employee_user_id = ?", *filter.EmployeeUserID)
	}
	if clientPhone := strings.TrimSpace(filter.ClientPhone); clientPhone != "" {
		query = query.Where("telephony_calls.client_phone LIKE ?", "%"+clientPhone+"%")
	}
	if len(filter.Statuses) > 0 {
		query = query.Where("LOWER(telephony_calls.status) IN ?", normalizeTelephonyStatuses(filter.Statuses))
	}
	if len(filter.GroupNames) > 0 {
		query = query.Where("COALESCE(telephony_calls.group_name, '') IN ?", normalizeTelephonyGroupNames(filter.GroupNames))
	}
	if filter.StartedFrom != nil {
		query = query.Where(
			"COALESCE(telephony_calls.started_at, telephony_calls.created_at) >= ?",
			*filter.StartedFrom,
		)
	}
	if filter.StartedTo != nil {
		query = query.Where(
			"COALESCE(telephony_calls.started_at, telephony_calls.created_at) <= ?",
			*filter.StartedTo,
		)
	}
	if filter.OnlyWithoutTicket {
		query = query.Joins("LEFT JOIN telephony_call_ticket_links ON telephony_call_ticket_links.telephony_call_id = telephony_calls.id")
	}
	if filter.OnlyMissed {
		query = query.Where(
			`(LOWER(telephony_calls.status) IN ? OR COALESCE(telephony_calls.missed_status, '') <> '')`,
			[]string{"missed", "cancel", "cancelled", "busy", "notavailable", "notallowed", "notfound", "noanswer"},
		)
	}
	if filter.OnlyWithoutTicket {
		query = query.Where("telephony_call_ticket_links.ticket_id IS NULL")
	}
	return query
}

func normalizeTelephonyStatuses(items []string) []string {
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func normalizeTelephonyGroupNames(items []string) []string {
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	return normalized
}

func applyTelephonyHistoryCoverageScope(query *gorm.DB, employeeLogin *string) *gorm.DB {
	if login := normalizeTelephonyHistoryEmployeeLogin(employeeLogin); login != "" {
		return query.Where("(employee_login = ? OR employee_login IS NULL)", login)
	}
	return query.Where("employee_login IS NULL")
}

func applyTelephonyHistoryExactScope(query *gorm.DB, employeeLogin *string) *gorm.DB {
	if login := normalizeTelephonyHistoryEmployeeLogin(employeeLogin); login != "" {
		return query.Where("employee_login = ?", login)
	}
	return query.Where("employee_login IS NULL")
}

func normalizeTelephonyHistoryEmployeeLogin(employeeLogin *string) string {
	if employeeLogin == nil {
		return ""
	}
	return strings.TrimSpace(*employeeLogin)
}

func isTelephonyHistoryRangeCovered(
	windows []telephony.CallHistorySyncWindow,
	startedFrom time.Time,
	startedTo time.Time,
) bool {
	if len(windows) == 0 {
		return false
	}

	sort.SliceStable(windows, func(i, j int) bool {
		if windows[i].StartedFrom.Equal(windows[j].StartedFrom) {
			return windows[i].StartedTo.Before(windows[j].StartedTo)
		}
		return windows[i].StartedFrom.Before(windows[j].StartedFrom)
	})

	coverageEnd := time.Time{}
	for i := range windows {
		window := windows[i]
		if coverageEnd.IsZero() {
			if window.StartedFrom.After(startedFrom) {
				return false
			}
			coverageEnd = window.StartedTo
		} else {
			if window.StartedFrom.After(coverageEnd.Add(time.Second)) {
				return false
			}
			if window.StartedTo.After(coverageEnd) {
				coverageEnd = window.StartedTo
			}
		}
		if !coverageEnd.Before(startedTo) {
			return true
		}
	}

	return false
}
