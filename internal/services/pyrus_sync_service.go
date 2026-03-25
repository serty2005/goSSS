package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type PyrusSyncService interface {
	IsEnabled() bool
	Start(ctx context.Context)
	EnqueueEvent(ctx context.Context, eventName string, payload events.PyrusSyncEntityPayload) error
}

type pyrusSyncService struct {
	cfg        *config.Config
	log        logger.LoggerInterface
	client     pyrusAPIClient
	redis      *redis.Client
	ticketRepo tickets.TicketRepository
	repo       pyrus.Repository
}

func NewPyrusSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client pyrusAPIClient,
	redisClient *redis.Client,
	ticketRepo tickets.TicketRepository,
	repo pyrus.Repository,
) PyrusSyncService {
	return &pyrusSyncService{
		cfg:        cfg,
		log:        log,
		client:     client,
		redis:      redisClient,
		ticketRepo: ticketRepo,
		repo:       repo,
	}
}

func (s *pyrusSyncService) IsEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.EnablePyrusGateway && s.client != nil && s.client.IsConfigured() && s.repo != nil
}

func (s *pyrusSyncService) EnqueueEvent(ctx context.Context, eventName string, payload events.PyrusSyncEntityPayload) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("Pyrus sync service не настроен")
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	item := &pyrus.OutgoingEvent{
		ID:          uuid.NewString(),
		EventName:   strings.TrimSpace(eventName),
		PayloadJSON: string(rawPayload),
		Status:      pyrus.OutgoingEventStatusNew,
		QueuedAt:    time.Now(),
	}
	if payload.TicketID != "" {
		ticketID := strings.TrimSpace(payload.TicketID)
		item.TicketID = &ticketID
	}
	if payload.TaskID > 0 {
		taskID := payload.TaskID
		item.PyrusTaskID = &taskID
	}

	if err = s.repo.InsertOutgoingEvent(ctx, item); err != nil {
		return err
	}
	if s.log != nil {
		s.log.Debug(
			"Pyrus: исходящее событие поставлено в очередь",
			"event_id", item.ID,
			"event_name", item.EventName,
			"ticket_id", payload.TicketID,
			"task_id", payload.TaskID,
			"reason", payload.Reason,
			"status", payload.Status,
			"comment", localTicketCommentSummary(payload.Comment),
		)
	}
	return nil
}

func (s *pyrusSyncService) Start(ctx context.Context) {
	if s == nil || s.cfg == nil || !s.cfg.EnablePyrusGateway {
		return
	}
	if s.repo == nil {
		s.log.Warn("Pyrus: исходящая синхронизация отключена, репозиторий не настроен")
		return
	}
	if s.client == nil || !s.client.IsConfigured() {
		s.log.Warn(
			"Pyrus: исходящая синхронизация отключена, клиент API не настроен",
			"login_present", strings.TrimSpace(s.cfg.PyrusLogin) != "",
			"security_key_present", strings.TrimSpace(s.cfg.PyrusSecurityKey) != "",
		)
		return
	}
	s.log.Info(
		"Pyrus: исходящая синхронизация запущена",
		"form_id", s.cfg.PyrusFormID,
		"redis_enabled", s.redis != nil,
	)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	s.processOutgoingBatch(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processOutgoingBatch(ctx)
		}
	}
}

func (s *pyrusSyncService) processOutgoingBatch(ctx context.Context) {
	items, err := s.repo.ListOutgoingEventsForRetry(ctx, 100, s.maxAttempts())
	if err != nil {
		s.log.Error("Pyrus: не удалось выбрать исходящие события для обработки", "error", err)
		return
	}
	if len(items) > 0 {
		s.log.Debug("Pyrus: выбрана пачка исходящих событий", "count", len(items))
	}
	for i := range items {
		if !s.shouldProcessOutgoingNow(&items[i]) {
			continue
		}
		s.processOutgoingEvent(ctx, &items[i])
	}
}

func (s *pyrusSyncService) processOutgoingEvent(ctx context.Context, item *pyrus.OutgoingEvent) {
	if item == nil {
		return
	}
	s.log.Debug(
		"Pyrus: начало обработки исходящего события",
		"event_id", item.ID,
		"event_name", item.EventName,
		"ticket_id", safeStringPointer(item.TicketID),
		"task_id", safeInt64Pointer(item.PyrusTaskID),
		"attempts", item.Attempts,
		"status", item.Status,
	)
	if err := s.repo.MarkOutgoingProcessing(ctx, item.ID); err != nil {
		s.log.Warn("Pyrus: не удалось отметить исходящее событие как processing", "event_id", item.ID, "error", err)
	}

	status, reason, err := s.handleOutgoingEvent(ctx, item)
	if err != nil {
		s.log.Error("Pyrus: ошибка обработки исходящего события", "event_id", item.ID, "event_name", item.EventName, "error", err)
		_ = s.repo.MarkOutgoingFailed(ctx, item.ID, err.Error())
		return
	}
	if status == pyrus.OutgoingEventStatusIgnored {
		s.log.Info("Pyrus: исходящее событие проигнорировано", "event_id", item.ID, "event_name", item.EventName, "reason", reason)
		_ = s.repo.MarkOutgoingIgnored(ctx, item.ID, reason)
		return
	}
	s.log.Debug("Pyrus: исходящее событие обработано", "event_id", item.ID, "event_name", item.EventName, "result_status", status)
	_ = s.repo.MarkOutgoingDone(ctx, item.ID)
}

func (s *pyrusSyncService) handleOutgoingEvent(ctx context.Context, item *pyrus.OutgoingEvent) (string, string, error) {
	payload := events.PyrusSyncEntityPayload{}
	if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
		return "", "", err
	}
	s.log.Debug(
		"Pyrus: разобрано исходящее событие",
		"event_id", item.ID,
		"event_name", item.EventName,
		"ticket_id", payload.TicketID,
		"task_id", payload.TaskID,
		"reason", payload.Reason,
		"status", payload.Status,
		"ext_id", payload.ExtID,
		"comment", localTicketCommentSummary(payload.Comment),
	)

	switch strings.TrimSpace(item.EventName) {
	case events.PyrusTicketExtIDSyncRequested:
		return s.handleExtIDSync(ctx, item, payload)
	case events.PyrusCommentSyncRequested:
		return s.handleCommentSync(ctx, item, payload)
	case events.PyrusTicketStatusSyncRequested:
		return s.handleStatusSync(ctx, item, payload)
	case events.PyrusTicketSyncRequested:
		if strings.TrimSpace(payload.Status) == "" {
			return pyrus.OutgoingEventStatusIgnored, "в событии ticket.sync не передан статус для синхронизации", nil
		}
		return s.handleStatusSync(ctx, item, payload)
	case events.PyrusTicketAssigneeSyncRequested:
		return pyrus.OutgoingEventStatusIgnored, "синхронизация исполнителя в Pyrus пока не реализована", nil
	default:
		return pyrus.OutgoingEventStatusIgnored, "неподдерживаемое исходящее событие", nil
	}
}

func (s *pyrusSyncService) handleExtIDSync(
	ctx context.Context,
	item *pyrus.OutgoingEvent,
	payload events.PyrusSyncEntityPayload,
) (string, string, error) {
	taskID := payload.TaskID
	extID := strings.TrimSpace(payload.ExtID)
	ticketID := strings.TrimSpace(payload.TicketID)

	if taskID <= 0 && ticketID != "" {
		link, err := s.repo.GetTicketLinkByTicketID(ctx, ticketID)
		if err != nil {
			return "", "", err
		}
		if link != nil {
			taskID = link.PyrusTaskID
		}
	}
	if extID == "" {
		extID = ticketID
	}
	if taskID <= 0 || extID == "" {
		return "", "", fmt.Errorf("для обновления ext_id не хватает task_id или ext_id")
	}
	s.log.Debug("Pyrus: отправка ext_id в задачу", "event_id", item.ID, "task_id", taskID, "ticket_id", ticketID, "ext_id", extID)

	task, err := s.client.UpdateTaskExtID(ctx, taskID, extID)
	if err != nil {
		return "", "", err
	}

	now := time.Now()
	if ticketID != "" {
		if err := s.repo.UpsertTicketLink(ctx, &pyrus.TicketLink{
			TicketID:       ticketID,
			PyrusTaskID:    taskID,
			LastOutgoingAt: &now,
		}); err != nil {
			s.log.Warn("Pyrus: не удалось обновить ticket link после отправки ext_id", "ticket_id", ticketID, "task_id", taskID, "error", err)
		}
	}
	if task != nil {
		if comment := findPyrusExtIDComment(task, extID); comment != nil && comment.ID > 0 {
			commentID := comment.ID
			link := &pyrus.CommentLink{
				EtalonCommentID: "pyrus-extid-sync:" + item.ID,
				PyrusCommentID:  &commentID,
				PyrusTaskID:     taskID,
				Direction:       "local_to_pyrus",
				Fingerprint:     pyrusCommentFingerprint(comment),
			}
			if err := s.repo.UpsertCommentLink(ctx, link); err != nil {
				s.log.Warn("Pyrus: не удалось сохранить link для служебного ext_id комментария", "event_id", item.ID, "comment_id", commentID, "error", err)
			}
		}
	}

	s.setSuppressTask(ctx, taskID)
	s.log.Debug("Pyrus: ext_id синхронизирован", "event_id", item.ID, "task_id", taskID, "ticket_id", ticketID, "ext_id", extID)
	return pyrus.OutgoingEventStatusDone, "", nil
}

func (s *pyrusSyncService) handleCommentSync(
	ctx context.Context,
	_ *pyrus.OutgoingEvent,
	payload events.PyrusSyncEntityPayload,
) (string, string, error) {
	comment := payload.Comment
	if comment == nil {
		return pyrus.OutgoingEventStatusIgnored, "в событии отсутствует комментарий", nil
	}
	if comment.IsPrivate {
		return pyrus.OutgoingEventStatusIgnored, "приватные комментарии не синхронизируются в Pyrus", nil
	}

	taskID, ticketID, err := s.resolveTaskAndTicketIDs(ctx, payload)
	if err != nil {
		return "", "", err
	}
	s.log.Debug("Pyrus: синхронизация комментария", "task_id", taskID, "ticket_id", ticketID, "comment", localTicketCommentSummary(comment))
	if err := s.syncSingleComment(ctx, taskID, ticketID, comment); err != nil {
		return "", "", err
	}
	return pyrus.OutgoingEventStatusDone, "", nil
}

func (s *pyrusSyncService) handleStatusSync(
	ctx context.Context,
	item *pyrus.OutgoingEvent,
	payload events.PyrusSyncEntityPayload,
) (string, string, error) {
	taskID, ticketID, err := s.resolveTaskAndTicketIDs(ctx, payload)
	if err != nil {
		return "", "", err
	}
	if ticketID == "" {
		return pyrus.OutgoingEventStatusIgnored, "для синхронизации статуса нужен локальный ticket_id", nil
	}

	if err := s.syncPendingPublicComments(ctx, taskID, ticketID); err != nil {
		return "", "", err
	}

	task, err := s.client.GetTask(ctx, taskID)
	if err != nil {
		return "", "", err
	}
	req, reason, err := buildPyrusStatusCommentRequest(task, payload.Status)
	if err != nil {
		return "", "", err
	}
	if reason != "" {
		return pyrus.OutgoingEventStatusIgnored, reason, nil
	}

	updatedTask, err := s.client.AddComment(ctx, taskID, req)
	if err != nil {
		return "", "", err
	}
	s.log.Debug("Pyrus: статус отправлен в задачу", "event_id", item.ID, "task_id", taskID, "ticket_id", ticketID, "request", truncateForPyrusLog(fmt.Sprintf("%+v", req), pyrusLogTextPreviewLimit))

	now := time.Now()
	if err := s.repo.UpsertTicketLink(ctx, &pyrus.TicketLink{
		TicketID:       ticketID,
		PyrusTaskID:    taskID,
		LastOutgoingAt: &now,
	}); err != nil {
		s.log.Warn("Pyrus: не удалось обновить ticket link после синхронизации статуса", "ticket_id", ticketID, "task_id", taskID, "error", err)
	}
	if updatedTask != nil {
		if comment := findPyrusStatusComment(updatedTask, req.Action); comment != nil && comment.ID > 0 {
			commentID := comment.ID
			link := &pyrus.CommentLink{
				EtalonCommentID: "pyrus-status-sync:" + item.ID,
				PyrusCommentID:  &commentID,
				PyrusTaskID:     taskID,
				Direction:       "local_to_pyrus",
				Fingerprint:     pyrusCommentFingerprint(comment),
			}
			if err := s.repo.UpsertCommentLink(ctx, link); err != nil {
				s.log.Warn("Pyrus: не удалось сохранить link для служебного комментария смены статуса", "event_id", item.ID, "comment_id", commentID, "error", err)
			}
		}
	}

	s.setSuppressTask(ctx, taskID)
	return pyrus.OutgoingEventStatusDone, "", nil
}

func (s *pyrusSyncService) resolveTaskAndTicketIDs(ctx context.Context, payload events.PyrusSyncEntityPayload) (int64, string, error) {
	taskID := payload.TaskID
	ticketID := strings.TrimSpace(payload.TicketID)

	if taskID <= 0 && ticketID != "" {
		link, err := s.repo.GetTicketLinkByTicketID(ctx, ticketID)
		if err != nil {
			return 0, "", err
		}
		if link != nil {
			taskID = link.PyrusTaskID
		}
	}
	if ticketID == "" && taskID > 0 {
		link, err := s.repo.GetTicketLinkByTaskID(ctx, taskID)
		if err != nil {
			return 0, "", err
		}
		if link != nil {
			ticketID = strings.TrimSpace(link.TicketID)
		}
	}
	if taskID <= 0 {
		return 0, ticketID, fmt.Errorf("не удалось определить task_id Pyrus для исходящей синхронизации")
	}
	return taskID, ticketID, nil
}

func (s *pyrusSyncService) syncPendingPublicComments(ctx context.Context, taskID int64, ticketID string) error {
	if s.ticketRepo == nil || strings.TrimSpace(ticketID) == "" {
		return nil
	}
	comments, err := s.ticketRepo.GetComments(ctx, ticketID)
	if err != nil {
		return err
	}
	for i := range comments {
		comment := comments[i]
		if comment.IsPrivate {
			continue
		}
		link, err := s.repo.GetCommentLinkByEtalonID(ctx, comment.ID)
		if err != nil {
			return err
		}
		if link != nil {
			continue
		}
		s.log.Debug("Pyrus: найден несинхронизированный публичный комментарий перед сменой статуса", "task_id", taskID, "ticket_id", ticketID, "comment", localTicketCommentSummary(&comment))
		if err := s.syncSingleComment(ctx, taskID, ticketID, &comment); err != nil {
			return err
		}
	}
	return nil
}

func (s *pyrusSyncService) syncSingleComment(ctx context.Context, taskID int64, ticketID string, comment *tickets.TicketComment) error {
	if comment == nil {
		return nil
	}
	text := strings.TrimSpace(comment.Text)
	if text == "" {
		return nil
	}

	existing, err := s.repo.GetCommentLinkByEtalonID(ctx, comment.ID)
	if err != nil {
		return err
	}

	req := pyrusplugin.CommentRequest{
		Text: text,
	}
	attachmentGUIDs, err := s.uploadCommentAttachments(ctx, ticketID, comment)
	if err != nil {
		return err
	}
	if len(attachmentGUIDs) > 0 {
		req.Attachments = attachmentGUIDs
	}
	if existing != nil && existing.PyrusCommentID != nil && *existing.PyrusCommentID > 0 {
		commentID := *existing.PyrusCommentID
		req.EditCommentID = &commentID
	} else {
		req.Channel = &pyrusplugin.Channel{Type: "mobile_app"}
	}
	s.log.Debug(
		"Pyrus: отправка комментария оператора",
		"task_id", taskID,
		"ticket_id", ticketID,
		"comment", localTicketCommentSummary(comment),
		"edit_comment_id", safeInt64Pointer(req.EditCommentID),
		"attachment_guids", req.Attachments,
		"channel_type", safePyrusChannelType(req.Channel),
	)

	updatedTask, err := s.client.AddComment(ctx, taskID, req)
	if err != nil {
		return err
	}

	link := &pyrus.CommentLink{
		EtalonCommentID: strings.TrimSpace(comment.ID),
		PyrusTaskID:     taskID,
		Direction:       "local_to_pyrus",
		Fingerprint:     pyrusCommentFingerprint(&pyrusplugin.Comment{Text: text, CreateDate: comment.CreationDate}),
	}
	if existing != nil && existing.PyrusCommentID != nil && *existing.PyrusCommentID > 0 {
		commentID := *existing.PyrusCommentID
		link.PyrusCommentID = &commentID
		link.Fingerprint = existing.Fingerprint
	} else if outgoingComment := findOutgoingPyrusComment(updatedTask, text); outgoingComment != nil && outgoingComment.ID > 0 {
		commentID := outgoingComment.ID
		link.PyrusCommentID = &commentID
		link.Fingerprint = pyrusCommentFingerprint(outgoingComment)
	}
	if err := s.repo.UpsertCommentLink(ctx, link); err != nil {
		return err
	}

	now := time.Now()
	if strings.TrimSpace(ticketID) != "" {
		if err := s.repo.UpsertTicketLink(ctx, &pyrus.TicketLink{
			TicketID:       ticketID,
			PyrusTaskID:    taskID,
			LastOutgoingAt: &now,
		}); err != nil {
			s.log.Warn("Pyrus: не удалось обновить ticket link после отправки комментария", "ticket_id", ticketID, "task_id", taskID, "error", err)
		}
	}
	s.setSuppressTask(ctx, taskID)
	s.log.Debug("Pyrus: комментарий оператора синхронизирован", "task_id", taskID, "ticket_id", ticketID, "comment_id", comment.ID, "pyrus_comment_id", safeInt64Pointer(link.PyrusCommentID))
	return nil
}

func (s *pyrusSyncService) uploadCommentAttachments(ctx context.Context, ticketID string, comment *tickets.TicketComment) ([]string, error) {
	if s == nil || s.ticketRepo == nil || s.client == nil || comment == nil {
		return nil, nil
	}
	commentID := strings.TrimSpace(comment.ID)
	if strings.TrimSpace(ticketID) == "" || commentID == "" {
		return nil, nil
	}

	links, err := s.ticketRepo.GetTicketFileLinksByRelation(ctx, ticketID, []string{tickets.RelationTypeInlineComment})
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, nil
	}

	commentKeys := map[string]struct{}{commentID: {}}
	if value := strings.TrimSpace(comment.ServiceDeskUUID); value != "" {
		commentKeys[value] = struct{}{}
	}
	guids := make([]string, 0, len(links))
	for i := range links {
		link := links[i]
		if link.CommentUUID == nil {
			continue
		}
		commentUUID := strings.TrimSpace(*link.CommentUUID)
		if _, ok := commentKeys[commentUUID]; !ok {
			continue
		}
		asset, err := s.ticketRepo.GetFileAssetByID(ctx, link.FileID)
		if err != nil {
			return nil, err
		}
		if asset == nil {
			return nil, fmt.Errorf("не найден file_asset=%s для комментария %s", link.FileID, commentID)
		}
		content, err := s.readTicketFileAsset(asset)
		if err != nil {
			return nil, err
		}
		mimeType := strings.TrimSpace(asset.MimeType)
		if mimeType == "" {
			mimeType = http.DetectContentType(content)
		}
		s.log.Debug("Pyrus: загрузка вложения комментария в Pyrus", "ticket_id", ticketID, "comment_id", commentID, "file_id", asset.ID, "file_name", asset.OriginalName, "size", len(content))
		guid, err := s.client.UploadFile(ctx, asset.OriginalName, mimeType, content)
		if err != nil {
			return nil, err
		}
		guids = append(guids, guid)
	}
	if len(guids) == 0 {
		return nil, nil
	}
	return guids, nil
}

func (s *pyrusSyncService) readTicketFileAsset(asset *tickets.FileAsset) ([]byte, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("Pyrus sync service не настроен")
	}
	if asset == nil {
		return nil, fmt.Errorf("не передан file_asset для загрузки в Pyrus")
	}
	basePath := strings.TrimSpace(s.cfg.TicketStoragePath)
	if basePath == "" {
		return nil, fmt.Errorf("не задан TICKET_STORAGE_PATH")
	}
	absPath := filepath.Join(basePath, filepath.FromSlash(asset.StorageKey))
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать файл %s для отправки в Pyrus: %w", absPath, err)
	}
	return content, nil
}

func buildPyrusStatusCommentRequest(task *pyrusplugin.Task, localStatus string) (pyrusplugin.CommentRequest, string, error) {
	normalizedStatus := strings.TrimSpace(localStatus)
	if normalizedStatus == "" {
		return pyrusplugin.CommentRequest{}, "в событии не передан статус для синхронизации", nil
	}

	currentStatus := resolvePyrusTaskStatus(task)
	if currentStatus == normalizedStatus {
		return pyrusplugin.CommentRequest{}, "статус в Pyrus уже актуален", nil
	}

	req := pyrusplugin.CommentRequest{}
	if normalizedStatus == tickets.StatusResolved || normalizedStatus == tickets.StatusClosed {
		req.Action = "finished"
		return req, "", nil
	}

	if currentStatus == tickets.StatusResolved || currentStatus == tickets.StatusClosed {
		req.Action = "reopened"
	}

	statusField := findPyrusStatusField(task)
	if statusField == nil {
		if req.Action != "" {
			return req, "", nil
		}
		return pyrusplugin.CommentRequest{}, "", fmt.Errorf("в задаче Pyrus не найдено поле статуса для перехода в %q", normalizedStatus)
	}

	statusToken, err := mapLocalStatusToPyrusFieldValue(normalizedStatus)
	if err != nil {
		return pyrusplugin.CommentRequest{}, "", err
	}
	currentFieldValue := fieldToString(*statusField)
	if normalizePyrusFieldKey(currentFieldValue) == normalizePyrusFieldKey(statusToken) {
		if req.Action != "" {
			return req, "", nil
		}
		return pyrusplugin.CommentRequest{}, "статус поля Pyrus уже актуален", nil
	}

	update := pyrusplugin.FieldUpdateRequest{Value: statusToken}
	if statusField.ID > 0 {
		update.ID = &statusField.ID
	} else if code := strings.TrimSpace(statusField.Code); code != "" {
		update.Code = code
	} else {
		return pyrusplugin.CommentRequest{}, "", fmt.Errorf("у поля статуса Pyrus отсутствуют id и code")
	}
	req.FieldUpdates = []pyrusplugin.FieldUpdateRequest{update}
	return req, "", nil
}

func mapLocalStatusToPyrusFieldValue(localStatus string) (string, error) {
	switch strings.TrimSpace(localStatus) {
	case tickets.StatusNew:
		return "open", nil
	case tickets.StatusInProgress:
		return "in_progress", nil
	case tickets.StatusPending:
		return "pending", nil
	case tickets.StatusDeferred:
		return "deferred", nil
	case tickets.StatusResolved, tickets.StatusClosed:
		return "finished", nil
	default:
		return "", fmt.Errorf("неподдерживаемый локальный статус для Pyrus: %q", localStatus)
	}
}

func findPyrusStatusField(task *pyrusplugin.Task) *pyrusplugin.Field {
	if task == nil {
		return nil
	}
	for i := range task.Fields {
		field := task.Fields[i]
		fieldType := normalizePyrusFieldKey(field.Type)
		fieldCode := normalizePyrusFieldKey(field.Code)
		fieldName := normalizePyrusFieldKey(field.Name)
		if fieldType == "status" || fieldCode == "status" || fieldName == "status" {
			return &task.Fields[i]
		}
	}
	return nil
}

func findOutgoingPyrusComment(task *pyrusplugin.Task, text string) *pyrusplugin.Comment {
	if task == nil || len(task.Comments) == 0 {
		return nil
	}
	targetText := strings.TrimSpace(text)
	for i := len(task.Comments) - 1; i >= 0; i-- {
		comment := task.Comments[i]
		if isPyrusExtIDSystemComment(&comment, "") {
			continue
		}
		if strings.TrimSpace(comment.Text) == targetText {
			return &comment
		}
	}
	return nil
}

func findPyrusStatusComment(task *pyrusplugin.Task, action string) *pyrusplugin.Comment {
	if task == nil || len(task.Comments) == 0 {
		return nil
	}
	targetAction := normalizePyrusFieldKey(action)
	for i := len(task.Comments) - 1; i >= 0; i-- {
		comment := task.Comments[i]
		if normalizePyrusFieldKey(comment.Action) == targetAction {
			return &comment
		}
	}
	return nil
}

func findPyrusExtIDComment(task *pyrusplugin.Task, extID string) *pyrusplugin.Comment {
	if task == nil || len(task.Comments) == 0 {
		return nil
	}
	targetExtID := strings.TrimSpace(extID)
	for i := len(task.Comments) - 1; i >= 0; i-- {
		comment := task.Comments[i]
		if !isPyrusExtIDSystemComment(&comment, targetExtID) {
			continue
		}
		return &comment
	}
	return nil
}

func pyrusCommentFingerprint(comment *pyrusplugin.Comment) string {
	if comment == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strconvFormatInt(comment.ID),
		comment.CreateDate.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(comment.Text),
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func strconvFormatInt(value int64) string {
	return fmt.Sprintf("%d", value)
}

func (s *pyrusSyncService) setSuppressTask(ctx context.Context, taskID int64) {
	if s.redis == nil || taskID <= 0 {
		return
	}
	ttl := s.cfg.PyrusSuppressTTL
	if ttl <= 0 {
		ttl = 20 * time.Second
	}
	if err := s.redis.Set(ctx, fmt.Sprintf("pyrus:suppress:task:%d", taskID), "1", ttl).Err(); err != nil {
		s.log.Warn("Pyrus: не удалось установить suppress-ключ", "task_id", taskID, "error", err)
	}
}

func (s *pyrusSyncService) maxAttempts() int {
	if s.cfg.PyrusIncomingMaxAttempts <= 0 {
		return 10
	}
	return s.cfg.PyrusIncomingMaxAttempts
}

func (s *pyrusSyncService) retryBaseDelay() time.Duration {
	if s.cfg.PyrusIncomingRetryBase <= 0 {
		return 500 * time.Millisecond
	}
	return s.cfg.PyrusIncomingRetryBase
}

func (s *pyrusSyncService) retryMaxDelay() time.Duration {
	if s.cfg.PyrusIncomingRetryMax <= 0 {
		return 30 * time.Second
	}
	return s.cfg.PyrusIncomingRetryMax
}

func (s *pyrusSyncService) retryDelay(attempts int) time.Duration {
	if attempts <= 0 {
		return s.retryBaseDelay()
	}
	delay := s.retryBaseDelay()
	for i := 1; i < attempts; i++ {
		delay *= 2
		if delay >= s.retryMaxDelay() {
			return s.retryMaxDelay()
		}
	}
	if delay > s.retryMaxDelay() {
		return s.retryMaxDelay()
	}
	return delay
}

func (s *pyrusSyncService) shouldProcessOutgoingNow(item *pyrus.OutgoingEvent) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Status) == pyrus.OutgoingEventStatusFailed {
		if item.Attempts >= s.maxAttempts() {
			return false
		}
		nextTryAt := item.UpdatedAt.Add(s.retryDelay(item.Attempts))
		return !time.Now().Before(nextTryAt)
	}
	return true
}
