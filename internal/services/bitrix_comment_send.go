package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/tickets"
	b24 "etalon-server/internal/infra/plugins/bitrix"
)

const (
	// bitrixCommentMaxSendAttempts ограничивает число обращений к Bitrix24 за одним комментарием.
	bitrixCommentMaxSendAttempts = 3
	// bitrixCommentFinalizeDelay - пауза перед дооформлением превью подтверждённого комментария фоновой сверкой.
	bitrixCommentFinalizeDelay = 15 * time.Second
	// bitrixCommentReconcileBatch - сколько неподтверждённых отправок обрабатывается за один проход сверки.
	bitrixCommentReconcileBatch = 20
	defaultBitrixCommentGrace   = 2 * time.Minute
	// bitrixCommentWatermarkMargin - запас по ID на случай параллельной выдачи идентификаторов (наблюдаемый откат ID не превышал 4).
	bitrixCommentWatermarkMargin = 50
	// bitrixCommentCreatedSkew - допуск на расхождение часов ServiceDesk и Bitrix24 при сравнении CREATED.
	bitrixCommentCreatedSkew = 2 * time.Minute
)

// errBitrixCommentSendExhausted означает, что лимит попыток отправки комментария исчерпан и автоматических повторов больше нет.
var errBitrixCommentSendExhausted = errors.New("исчерпан лимит попыток отправки комментария в Bitrix24")

var (
	bitrixFingerprintBBCodeRe  = regexp.MustCompile(`\[/?[A-Za-z]+[^\]]*\]`)
	bitrixFingerprintHTMLTagRe = regexp.MustCompile(`<[^>]*>`)
	// bitrixEmptyCommentFingerprint - отпечаток комментария без текста (только вложения).
	bitrixEmptyCommentFingerprint = bitrixCommentFingerprint("")
)

// bitrixOutgoingComment - подготовленная к отправке версия комментария.
type bitrixOutgoingComment struct {
	Message     string
	Files       []b24.FileToUpload
	Fingerprint string
}

// bitrixCommentFingerprint возвращает отпечаток текста комментария, устойчивый к разметке Bitrix24:
// теги, BBCode, ссылки на inline-файлы, регистр и пробелы не влияют на результат.
func bitrixCommentFingerprint(text string) string {
	text = html.UnescapeString(text)
	text = bitrixInlineStaticRefRe.ReplaceAllString(text, "")
	text = bitrixFingerprintBBCodeRe.ReplaceAllString(text, "")
	text = bitrixFingerprintHTMLTagRe.ReplaceAllString(text, "")

	var normalized strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsSpace(r) || r == 0x200b || r == 0xfeff {
			continue
		}
		normalized.WriteRune(r)
	}
	sum := sha256.Sum256([]byte(normalized.String()))
	return hex.EncodeToString(sum[:])
}

// bitrixCommentMatchesSend проверяет, что комментарий Bitrix24 - это отправленная версия комментария из состояния.
func bitrixCommentMatchesSend(comment b24.TimelineComment, state bitrix.CommentSendState, integrationUserID int64) bool {
	if comment.ID <= 0 || comment.EntityType != "deal" || comment.EntityID != state.B24DealID {
		return false
	}
	if integrationUserID > 0 && comment.AuthorID != nil && *comment.AuthorID != integrationUserID {
		return false
	}
	if state.WatermarkB24ID > 0 && comment.ID <= state.WatermarkB24ID-bitrixCommentWatermarkMargin {
		return false
	}
	if created, ok := bitrixCommentCreatedAt(comment.Raw); ok && created.Before(state.CreatedAt.Add(-bitrixCommentCreatedSkew)) {
		return false
	}
	if state.Fingerprint != bitrixEmptyCommentFingerprint {
		return bitrixCommentFingerprint(comment.Comment) == state.Fingerprint
	}
	return bitrixCommentHasFiles(comment.Raw) && bitrixCommentFingerprint(comment.Comment) == bitrixEmptyCommentFingerprint
}

// bitrixCommentCreatedAt читает время создания комментария (ISO 8601 со смещением) из ответа Bitrix24.
func bitrixCommentCreatedAt(raw map[string]interface{}) (time.Time, bool) {
	value, _ := raw["CREATED"].(string)
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func bitrixCommentHasFiles(raw map[string]interface{}) bool {
	switch files := raw["FILES"].(type) {
	case map[string]interface{}:
		return len(files) > 0
	case []interface{}:
		return len(files) > 0
	default:
		return false
	}
}

func (s *bitrixSyncService) commentConfirmGrace() time.Duration {
	if s.cfg != nil && s.cfg.BitrixCommentConfirmGrace > 0 {
		return s.cfg.BitrixCommentConfirmGrace
	}
	return defaultBitrixCommentGrace
}

func (s *bitrixSyncService) integrationUserID() int64 {
	if s.cfg == nil {
		return 0
	}
	return s.cfg.BitrixIntegrationUserID
}

// buildOutgoingComment собирает текст, вложения и отпечаток исходящей версии комментария.
func (s *bitrixSyncService) buildOutgoingComment(
	ctx context.Context,
	ticketID string,
	comment *tickets.TicketComment,
	authorID *int64,
	authorName string,
) (*bitrixOutgoingComment, error) {
	message := s.buildCommentBody(ctx, ticketID, comment, authorID, authorName)
	files, err := s.buildCommentFilesPayload(ctx, ticketID, comment)
	if err != nil {
		return nil, err
	}
	return &bitrixOutgoingComment{
		Message:     message,
		Files:       files,
		Fingerprint: bitrixCommentFingerprint(message),
	}, nil
}

// sendNewComment - единственная точка исходящей отправки нового комментария в Bitrix24.
// Перед обращением к Bitrix24 отправка резервируется в БД, поэтому параллельные вызовы, повторы событий
// и обычная синхронизация тикета не могут отправить один и тот же комментарий дважды.
func (s *bitrixSyncService) sendNewComment(
	ctx context.Context,
	ticketID string,
	dealID int64,
	comment *tickets.TicketComment,
	out *bitrixOutgoingComment,
) error {
	// Граница ID фиксируется до обращения к Bitrix24: все комментарии, созданные этой отправкой, имеют больший ID.
	watermark, err := s.repo.MaxCommentLinkB24ID(ctx)
	if err != nil {
		return err
	}
	claim := &bitrix.CommentSendState{
		EtalonCommentID: comment.ID,
		TicketID:        ticketID,
		B24DealID:       dealID,
		Fingerprint:     out.Fingerprint,
		WatermarkB24ID:  watermark,
	}
	current, claimed, err := s.repo.ClaimCommentSend(ctx, claim, bitrixCommentMaxSendAttempts)
	if err != nil {
		return err
	}
	if !claimed {
		return s.explainUnclaimedCommentSend(ticketID, comment.ID, current)
	}

	// Состояние после вызова Bitrix24 фиксируется даже при остановке сервера.
	stateCtx := context.WithoutCancel(ctx)

	var b24ID int64
	if len(out.Files) > 0 {
		b24ID, err = s.client.TimelineCommentAddWithFiles(ctx, "deal", dealID, out.Message, out.Files)
	} else {
		b24ID, err = s.client.TimelineCommentAdd(ctx, dealID, out.Message, nil)
	}
	if err != nil {
		if errors.Is(err, b24.ErrResultUnknown) {
			if markErr := s.repo.MarkCommentSendAmbiguous(stateCtx, comment.ID, err.Error()); markErr != nil {
				s.log.Error("Bitrix24: не удалось сохранить неоднозначный результат отправки комментария", "ticket_id", ticketID, "comment_id", comment.ID, "error", markErr)
			}
			// Вебхук мог подтвердить комментарий раньше, чем истёк таймаут ответа: тогда отправка уже завершена.
			if current, getErr := s.repo.GetCommentSendState(stateCtx, comment.ID); getErr == nil && current != nil && current.Status == bitrix.CommentSendStatusConfirmed {
				s.log.Info("Bitrix24: ответ на отправку комментария не получен, но комментарий уже подтверждён событием", "ticket_id", ticketID, "comment_id", comment.ID)
				return nil
			}
			s.log.Warn("Bitrix24: результат отправки комментария неизвестен, повтор запрещён до сверки с таймлайном сделки", "ticket_id", ticketID, "comment_id", comment.ID, "deal_id", dealID, "error", err)
			return err
		}
		if markErr := s.repo.MarkCommentSendRejected(stateCtx, comment.ID, err.Error()); markErr != nil {
			s.log.Error("Bitrix24: не удалось сохранить отказ отправки комментария", "ticket_id", ticketID, "comment_id", comment.ID, "error", markErr)
		}
		return err
	}

	s.setCommentSuppress(ctx, b24ID)
	confirmed, err := s.repo.ConfirmCommentSend(stateCtx, comment.ID, b24ID, false)
	if err != nil {
		return fmt.Errorf("комментарий создан в Bitrix24 (id=%d), но связь не сохранена: %w", b24ID, err)
	}
	if !confirmed {
		s.log.Warn("Bitrix24: комментарий уже подтверждён другим идентификатором, созданный дубль оставлен без изменений", "ticket_id", ticketID, "comment_id", comment.ID, "bitrix_comment_id", b24ID)
		return nil
	}

	if err := s.applyImagePreview(ctx, b24ID, out.Message, out.Files); err != nil {
		// Комментарий уже доставлен и связан; превью дооформит фоновая сверка.
		s.log.Warn("Bitrix24: не удалось оформить превью картинок комментария, дооформление отложено", "ticket_id", ticketID, "comment_id", comment.ID, "bitrix_comment_id", b24ID, "error", err)
		if markErr := s.repo.SetCommentSendFinalizePending(stateCtx, comment.ID, true); markErr != nil {
			s.log.Error("Bitrix24: не удалось отложить оформление превью комментария", "comment_id", comment.ID, "error", markErr)
		}
		return nil
	}
	return nil
}

// explainUnclaimedCommentSend объясняет, почему отправку нельзя выполнять повторно.
func (s *bitrixSyncService) explainUnclaimedCommentSend(ticketID string, commentID string, current *bitrix.CommentSendState) error {
	if current == nil {
		return nil
	}
	switch current.Status {
	case bitrix.CommentSendStatusConfirmed:
		return nil
	case bitrix.CommentSendStatusSending, bitrix.CommentSendStatusAmbiguous:
		s.log.Info("Bitrix24: отправка комментария уже выполняется или ожидает подтверждения, повтор не требуется", "ticket_id", ticketID, "comment_id", commentID, "status", current.Status, "attempts", current.Attempts)
		return nil
	default:
		s.log.Error("Bitrix24: исчерпан лимит попыток отправки комментария, требуется ручной разбор", "ticket_id", ticketID, "comment_id", commentID, "attempts", current.Attempts)
		return fmt.Errorf("комментарий %s: %w", commentID, errBitrixCommentSendExhausted)
	}
}

// applyImagePreview заменяет ссылки на inline-файлы в отправленном комментарии маркерами диска Bitrix24.
// Операция идемпотентна: итоговый текст всегда строится из исходного сообщения.
func (s *bitrixSyncService) applyImagePreview(ctx context.Context, b24CommentID int64, message string, files []b24.FileToUpload) error {
	if len(files) == 0 {
		return nil
	}
	finalText, err := s.rewriteCommentForBitrixImagePreview(ctx, b24CommentID, message, files)
	if err != nil {
		return err
	}
	if strings.TrimSpace(finalText) == strings.TrimSpace(message) {
		return nil
	}
	s.setCommentSuppress(ctx, b24CommentID)
	if err := s.client.TimelineCommentUpdateWithFiles(ctx, b24CommentID, finalText, nil); err != nil {
		return err
	}
	s.setCommentSuppress(ctx, b24CommentID)
	return nil
}

// ReconcileCommentSends сверяет неподтверждённые исходящие комментарии с таймлайном сделки Bitrix24
// и дооформляет превью подтверждённых комментариев.
func (s *bitrixSyncService) ReconcileCommentSends(ctx context.Context) (int, error) {
	if !s.IsEnabled() || s.repo == nil {
		return 0, nil
	}
	now := time.Now()
	states, err := s.repo.ListCommentSendsForReconcile(ctx, now.Add(-s.commentConfirmGrace()), now.Add(-bitrixCommentFinalizeDelay), bitrixCommentReconcileBatch)
	if err != nil {
		return 0, err
	}
	handled := 0
	for i := range states {
		if ctx.Err() != nil {
			break
		}
		state := states[i]
		var stepErr error
		if state.Status == bitrix.CommentSendStatusConfirmed {
			stepErr = s.finalizeConfirmedComment(ctx, state.EtalonCommentID)
		} else {
			stepErr = s.reconcileCommentSend(ctx, state)
		}
		if stepErr != nil {
			s.log.Warn("Bitrix24: сверка исходящего комментария отложена до следующего прохода", "ticket_id", state.TicketID, "comment_id", state.EtalonCommentID, "status", state.Status, "error", stepErr)
			continue
		}
		handled++
	}
	return handled, nil
}

func (s *bitrixSyncService) reconcileCommentSend(ctx context.Context, state bitrix.CommentSendState) error {
	if state.Status == bitrix.CommentSendStatusSending {
		moved, err := s.repo.MarkCommentSendNeedsReconcile(ctx, state.EtalonCommentID)
		if err != nil {
			return err
		}
		if !moved {
			return nil
		}
	}

	comments, err := s.client.TimelineCommentListSince(ctx, state.B24DealID, max(state.WatermarkB24ID-bitrixCommentWatermarkMargin, 0))
	if err != nil {
		return err
	}
	// При нескольких подходящих копиях подтверждается самая ранняя: так выбор детерминирован.
	sort.Slice(comments, func(i, j int) bool { return comments[i].ID < comments[j].ID })
	for i := range comments {
		if !bitrixCommentMatchesSend(comments[i], state, s.integrationUserID()) {
			continue
		}
		link, err := s.repo.GetCommentLinkByB24ID(ctx, comments[i].ID)
		if err != nil {
			return err
		}
		if link != nil {
			continue
		}
		return s.adoptSentComment(ctx, state, comments[i].ID)
	}

	absent, err := s.repo.ResolveCommentSendAbsent(ctx, state.EtalonCommentID, "комментарий не найден в таймлайне сделки Bitrix24")
	if err != nil || !absent {
		return err
	}
	s.log.Warn("Bitrix24: комментарий достоверно отсутствует в сделке, отправка будет повторена", "ticket_id", state.TicketID, "comment_id", state.EtalonCommentID, "attempts", state.Attempts)
	return s.resendRejectedComment(ctx, state)
}

// adoptSentComment связывает ранее отправленный комментарий с найденным в Bitrix24.
func (s *bitrixSyncService) adoptSentComment(ctx context.Context, state bitrix.CommentSendState, b24CommentID int64) error {
	s.setCommentSuppress(ctx, b24CommentID)
	confirmed, err := s.repo.ConfirmCommentSend(ctx, state.EtalonCommentID, b24CommentID, true)
	if err != nil {
		return err
	}
	if !confirmed {
		return nil
	}
	s.log.Info("Bitrix24: исходящий комментарий подтверждён сверкой", "ticket_id", state.TicketID, "comment_id", state.EtalonCommentID, "bitrix_comment_id", b24CommentID)
	return s.finalizeConfirmedComment(ctx, state.EtalonCommentID)
}

// finalizeConfirmedComment дооформляет превью картинок подтверждённого комментария.
func (s *bitrixSyncService) finalizeConfirmedComment(ctx context.Context, etalonCommentID string) error {
	state, err := s.repo.GetCommentSendState(ctx, etalonCommentID)
	if err != nil {
		return err
	}
	if state == nil || state.Status != bitrix.CommentSendStatusConfirmed || !state.FinalizePending || state.B24CommentID == nil {
		return nil
	}
	comment, err := s.ticketRepo.GetCommentByUUID(ctx, state.TicketID, state.EtalonCommentID)
	if err != nil {
		return err
	}
	if comment != nil {
		var authorID *int64
		authorName := ""
		if comment.AuthorUserID != nil {
			if authorID, err = s.resolveBitrixUserID(ctx, *comment.AuthorUserID); err != nil {
				return err
			}
			if authorName, err = s.resolveEtalonUserDisplayName(ctx, *comment.AuthorUserID); err != nil {
				return err
			}
		}
		out, err := s.buildOutgoingComment(ctx, state.TicketID, comment, authorID, authorName)
		if err != nil {
			return err
		}
		if err := s.applyImagePreview(ctx, *state.B24CommentID, out.Message, out.Files); err != nil {
			return err
		}
	}
	return s.repo.SetCommentSendFinalizePending(ctx, state.EtalonCommentID, false)
}

// resendRejectedComment повторяет отправку комментария, отсутствие которого в Bitrix24 подтверждено.
func (s *bitrixSyncService) resendRejectedComment(ctx context.Context, state bitrix.CommentSendState) error {
	comment, err := s.ticketRepo.GetCommentByUUID(ctx, state.TicketID, state.EtalonCommentID)
	if err != nil {
		return err
	}
	if comment == nil || comment.AuthorUserID == nil {
		s.log.Warn("Bitrix24: повторная отправка комментария невозможна, комментарий удалён или без автора", "ticket_id", state.TicketID, "comment_id", state.EtalonCommentID)
		return nil
	}
	return s.SyncComment(ctx, state.TicketID, comment, *comment.AuthorUserID)
}
