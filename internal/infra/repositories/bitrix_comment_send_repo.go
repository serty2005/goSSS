package repositories

import (
	"context"
	"errors"
	"strings"
	"time"

	"etalon-server/internal/domain/bitrix"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const bitrixCommentDirectionEtalonToB24 = "etalon_to_b24"

func (r *bitrixRepo) InsertCommentLinkIfAbsent(ctx context.Context, link *bitrix.CommentLink) (bool, error) {
	if link == nil {
		return false, nil
	}
	res := r.getDB(ctx).WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(link)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *bitrixRepo) DeleteCommentLinkByB24ID(ctx context.Context, b24CommentID int64) error {
	return r.getDB(ctx).WithContext(ctx).Where("b24_comment_id = ?", b24CommentID).Delete(&bitrix.CommentLink{}).Error
}

func (r *bitrixRepo) MaxCommentLinkB24ID(ctx context.Context) (int64, error) {
	var maxID int64
	err := r.getDB(ctx).WithContext(ctx).Model(&bitrix.CommentLink{}).Select("COALESCE(MAX(b24_comment_id), 0)").Scan(&maxID).Error
	return maxID, err
}

func (r *bitrixRepo) ClaimCommentSend(ctx context.Context, state *bitrix.CommentSendState, maxAttempts int) (*bitrix.CommentSendState, bool, error) {
	if state == nil || strings.TrimSpace(state.EtalonCommentID) == "" {
		return nil, false, errors.New("не указан комментарий для резервирования отправки")
	}
	now := time.Now()
	state.Status = bitrix.CommentSendStatusSending
	state.Attempts = 1
	state.LastAttemptAt = now
	state.B24CommentID = nil
	state.LastError = nil

	db := r.getDB(ctx).WithContext(ctx)
	res := db.Clauses(clause.OnConflict{DoNothing: true}).Create(state)
	if res.Error != nil {
		return nil, false, res.Error
	}
	if res.RowsAffected == 1 {
		return state, true, nil
	}

	retry := db.Model(&bitrix.CommentSendState{}).
		Where("etalon_comment_id = ? AND status = ? AND attempts < ?", state.EtalonCommentID, bitrix.CommentSendStatusRejected, maxAttempts).
		Updates(map[string]any{
			"status":          bitrix.CommentSendStatusSending,
			"attempts":        gorm.Expr("attempts + 1"),
			"last_attempt_at": now,
			"fingerprint":     state.Fingerprint,
			"b24_deal_id":     state.B24DealID,
			"last_error":      nil,
			"updated_at":      now,
		})
	if retry.Error != nil {
		return nil, false, retry.Error
	}
	current, err := r.GetCommentSendState(ctx, state.EtalonCommentID)
	if err != nil {
		return nil, false, err
	}
	return current, retry.RowsAffected == 1, nil
}

func (r *bitrixRepo) GetCommentSendState(ctx context.Context, etalonCommentID string) (*bitrix.CommentSendState, error) {
	var item bitrix.CommentSendState
	err := r.getDB(ctx).WithContext(ctx).Where("etalon_comment_id = ?", etalonCommentID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// transitionCommentSend меняет статус условным UPDATE и сообщает, выполнился ли переход.
func (r *bitrixRepo) transitionCommentSend(ctx context.Context, etalonCommentID string, from []string, to string, errText string) (bool, error) {
	updates := map[string]any{"status": to, "updated_at": time.Now()}
	if text := strings.TrimSpace(errText); text != "" {
		updates["last_error"] = text
	}
	res := r.getDB(ctx).WithContext(ctx).Model(&bitrix.CommentSendState{}).
		Where("etalon_comment_id = ? AND status IN ?", etalonCommentID, from).
		Updates(updates)
	return res.RowsAffected > 0, res.Error
}

func (r *bitrixRepo) MarkCommentSendAmbiguous(ctx context.Context, etalonCommentID string, errText string) error {
	_, err := r.transitionCommentSend(ctx, etalonCommentID, []string{bitrix.CommentSendStatusSending}, bitrix.CommentSendStatusAmbiguous, errText)
	return err
}

func (r *bitrixRepo) MarkCommentSendRejected(ctx context.Context, etalonCommentID string, errText string) error {
	_, err := r.transitionCommentSend(ctx, etalonCommentID, []string{bitrix.CommentSendStatusSending}, bitrix.CommentSendStatusRejected, errText)
	return err
}

func (r *bitrixRepo) MarkCommentSendNeedsReconcile(ctx context.Context, etalonCommentID string) (bool, error) {
	return r.transitionCommentSend(ctx, etalonCommentID, []string{bitrix.CommentSendStatusSending}, bitrix.CommentSendStatusAmbiguous, "результат отправки не получен вовремя")
}

func (r *bitrixRepo) ResolveCommentSendAbsent(ctx context.Context, etalonCommentID string, errText string) (bool, error) {
	return r.transitionCommentSend(
		ctx,
		etalonCommentID,
		[]string{bitrix.CommentSendStatusSending, bitrix.CommentSendStatusAmbiguous},
		bitrix.CommentSendStatusRejected,
		errText,
	)
}

func (r *bitrixRepo) ConfirmCommentSend(ctx context.Context, etalonCommentID string, b24CommentID int64, finalizePending bool) (bool, error) {
	if b24CommentID <= 0 {
		return false, errors.New("не указан идентификатор комментария Bitrix24")
	}
	confirmed := false
	err := r.getDB(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state bitrix.CommentSendState
		if err := tx.Where("etalon_comment_id = ?", etalonCommentID).First(&state).Error; err != nil {
			return err
		}
		if state.Status == bitrix.CommentSendStatusConfirmed {
			confirmed = state.B24CommentID != nil && *state.B24CommentID == b24CommentID
			return nil
		}
		link := bitrix.CommentLink{
			EtalonCommentID: etalonCommentID,
			B24CommentID:    b24CommentID,
			TicketID:        state.TicketID,
			Direction:       bitrixCommentDirectionEtalonToB24,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "etalon_comment_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"b24_comment_id", "ticket_id", "direction", "updated_at"}),
		}).Create(&link).Error; err != nil {
			return err
		}
		if err := tx.Model(&bitrix.CommentSendState{}).Where("etalon_comment_id = ?", etalonCommentID).Updates(map[string]any{
			"status":           bitrix.CommentSendStatusConfirmed,
			"b24_comment_id":   b24CommentID,
			"finalize_pending": finalizePending,
			"last_error":       nil,
			"updated_at":       time.Now(),
		}).Error; err != nil {
			return err
		}
		confirmed = true
		return nil
	})
	return confirmed, err
}

func (r *bitrixRepo) SetCommentSendFinalizePending(ctx context.Context, etalonCommentID string, pending bool) error {
	return r.getDB(ctx).WithContext(ctx).Model(&bitrix.CommentSendState{}).
		Where("etalon_comment_id = ?", etalonCommentID).
		Updates(map[string]any{"finalize_pending": pending, "updated_at": time.Now()}).Error
}

func (r *bitrixRepo) UpdateCommentSendFingerprint(ctx context.Context, etalonCommentID string, fingerprint string) error {
	return r.getDB(ctx).WithContext(ctx).Model(&bitrix.CommentSendState{}).
		Where("etalon_comment_id = ?", etalonCommentID).
		Updates(map[string]any{"fingerprint": fingerprint, "updated_at": time.Now()}).Error
}

func (r *bitrixRepo) ListOpenCommentSendsByTicket(ctx context.Context, ticketID string) ([]bitrix.CommentSendState, error) {
	var items []bitrix.CommentSendState
	err := r.getDB(ctx).WithContext(ctx).
		Where("ticket_id = ? AND status IN ?", ticketID, []string{bitrix.CommentSendStatusSending, bitrix.CommentSendStatusAmbiguous}).
		Order("last_attempt_at ASC").
		Find(&items).Error
	return items, err
}

func (r *bitrixRepo) ListCommentSendsForReconcile(ctx context.Context, openBefore time.Time, finalizeBefore time.Time, limit int) ([]bitrix.CommentSendState, error) {
	if limit <= 0 {
		limit = 20
	}
	var items []bitrix.CommentSendState
	err := r.getDB(ctx).WithContext(ctx).
		Where(
			"(status IN ? AND last_attempt_at <= ?) OR (status = ? AND finalize_pending = ? AND updated_at <= ?)",
			[]string{bitrix.CommentSendStatusSending, bitrix.CommentSendStatusAmbiguous}, openBefore,
			bitrix.CommentSendStatusConfirmed, true, finalizeBefore,
		).
		Order("last_attempt_at ASC").
		Limit(limit).
		Find(&items).Error
	return items, err
}
