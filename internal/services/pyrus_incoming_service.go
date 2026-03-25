package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"etalon-server/internal/core/events"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/pkg/eventbus"
	"fmt"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrPyrusWebhookUnauthorized = errors.New("неверная подпись вебхука Pyrus")
	ErrPyrusWebhookBadRequest   = errors.New("некорректный payload вебхука Pyrus")
)

type PyrusIncomingService interface {
	HandleWebhook(ctx context.Context, rawBody []byte, signature string) error
	Start(ctx context.Context)
	ReplayEvent(ctx context.Context, id string) error
}

type pyrusIncomingService struct {
	cfg           *config.Config
	log           logger.LoggerInterface
	client        pyrusAPIClient
	redis         *redis.Client
	ticketRepo    tickets.TicketRepository
	ticketService TicketService
	history       TicketHistoryWriter
	userRepo      user.Repository
	serverRepo    server.Repository
	repo          pyrus.Repository
	eventBus      eventbus.EventBus

	consumerName string
	taskLocks    keyedMutex
}

func NewPyrusIncomingService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client pyrusAPIClient,
	redisClient *redis.Client,
	ticketRepo tickets.TicketRepository,
	ticketService TicketService,
	userRepo user.Repository,
	serverRepo server.Repository,
	repo pyrus.Repository,
	eventBus eventbus.EventBus,
) PyrusIncomingService {
	host, _ := os.Hostname()
	return &pyrusIncomingService{
		cfg:           cfg,
		log:           log,
		client:        client,
		redis:         redisClient,
		ticketRepo:    ticketRepo,
		ticketService: ticketService,
		history:       NewTicketHistoryWriter(ticketRepo, log.With("component", "ticket_history_writer")),
		userRepo:      userRepo,
		serverRepo:    serverRepo,
		repo:          repo,
		eventBus:      eventBus,
		consumerName:  fmt.Sprintf("%s-%d-%s", strings.TrimSpace(host), os.Getpid(), uuid.NewString()),
	}
}

func (s *pyrusIncomingService) HandleWebhook(ctx context.Context, rawBody []byte, signature string) error {
	if s == nil || s.cfg == nil || !s.cfg.EnablePyrusGateway || !s.cfg.PyrusWebhookEnabled {
		return ErrPyrusWebhookBadRequest
	}
	if !isPyrusSignatureValid(pyrusWebhookSecret(s.cfg), rawBody, signature) {
		return ErrPyrusWebhookUnauthorized
	}
	payload, err := pyrusplugin.ParseWebhookPayload(rawBody)
	if err != nil {
		return ErrPyrusWebhookBadRequest
	}
	taskID := resolvePyrusTaskID(payload)
	if taskID <= 0 {
		return ErrPyrusWebhookBadRequest
	}

	payloadHash := sha256.Sum256(rawBody)
	hashText := hex.EncodeToString(payloadHash[:])
	event := &pyrus.IncomingEvent{
		ID:          uuid.NewString(),
		EventName:   strings.TrimSpace(payload.Event),
		PayloadHash: hashText,
		PayloadRaw:  string(rawBody),
		Status:      pyrus.IncomingEventStatusNew,
		ReceivedAt:  time.Now(),
	}
	event.PyrusTaskID = &taskID

	created, err := s.repo.InsertIncomingEventIfNotExists(ctx, event)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	if err := s.enqueueEvent(ctx, event.ID); err != nil {
		s.log.Warn("Pyrus: не удалось поставить входящее событие в очередь Redis, обработка пойдёт через Postgres", "event_id", event.ID, "error", err)
		return nil
	}
	if err := s.repo.MarkIncomingQueued(ctx, event.ID); err != nil {
		s.log.Warn("Pyrus: не удалось отметить событие как queued", "event_id", event.ID, "error", err)
	}
	return nil
}

func (s *pyrusIncomingService) ReplayEvent(ctx context.Context, id string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("Pyrus incoming service не настроен")
	}
	eventID := strings.TrimSpace(id)
	if eventID == "" {
		return fmt.Errorf("не указан event_id")
	}
	if err := s.repo.ResetIncomingEventForReplay(ctx, eventID); err != nil {
		return err
	}
	if err := s.enqueueEvent(ctx, eventID); err != nil {
		s.log.Warn("Pyrus: не удалось сразу поставить replay в Redis, событие останется в очереди Postgres", "event_id", eventID, "error", err)
		return nil
	}
	return s.repo.MarkIncomingQueued(ctx, eventID)
}

func (s *pyrusIncomingService) Start(ctx context.Context) {
	if s == nil || s.cfg == nil || !s.cfg.EnablePyrusGateway || !s.cfg.PyrusWebhookEnabled {
		if s != nil && s.log != nil {
			s.log.Info("Pyrus webhook worker отключен")
		}
		return
	}
	if s.repo == nil || s.ticketRepo == nil || s.ticketService == nil || s.serverRepo == nil {
		s.log.Warn("Pyrus webhook worker отключен: не настроены обязательные зависимости")
		return
	}
	if s.client == nil || !s.client.IsConfigured() {
		s.log.Warn("Pyrus webhook worker отключен: клиент Pyrus API не настроен")
		return
	}
	if s.redis != nil {
		if err := s.ensureConsumerGroup(ctx); err != nil {
			s.log.Error("Pyrus: не удалось подготовить consumer group", "error", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dispatchLoop(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.consumeLoop(ctx)
	}()
	if s.redis != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.claimPendingLoop(ctx)
		}()
	}

	<-ctx.Done()
	wg.Wait()
}

func (s *pyrusIncomingService) ensureConsumerGroup(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	err := s.redis.XGroupCreateMkStream(ctx, s.cfg.PyrusEventsStreamName, s.cfg.PyrusEventsConsumerGroup, "$").Err()
	if err != nil && strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return nil
	}
	return err
}

func (s *pyrusIncomingService) enqueueEvent(ctx context.Context, eventID string) error {
	if s.redis == nil {
		return errors.New("redis не настроен")
	}
	return s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: s.cfg.PyrusEventsStreamName,
		Values: map[string]any{
			"event_id": strings.TrimSpace(eventID),
			"ts":       time.Now().Unix(),
		},
	}).Err()
}

func (s *pyrusIncomingService) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.redis == nil {
			continue
		}
		items, err := s.repo.ListIncomingNewOrFailedForEnqueue(ctx, 200, s.maxAttempts())
		if err != nil {
			s.log.Error("Pyrus: не удалось получить входящие события для enqueue", "error", err)
			continue
		}
		for i := range items {
			if !s.shouldProcessIncomingNow(&items[i]) {
				continue
			}
			if err := s.enqueueEvent(ctx, items[i].ID); err != nil {
				continue
			}
			_ = s.repo.MarkIncomingQueued(ctx, items[i].ID)
		}
	}
}

func (s *pyrusIncomingService) consumeLoop(ctx context.Context) {
	if s.redis == nil {
		s.consumeFromPostgresLoop(ctx)
		return
	}
	parallelism := s.cfg.PyrusIncomingParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		default:
		}

		streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.cfg.PyrusEventsConsumerGroup,
			Consumer: s.consumerName,
			Streams:  []string{s.cfg.PyrusEventsStreamName, ">"},
			Count:    20,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			s.log.Warn("Pyrus: ошибка XREADGROUP", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				eventID := strings.TrimSpace(anyToString(msg.Values["event_id"]))
				if eventID == "" {
					_ = s.redis.XAck(ctx, s.cfg.PyrusEventsStreamName, s.cfg.PyrusEventsConsumerGroup, msg.ID).Err()
					continue
				}
				sem <- struct{}{}
				wg.Add(1)
				go func(messageID string, incomingID string) {
					defer wg.Done()
					defer func() { <-sem }()
					s.processAndAck(ctx, messageID, incomingID)
				}(msg.ID, eventID)
			}
		}
	}
}

func (s *pyrusIncomingService) consumeFromPostgresLoop(ctx context.Context) {
	parallelism := s.cfg.PyrusIncomingParallelism
	if parallelism <= 0 {
		parallelism = 4
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		items, err := s.repo.ListIncomingNewOrFailedForEnqueue(ctx, parallelism*4, s.maxAttempts())
		if err != nil {
			s.log.Error("Pyrus: не удалось получить входящие события из Postgres", "error", err)
			continue
		}
		if len(items) == 0 {
			continue
		}

		sem := make(chan struct{}, parallelism)
		var wg sync.WaitGroup
		for i := range items {
			item := items[i]
			if !s.shouldProcessIncomingNow(&item) {
				continue
			}
			sem <- struct{}{}
			wg.Add(1)
			go func(eventID string) {
				defer wg.Done()
				defer func() { <-sem }()
				s.processIncomingEvent(ctx, eventID)
			}(item.ID)
		}
		wg.Wait()
	}
}

func (s *pyrusIncomingService) claimPendingLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		pending, err := s.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: s.cfg.PyrusEventsStreamName,
			Group:  s.cfg.PyrusEventsConsumerGroup,
			Start:  "-",
			End:    "+",
			Count:  50,
			Idle:   60 * time.Second,
		}).Result()
		if err != nil || len(pending) == 0 {
			continue
		}
		for i := range pending {
			claimed, claimErr := s.redis.XClaim(ctx, &redis.XClaimArgs{
				Stream:   s.cfg.PyrusEventsStreamName,
				Group:    s.cfg.PyrusEventsConsumerGroup,
				Consumer: s.consumerName,
				MinIdle:  60 * time.Second,
				Messages: []string{pending[i].ID},
			}).Result()
			if claimErr != nil {
				continue
			}
			for _, msg := range claimed {
				eventID := strings.TrimSpace(anyToString(msg.Values["event_id"]))
				if eventID == "" {
					_ = s.redis.XAck(ctx, s.cfg.PyrusEventsStreamName, s.cfg.PyrusEventsConsumerGroup, msg.ID).Err()
					continue
				}
				s.processAndAck(ctx, msg.ID, eventID)
			}
		}
	}
}

func (s *pyrusIncomingService) processAndAck(ctx context.Context, messageID string, eventID string) {
	defer func() {
		if s.redis != nil {
			_ = s.redis.XAck(ctx, s.cfg.PyrusEventsStreamName, s.cfg.PyrusEventsConsumerGroup, messageID).Err()
		}
	}()
	s.processIncomingEvent(ctx, eventID)
}

func (s *pyrusIncomingService) processIncomingEvent(ctx context.Context, eventID string) {
	item, err := s.repo.GetIncomingEventByID(ctx, eventID)
	if err != nil || item == nil {
		return
	}
	if err := s.repo.MarkIncomingProcessing(ctx, item.ID); err != nil {
		s.log.Warn("Pyrus: не удалось отметить событие как processing", "event_id", item.ID, "error", err)
	}
	status, reason, procErr := s.handleIncomingEvent(ctx, item)
	if procErr != nil {
		_ = s.repo.MarkIncomingFailed(ctx, item.ID, procErr.Error())
		return
	}
	if status == pyrus.IncomingEventStatusIgnored {
		_ = s.repo.MarkIncomingIgnored(ctx, item.ID, reason)
		return
	}
	_ = s.repo.MarkIncomingDone(ctx, item.ID)
}

func (s *pyrusIncomingService) handleIncomingEvent(ctx context.Context, item *pyrus.IncomingEvent) (string, string, error) {
	if item == nil {
		return pyrus.IncomingEventStatusIgnored, "пустое событие", nil
	}
	if s.cfg.PyrusFormID <= 0 {
		return "", "", fmt.Errorf("не задан PYRUS_FORM_ID")
	}
	payload, err := pyrusplugin.ParseWebhookPayload([]byte(item.PayloadRaw))
	if err != nil {
		return "", "", err
	}
	taskID := resolvePyrusTaskID(payload)
	if taskID <= 0 {
		return pyrus.IncomingEventStatusIgnored, "в webhook отсутствует task_id", nil
	}

	unlock := s.lockTask(taskID)
	defer unlock()

	if s.isSuppressedTask(ctx, taskID) {
		return pyrus.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}

	task, err := s.loadTask(ctx, payload, taskID)
	if err != nil {
		return "", "", err
	}
	if task == nil {
		return pyrus.IncomingEventStatusIgnored, "задача Pyrus не найдена", nil
	}
	if task.FormID != s.cfg.PyrusFormID {
		return pyrus.IncomingEventStatusIgnored, "форма не входит в поддерживаемый контур", nil
	}

	extID := strings.TrimSpace(extractPyrusFieldString(task, "ext_id"))
	if extID == "" {
		if _, err := s.createTicketFromPyrusTask(ctx, task); err != nil {
			return "", "", err
		}
		return pyrus.IncomingEventStatusDone, "", nil
	}
	if err := s.syncExistingTicketFromPyrusTask(ctx, task, extID); err != nil {
		return "", "", err
	}
	return pyrus.IncomingEventStatusDone, "", nil
}

func (s *pyrusIncomingService) loadTask(ctx context.Context, payload *pyrusplugin.WebhookPayload, taskID int64) (*pyrusplugin.Task, error) {
	if payload != nil && payload.Task.ID > 0 {
		taskCopy := payload.Task
		if taskCopy.FormID > 0 {
			return &taskCopy, nil
		}
	}
	return s.client.GetTask(ctx, taskID)
}

func (s *pyrusIncomingService) createTicketFromPyrusTask(ctx context.Context, task *pyrusplugin.Task) (*tickets.Ticket, error) {
	companyID, err := s.resolveCompanyIDByCRMID(ctx, extractPyrusFieldString(task, "CRMID", "CrmId", "crm_id"))
	if err != nil {
		return nil, err
	}

	reporterName := strings.TrimSpace(extractPyrusFieldString(task, "SenderName", "Sender Name"))
	if reporterName == "" && task.Author != nil {
		reporterName = strings.TrimSpace(task.Author.DisplayName())
	}

	// TODO: после подтверждения бизнес-контракта формы Pyrus заменить временный консервативный маппинг статусов и типа тикета.
	ticket, err := s.ticketService.CreateFromPyrus(ctx, TicketCreateFromPyrusInput{
		TaskID:       task.ID,
		CompanyID:    companyID,
		Subject:      strings.TrimSpace(extractPyrusFieldString(task, "Subject")),
		Description:  buildPyrusTicketDescription(task),
		ReporterName: reporterName,
		Status:       resolvePyrusTaskStatus(task),
		Type:         "",
	})
	if err != nil {
		return nil, err
	}

	now := time.Now()
	if err := s.repo.UpsertTicketLink(ctx, &pyrus.TicketLink{
		TicketID:       ticket.ID,
		PyrusTaskID:    task.ID,
		PyrusFormID:    task.FormID,
		LastIncomingAt: &now,
	}); err != nil {
		return nil, err
	}

	if _, err := s.syncTaskAttachments(ctx, ticket.ID, task.ID, nil, task.Attachments); err != nil {
		return nil, err
	}
	if _, err := s.syncTaskComments(ctx, ticket, task); err != nil {
		return nil, err
	}

	s.publishPyrusExtIDSyncRequested(ticket.ID, task.ID)
	s.publishTicketUpdated(ticket, "ticket_created_from_pyrus", "Создан тикет из Pyrus")
	return ticket, nil
}

func (s *pyrusIncomingService) syncExistingTicketFromPyrusTask(ctx context.Context, task *pyrusplugin.Task, extID string) error {
	ticket, err := s.ticketRepo.GetByID(ctx, extID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return fmt.Errorf("локальный тикет %s для Pyrus task_id=%d не найден", extID, task.ID)
		}
		return err
	}
	if ticket == nil {
		return fmt.Errorf("локальный тикет %s для Pyrus task_id=%d не найден", extID, task.ID)
	}

	now := time.Now()
	if err := s.repo.UpsertTicketLink(ctx, &pyrus.TicketLink{
		TicketID:       ticket.ID,
		PyrusTaskID:    task.ID,
		PyrusFormID:    task.FormID,
		LastIncomingAt: &now,
	}); err != nil {
		return err
	}

	if _, err := s.syncTaskAttachments(ctx, ticket.ID, task.ID, nil, task.Attachments); err != nil {
		return err
	}
	commentCount, err := s.syncTaskComments(ctx, ticket, task)
	if err != nil {
		return err
	}
	statusChanged, err := s.applyPyrusStatusToTicket(ctx, ticket, task)
	if err != nil {
		return err
	}
	if commentCount > 0 {
		s.publishTicketUpdated(ticket, "ticket_comment_added", "Добавлен комментарий из Pyrus")
	}
	if statusChanged {
		s.publishTicketUpdated(ticket, "ticket_status_changed", "Изменён статус из Pyrus")
	}
	return nil
}

func (s *pyrusIncomingService) syncTaskComments(ctx context.Context, ticket *tickets.Ticket, task *pyrusplugin.Task) (int, error) {
	if ticket == nil || task == nil || len(task.Comments) == 0 {
		return 0, nil
	}
	added := 0
	for i := range task.Comments {
		comment := task.Comments[i]
		if isPyrusExtIDSystemComment(&comment, ticket.ID) {
			continue
		}
		if comment.ID > 0 {
			link, err := s.repo.GetCommentLinkByPyrusCommentID(ctx, comment.ID)
			if err != nil {
				return added, err
			}
			if link != nil {
				continue
			}
		}

		localCommentID := fmt.Sprintf("pyrus-%d", comment.ID)
		if comment.ID <= 0 {
			localCommentID = "pyrus-" + pyrusCommentFingerprint(&comment)
		}
		existing, err := s.ticketRepo.GetCommentByUUID(ctx, ticket.ID, localCommentID)
		if err != nil {
			return added, err
		}
		if existing != nil {
			if err := s.upsertPyrusCommentLink(ctx, task.ID, localCommentID, &comment); err != nil {
				return added, err
			}
			continue
		}

		commentText, err := s.enrichPyrusCommentWithAttachments(ctx, ticket.ID, task.ID, localCommentID, strings.TrimSpace(comment.Text), comment.Attachments)
		if err != nil {
			return added, err
		}
		if strings.TrimSpace(commentText) == "" {
			continue
		}

		var authorUserID *uint
		if mappedUserID, ok, err := s.resolveEtalonUserIDByPyrusComment(ctx, &comment); err != nil {
			return added, err
		} else if ok {
			authorUserID = &mappedUserID
		}

		creationDate := comment.CreateDate
		if creationDate.IsZero() {
			creationDate = time.Now()
		}
		item := tickets.TicketComment{
			ID:              localCommentID,
			TicketID:        ticket.ID,
			ServiceDeskUUID: localCommentID,
			Text:            commentText,
			AuthorName:      pyrusCommentAuthorName(&comment),
			AuthorUserID:    authorUserID,
			Source:          tickets.CommentSourcePyrus,
			CreationDate:    creationDate,
			IsInternal:      false,
			IsPrivate:       false,
		}
		if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{item}); err != nil {
			return added, err
		}
		if err := s.upsertPyrusCommentLink(ctx, task.ID, localCommentID, &comment); err != nil {
			return added, err
		}
		if s.history != nil {
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				UserID:   authorUserID,
				Action:   tickets.HistoryActionCommentAdded,
				Field:    tickets.HistoryFieldComment,
				Source:   tickets.HistorySourcePyrus,
				NewValue: commentText,
				Meta: map[string]any{
					"comment_id": item.ID,
				},
			})
		}
		added++
	}
	return added, nil
}

func (s *pyrusIncomingService) upsertPyrusCommentLink(ctx context.Context, taskID int64, localCommentID string, comment *pyrusplugin.Comment) error {
	link := &pyrus.CommentLink{
		EtalonCommentID: strings.TrimSpace(localCommentID),
		PyrusTaskID:     taskID,
		Direction:       "pyrus_to_local",
		Fingerprint:     pyrusCommentFingerprint(comment),
	}
	if comment != nil && comment.ID > 0 {
		commentID := comment.ID
		link.PyrusCommentID = &commentID
	}
	return s.repo.UpsertCommentLink(ctx, link)
}

func (s *pyrusIncomingService) syncTaskAttachments(
	ctx context.Context,
	ticketID string,
	taskID int64,
	commentID *string,
	attachments []pyrusplugin.Attachment,
) (int, error) {
	if len(attachments) == 0 {
		return 0, nil
	}
	added := 0
	for i := range attachments {
		created, _, _, err := s.persistPyrusAttachment(ctx, ticketID, taskID, commentID, &attachments[i])
		if err != nil {
			return added, err
		}
		if created {
			added++
		}
	}
	return added, nil
}

func (s *pyrusIncomingService) enrichPyrusCommentWithAttachments(
	ctx context.Context,
	ticketID string,
	taskID int64,
	localCommentID string,
	text string,
	attachments []pyrusplugin.Attachment,
) (string, error) {
	if len(attachments) == 0 {
		return text, nil
	}
	commentID := strings.TrimSpace(localCommentID)
	links := make([]string, 0, len(attachments))
	for i := range attachments {
		_, publicURL, fileName, err := s.persistPyrusAttachment(ctx, ticketID, taskID, &commentID, &attachments[i])
		if err != nil {
			return text, err
		}
		if publicURL == "" {
			continue
		}
		links = append(links, renderPyrusAttachmentHTML(publicURL, fileName))
	}
	if len(links) == 0 {
		return text, nil
	}
	attachmentBlock := "<p>Вложения из Pyrus:</p><ul><li>" + strings.Join(links, "</li><li>") + "</li></ul>"
	if strings.TrimSpace(text) == "" {
		return attachmentBlock, nil
	}
	return strings.TrimSpace(text) + "\n" + attachmentBlock, nil
}

func (s *pyrusIncomingService) persistPyrusAttachment(
	ctx context.Context,
	ticketID string,
	taskID int64,
	commentID *string,
	attachment *pyrusplugin.Attachment,
) (bool, string, string, error) {
	if attachment == nil {
		return false, "", "", nil
	}
	if attachment.ID <= 0 {
		return false, "", "", fmt.Errorf("во вложении Pyrus отсутствует attachment.id")
	}

	existingLink, err := s.repo.GetFileLinkByPyrusAttachmentID(ctx, attachment.ID)
	if err != nil {
		return false, "", "", err
	}
	if existingLink != nil {
		asset, assetErr := s.ticketRepo.GetFileAssetByID(ctx, existingLink.LocalFileID)
		if assetErr != nil {
			return false, "", "", assetErr
		}
		if asset != nil {
			return false, "/api/static/tickets/" + asset.StorageKey, asset.OriginalName, nil
		}
	}

	fileData, err := s.client.DownloadFile(ctx, attachment.ID)
	if err != nil {
		return false, "", "", err
	}
	if fileData == nil {
		return false, "", "", fmt.Errorf("Pyrus вернул пустой файл для attachment_id=%d", attachment.ID)
	}

	fileName := sanitizePyrusAttachmentName(attachment.Name)
	if fileName == "" {
		fileName = sanitizePyrusAttachmentName(fileData.FileName)
	}
	if fileName == "" {
		fileName = fmt.Sprintf("pyrus-%d.bin", attachment.ID)
	}

	mimeType := strings.TrimSpace(fileData.MimeType)
	if mimeType == "" {
		mimeType = http.DetectContentType(fileData.Content)
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	basePath := strings.TrimSpace(s.cfg.TicketStoragePath)
	if basePath == "" {
		return false, "", "", fmt.Errorf("не задан TICKET_STORAGE_PATH")
	}
	storageKey := filepath.ToSlash(filepath.Join(ticketID, "pyrus", fmt.Sprintf("attachment-%d-%s", attachment.ID, fileName)))
	absPath := filepath.Join(basePath, filepath.FromSlash(storageKey))
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return false, "", "", err
	}
	if err := os.WriteFile(absPath, fileData.Content, 0644); err != nil {
		return false, "", "", err
	}

	hash := sha256.Sum256(fileData.Content)
	asset, err := s.ticketRepo.UpsertFileAsset(ctx, &tickets.FileAsset{
		StorageKey:   storageKey,
		OriginalName: fileName,
		MimeType:     mimeType,
		Size:         int64(len(fileData.Content)),
		Checksum:     hex.EncodeToString(hash[:]),
	})
	if err != nil {
		return false, "", "", err
	}
	if asset == nil {
		return false, "", "", fmt.Errorf("не удалось сохранить файл Pyrus")
	}

	relationType := tickets.RelationTypeDirectTicketAttachment
	if commentID != nil && strings.TrimSpace(*commentID) != "" {
		relationType = tickets.RelationTypeInlineComment
	}
	if err := s.ticketRepo.UpsertTicketFileLink(ctx, &tickets.TicketFileLink{
		TicketID:     ticketID,
		FileID:       asset.ID,
		RelationType: relationType,
		CommentUUID:  commentID,
	}); err != nil {
		return false, "", "", err
	}

	link := &pyrus.FileLink{
		LocalFileID:       asset.ID,
		PyrusAttachmentID: &attachment.ID,
		TicketID:          ticketID,
		CommentID:         commentID,
	}
	// TODO: заполнить PyrusGUID после подтверждения стабильного поля GUID в payload/webhook Pyrus.
	if err := s.repo.UpsertFileLink(ctx, link); err != nil {
		return false, "", "", err
	}

	return true, "/api/static/tickets/" + asset.StorageKey, asset.OriginalName, nil
}

func (s *pyrusIncomingService) applyPyrusStatusToTicket(ctx context.Context, ticket *tickets.Ticket, task *pyrusplugin.Task) (bool, error) {
	if ticket == nil || task == nil {
		return false, nil
	}
	nextStatus := resolvePyrusTaskStatus(task)
	if nextStatus == "" || ticket.Status == nextStatus {
		return false, nil
	}
	oldStatus := ticket.Status
	ticket.Status = nextStatus
	ticket.LastUpdatedBy = "pyrus_webhook"
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return false, err
	}
	if s.history != nil {
		s.history.Write(ctx, TicketHistoryWriteRequest{
			TicketID: ticket.ID,
			Action:   tickets.HistoryActionFieldChanged,
			Field:    tickets.HistoryFieldStatus,
			Source:   tickets.HistorySourcePyrus,
			OldValue: oldStatus,
			NewValue: nextStatus,
			Meta: map[string]any{
				"pyrus_task_id": task.ID,
			},
		})
	}
	return true, nil
}

func (s *pyrusIncomingService) resolveCompanyIDByCRMID(ctx context.Context, crmID string) (string, error) {
	normalizedCRMID := strings.TrimSpace(crmID)
	if normalizedCRMID == "" {
		return "", fmt.Errorf("в задаче Pyrus отсутствует CRMID")
	}
	servers, err := s.serverRepo.ListByCRMid(ctx, normalizedCRMID)
	if err != nil {
		return "", err
	}
	ownerIDs := make([]string, 0, len(servers))
	seen := make(map[string]struct{}, len(servers))
	for i := range servers {
		if servers[i].OwnerID == nil {
			continue
		}
		ownerID := strings.TrimSpace(*servers[i].OwnerID)
		if ownerID == "" {
			continue
		}
		if _, exists := seen[ownerID]; exists {
			continue
		}
		seen[ownerID] = struct{}{}
		ownerIDs = append(ownerIDs, ownerID)
	}
	switch len(ownerIDs) {
	case 0:
		return "", fmt.Errorf("по CRMID=%s не найден однозначный owner_id", normalizedCRMID)
	case 1:
		return ownerIDs[0], nil
	default:
		return "", fmt.Errorf("по CRMID=%s найдено несколько owner_id", normalizedCRMID)
	}
}

func (s *pyrusIncomingService) resolveEtalonUserIDByPyrusComment(ctx context.Context, comment *pyrusplugin.Comment) (uint, bool, error) {
	if comment == nil || comment.Author == nil || comment.Author.ID <= 0 {
		return 0, false, nil
	}
	return s.resolveEtalonUserIDByPyrusUserID(ctx, comment.Author.ID)
}

func (s *pyrusIncomingService) resolveEtalonUserIDByPyrusUserID(ctx context.Context, pyrusUserID int64) (uint, bool, error) {
	if pyrusUserID <= 0 {
		return 0, false, nil
	}
	if mapped, err := s.repo.GetUserMapByPyrusID(ctx, pyrusUserID); err != nil {
		return 0, false, err
	} else if mapped != nil {
		return mapped.EtalonUserID, true, nil
	}

	targetExternalID := fmt.Sprintf("%d", pyrusUserID)
	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return 0, false, err
	}
	matches := make([]uint, 0, 1)
	for i := range users {
		u := users[i]
		if u.ExternalType != nil && u.ExternalID != nil &&
			strings.TrimSpace(strings.ToLower(*u.ExternalType)) == user.ExternalTypePyrus &&
			strings.TrimSpace(*u.ExternalID) == targetExternalID {
			matches = append(matches, u.ID)
			continue
		}
		for j := range u.Integrations {
			integration := u.Integrations[j]
			if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypePyrus {
				continue
			}
			if strings.TrimSpace(integration.ExternalID) != targetExternalID {
				continue
			}
			if !integration.IsVerified && !integration.IsLocked {
				continue
			}
			matches = append(matches, u.ID)
			break
		}
	}
	if len(matches) == 0 {
		return 0, false, nil
	}
	uniqueMatches := make([]uint, 0, len(matches))
	seen := make(map[uint]struct{}, len(matches))
	for _, id := range matches {
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueMatches = append(uniqueMatches, id)
	}
	if len(uniqueMatches) != 1 {
		return 0, false, fmt.Errorf("для pyrus_user_id=%d найдено несколько локальных пользователей", pyrusUserID)
	}
	_ = s.repo.UpsertUserMap(ctx, &pyrus.UserMap{
		EtalonUserID: uniqueMatches[0],
		PyrusUserID:  pyrusUserID,
	})
	return uniqueMatches[0], true, nil
}

func (s *pyrusIncomingService) publishPyrusExtIDSyncRequested(ticketID string, taskID int64) {
	if s.eventBus == nil || strings.TrimSpace(ticketID) == "" || taskID <= 0 {
		return
	}
	s.eventBus.Publish(eventbus.Event{
		Type: events.PyrusTicketExtIDSyncRequested,
		Payload: events.PyrusSyncEntityPayload{
			TicketID: ticketID,
			TaskID:   taskID,
			ExtID:    ticketID,
			Reason:   "ticket_created_from_pyrus",
		},
	})
}

func (s *pyrusIncomingService) publishTicketUpdated(ticket *tickets.Ticket, action string, message string) {
	if s.eventBus == nil || ticket == nil || strings.TrimSpace(ticket.ID) == "" {
		return
	}
	var recipientUserID *uint
	if ticket.AssigneeID != nil && *ticket.AssigneeID > 0 {
		recipient := *ticket.AssigneeID
		recipientUserID = &recipient
	}
	s.eventBus.Publish(eventbus.Event{
		Type: events.TicketUpdated,
		Payload: events.TicketUpdatedPayload{
			TicketID:        ticket.ID,
			Action:          strings.TrimSpace(action),
			Source:          tickets.HistorySourcePyrus,
			Message:         strings.TrimSpace(message),
			OccurredAt:      time.Now(),
			RecipientUserID: recipientUserID,
		},
	})
}

func (s *pyrusIncomingService) lockTask(taskID int64) func() {
	return s.taskLocks.Lock(taskID)
}

func (s *pyrusIncomingService) isSuppressedTask(ctx context.Context, taskID int64) bool {
	if s.redis == nil || taskID <= 0 {
		return false
	}
	ok, err := s.redis.Exists(ctx, fmt.Sprintf("pyrus:suppress:task:%d", taskID)).Result()
	if err != nil || ok == 0 {
		return false
	}
	return true
}

func (s *pyrusIncomingService) maxAttempts() int {
	if s.cfg.PyrusIncomingMaxAttempts <= 0 {
		return 10
	}
	return s.cfg.PyrusIncomingMaxAttempts
}

func (s *pyrusIncomingService) retryBaseDelay() time.Duration {
	if s.cfg.PyrusIncomingRetryBase <= 0 {
		return 500 * time.Millisecond
	}
	return s.cfg.PyrusIncomingRetryBase
}

func (s *pyrusIncomingService) retryMaxDelay() time.Duration {
	if s.cfg.PyrusIncomingRetryMax <= 0 {
		return 30 * time.Second
	}
	return s.cfg.PyrusIncomingRetryMax
}

func (s *pyrusIncomingService) retryDelay(attempts int) time.Duration {
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

func (s *pyrusIncomingService) shouldProcessIncomingNow(item *pyrus.IncomingEvent) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Status) == pyrus.IncomingEventStatusFailed {
		if item.Attempts >= s.maxAttempts() {
			return false
		}
		nextTryAt := item.UpdatedAt.Add(s.retryDelay(item.Attempts))
		return !time.Now().Before(nextTryAt)
	}
	return true
}

func sanitizePyrusAttachmentName(name string) string {
	value := strings.TrimSpace(name)
	if value == "" {
		return ""
	}
	value = filepath.Base(value)
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "\\", "_")
	return strings.TrimSpace(value)
}

func renderPyrusAttachmentHTML(publicURL string, fileName string) string {
	safeURL := html.EscapeString(strings.TrimSpace(publicURL))
	safeName := html.EscapeString(strings.TrimSpace(fileName))
	if safeName == "" {
		safeName = "Файл из Pyrus"
	}
	return fmt.Sprintf(`<a href="%s" target="_blank" rel="noopener noreferrer">%s</a>`, safeURL, safeName)
}
