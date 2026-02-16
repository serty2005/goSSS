package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"etalon-server/internal/core/events"
	domain "etalon-server/internal/domain"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/pkg/eventbus"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrBitrixWebhookUnauthorized = errors.New("неверный application_token вебхука Bitrix24")
	ErrBitrixWebhookBadRequest   = errors.New("некорректный payload вебхука Bitrix24")
)

var bitrixAllowedIncomingEvents = map[string]struct{}{
	"ONCRMDEALADD":               {},
	"ONCRMDEALUPDATE":            {},
	"ONCRMDEALDELETE":            {},
	"ONCRMTIMELINECOMMENTADD":    {},
	"ONCRMTIMELINECOMMENTUPDATE": {},
	"ONCRMTIMELINECOMMENTDELETE": {},
}

type BitrixIncomingService interface {
	HandleWebhook(ctx context.Context, rawBody []byte, form url.Values) error
	Start(ctx context.Context)
}

type bitrixIncomingService struct {
	cfg        *config.Config
	log        logger.LoggerInterface
	client     *b24.Client
	redis      *redis.Client
	ticketRepo tickets.TicketRepository
	history    TicketHistoryWriter
	userRepo   user.Repository
	repo       bitrix.Repository
	eventBus   eventbus.EventBus

	consumerName string
}

func NewBitrixIncomingService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client *b24.Client,
	redisClient *redis.Client,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
	repo bitrix.Repository,
	eventBus eventbus.EventBus,
) BitrixIncomingService {
	host, _ := os.Hostname()
	consumer := fmt.Sprintf("%s-%d-%s", strings.TrimSpace(host), os.Getpid(), uuid.NewString())
	return &bitrixIncomingService{
		cfg:          cfg,
		log:          log,
		client:       client,
		redis:        redisClient,
		ticketRepo:   ticketRepo,
		history:      NewTicketHistoryWriter(ticketRepo, log.With("component", "ticket_history_writer")),
		userRepo:     userRepo,
		repo:         repo,
		eventBus:     eventBus,
		consumerName: consumer,
	}
}

func (s *bitrixIncomingService) HandleWebhook(ctx context.Context, rawBody []byte, form url.Values) error {
	if !s.cfg.EnableBitrixGateway || !s.cfg.BitrixWebhookEnabled {
		return ErrBitrixWebhookBadRequest
	}
	token := strings.TrimSpace(form.Get("auth[application_token]"))
	if token == "" || token != strings.TrimSpace(s.cfg.BitrixWebhookAppToken) {
		return ErrBitrixWebhookUnauthorized
	}
	eventName := strings.ToUpper(strings.TrimSpace(form.Get("event")))
	if eventName == "" {
		return ErrBitrixWebhookBadRequest
	}

	payloadHash := sha256.Sum256(rawBody)
	hashText := hex.EncodeToString(payloadHash[:])
	entityID := strings.TrimSpace(form.Get("data[FIELDS][ID]"))

	event := &bitrix.IncomingEvent{
		ID:          uuid.NewString(),
		EventName:   eventName,
		PayloadRaw:  string(rawBody),
		PayloadHash: hashText,
		Status:      bitrix.IncomingEventStatusNew,
		ReceivedAt:  time.Now(),
	}
	if entityID != "" {
		event.EntityID = &entityID
	}
	if ts, ok := parseInt64(strings.TrimSpace(form.Get("ts"))); ok {
		event.EventTS = &ts
	}
	if handlerID, ok := parseInt64(strings.TrimSpace(form.Get("event_handler_id"))); ok {
		event.EventHandlerID = &handlerID
	}

	created, err := s.repo.InsertIfNotExistsByHash(ctx, event)
	if err != nil {
		return err
	}
	if !created {
		return nil
	}
	if err := s.enqueueEvent(ctx, event.ID); err != nil {
		s.log.Warn("Bitrix24: не удалось поставить событие в Redis Streams, оставляем в Postgres", "event_id", event.ID, "error", err)
		return nil
	}
	if err := s.repo.MarkQueued(ctx, event.ID); err != nil {
		s.log.Warn("Bitrix24: не удалось отметить событие как queued", "event_id", event.ID, "error", err)
	}
	return nil
}

func (s *bitrixIncomingService) Start(ctx context.Context) {
	if !s.cfg.EnableBitrixGateway || !s.cfg.BitrixWebhookEnabled {
		s.log.Info("Bitrix24 webhook worker отключен")
		return
	}
	if s.client == nil || !s.client.IsConfigured() {
		s.log.Warn("Bitrix24 webhook worker отключен: клиент Bitrix24 не настроен")
		return
	}

	if s.redis != nil {
		if err := s.ensureConsumerGroup(ctx); err != nil {
			s.log.Error("Bitrix24 webhook worker: не удалось создать consumer group", "error", err)
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
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.claimPendingLoop(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
}

func (s *bitrixIncomingService) ensureConsumerGroup(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}
	err := s.redis.XGroupCreateMkStream(ctx, s.cfg.BitrixEventsStreamName, s.cfg.BitrixEventsConsumerGroup, "$").Err()
	if err != nil && strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return nil
	}
	return err
}

func (s *bitrixIncomingService) enqueueEvent(ctx context.Context, eventID string) error {
	if s.redis == nil {
		return errors.New("redis не настроен")
	}
	return s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: s.cfg.BitrixEventsStreamName,
		Values: map[string]interface{}{"event_id": eventID, "ts": time.Now().Unix()},
	}).Err()
}

func (s *bitrixIncomingService) dispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items, err := s.repo.ListNewOrFailedForEnqueue(ctx, 200, s.maxAttempts())
			if err != nil {
				s.log.Error("Bitrix24: ошибка выборки новых/failed событий", "error", err)
				continue
			}
			for i := range items {
				if !s.shouldEnqueueNow(&items[i]) {
					continue
				}
				if err := s.enqueueEvent(ctx, items[i].ID); err != nil {
					continue
				}
				_ = s.repo.MarkQueued(ctx, items[i].ID)
			}
		}
	}
}

func (s *bitrixIncomingService) consumeLoop(ctx context.Context) {
	parallelism := s.cfg.BitrixIncomingParallelism
	if parallelism <= 0 {
		parallelism = 8
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
		if s.redis == nil {
			time.Sleep(3 * time.Second)
			continue
		}

		streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    s.cfg.BitrixEventsConsumerGroup,
			Consumer: s.consumerName,
			Streams:  []string{s.cfg.BitrixEventsStreamName, ">"},
			Count:    20,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			s.log.Warn("Bitrix24: ошибка XREADGROUP", "error", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				eventID := strings.TrimSpace(toString(msg.Values["event_id"]))
				if eventID == "" {
					_ = s.redis.XAck(ctx, s.cfg.BitrixEventsStreamName, s.cfg.BitrixEventsConsumerGroup, msg.ID).Err()
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

func (s *bitrixIncomingService) claimPendingLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.redis == nil {
				continue
			}
			pending, err := s.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
				Stream: s.cfg.BitrixEventsStreamName,
				Group:  s.cfg.BitrixEventsConsumerGroup,
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
					Stream:   s.cfg.BitrixEventsStreamName,
					Group:    s.cfg.BitrixEventsConsumerGroup,
					Consumer: s.consumerName,
					MinIdle:  60 * time.Second,
					Messages: []string{pending[i].ID},
				}).Result()
				if claimErr != nil {
					continue
				}
				for _, msg := range claimed {
					eventID := strings.TrimSpace(toString(msg.Values["event_id"]))
					if eventID == "" {
						_ = s.redis.XAck(ctx, s.cfg.BitrixEventsStreamName, s.cfg.BitrixEventsConsumerGroup, msg.ID).Err()
						continue
					}
					s.processAndAck(ctx, msg.ID, eventID)
				}
			}
		}
	}
}

func (s *bitrixIncomingService) processAndAck(ctx context.Context, messageID string, eventID string) {
	defer func() {
		if s.redis != nil {
			_ = s.redis.XAck(ctx, s.cfg.BitrixEventsStreamName, s.cfg.BitrixEventsConsumerGroup, messageID).Err()
		}
	}()

	item, err := s.repo.GetByID(ctx, eventID)
	if err != nil || item == nil {
		return
	}
	_ = s.repo.MarkProcessing(ctx, eventID)

	status, reason, procErr := s.handleIncomingEvent(ctx, item)
	if procErr != nil {
		if s.shouldApplyRetryBackoff(item) {
			delay := s.retryDelay(item.Attempts)
			select {
			case <-ctx.Done():
			case <-time.After(delay):
			}
		}
		_ = s.repo.MarkFailed(ctx, eventID, procErr.Error())
		return
	}
	if status == bitrix.IncomingEventStatusIgnored {
		s.log.Info(
			"Bitrix24: входящее событие проигнорировано",
			"event_id", item.ID,
			"event_name", item.EventName,
			"entity_id", safeEntityID(item.EntityID),
			"reason", reason,
		)
		_ = s.repo.MarkIgnored(ctx, eventID, reason)
		return
	}
	_ = s.repo.MarkDone(ctx, eventID)
}

func (s *bitrixIncomingService) handleIncomingEvent(ctx context.Context, item *bitrix.IncomingEvent) (string, string, error) {
	eventName := strings.ToUpper(strings.TrimSpace(item.EventName))
	if _, ok := bitrixAllowedIncomingEvents[eventName]; !ok {
		return bitrix.IncomingEventStatusIgnored, "событие не входит в поддерживаемый набор", nil
	}
	if item.EntityID == nil || strings.TrimSpace(*item.EntityID) == "" {
		return bitrix.IncomingEventStatusIgnored, "в payload отсутствует data[FIELDS][ID]", nil
	}
	id, ok := parseInt64(strings.TrimSpace(*item.EntityID))
	if !ok || id <= 0 {
		return bitrix.IncomingEventStatusIgnored, "некорректный идентификатор сущности", nil
	}

	switch eventName {
	case "ONCRMDEALADD", "ONCRMDEALUPDATE":
		return s.handleDealAddOrUpdate(ctx, id)
	case "ONCRMDEALDELETE":
		return s.handleDealDelete(ctx, id)
	case "ONCRMTIMELINECOMMENTADD":
		return s.handleTimelineCommentAdd(ctx, id)
	case "ONCRMTIMELINECOMMENTUPDATE":
		return s.handleTimelineCommentUpdate(ctx, id)
	case "ONCRMTIMELINECOMMENTDELETE":
		return s.handleTimelineCommentDelete(ctx, id)
	default:
		return bitrix.IncomingEventStatusIgnored, "событие не поддерживается", nil
	}
}

func (s *bitrixIncomingService) handleDealAddOrUpdate(ctx context.Context, dealID int64) (string, string, error) {
	if s.isSuppressedDeal(ctx, dealID) {
		return bitrix.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}
	deal, err := s.client.DealGet(ctx, dealID)
	if err != nil {
		return "", "", err
	}
	if deal == nil {
		return bitrix.IncomingEventStatusIgnored, "сделка не найдена", nil
	}
	if !s.isOurDeal(deal) {
		return bitrix.IncomingEventStatusIgnored, "сделка не относится к интеграции", nil
	}

	ticket, created, err := s.resolveOrCreateTicketForDeal(ctx, deal)
	if err != nil {
		return "", "", err
	}
	if ticket == nil {
		return bitrix.IncomingEventStatusIgnored, "локальный тикет для сделки не определен", nil
	}
	if ticket.IsArchived {
		return bitrix.IncomingEventStatusIgnored, "тикет находится в архиве", nil
	}

	if err = s.repo.UpsertDealLink(ctx, &bitrix.DealLink{TicketID: ticket.ID, B24DealID: deal.ID, LastSyncAt: time.Now()}); err != nil {
		return "", "", err
	}
	if err = s.applyDealSnapshotToTicket(ctx, ticket, deal); err != nil {
		return "", "", err
	}
	if created {
		if err = s.syncAllTimelineCommentsForDeal(ctx, ticket); err != nil {
			return "", "", err
		}
		s.publishTicketUpdated(ticket.ID, "ticket_created_from_bitrix", "bitrix", "Создан тикет из сделки Bitrix24")
	} else {
		s.publishTicketUpdated(ticket.ID, "ticket_updated_from_bitrix", "bitrix", "Обновлён тикет из сделки Bitrix24")
	}
	return bitrix.IncomingEventStatusDone, "", nil
}
func (s *bitrixIncomingService) handleDealDelete(ctx context.Context, dealID int64) (string, string, error) {
	if s.isSuppressedDeal(ctx, dealID) {
		return bitrix.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}
	link, err := s.repo.GetDealLinkByDealID(ctx, dealID)
	if err != nil {
		return "", "", err
	}
	if link == nil {
		return bitrix.IncomingEventStatusIgnored, "deal_link не найден", nil
	}
	if err := s.repo.DeleteDealLinkByTicketID(ctx, link.TicketID); err != nil {
		return "", "", err
	}
	s.publishTicketUpdated(link.TicketID, "ticket_bitrix_link_deleted", "bitrix", "Удалена связь тикета со сделкой Bitrix24")
	return bitrix.IncomingEventStatusDone, "", nil
}

func (s *bitrixIncomingService) handleTimelineCommentAdd(ctx context.Context, commentID int64) (string, string, error) {
	if s.isSuppressedComment(ctx, commentID) {
		return bitrix.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}
	comment, ticket, reason, err := s.resolveCommentAndTicket(ctx, commentID)
	if err != nil {
		return "", "", err
	}
	if comment == nil || ticket == nil {
		return bitrix.IncomingEventStatusIgnored, reason, nil
	}
	if ticket.IsArchived {
		return bitrix.IncomingEventStatusIgnored, "тикет находится в архиве", nil
	}
	if isClosedTicketStatus(ticket.Status) {
		return bitrix.IncomingEventStatusIgnored, "тикет закрыт/решён, импорт комментария пропущен", nil
	}
	if err := s.addOrUpdateCommentFromBitrix(ctx, ticket, comment, false); err != nil {
		return "", "", err
	}
	s.publishTicketUpdated(ticket.ID, "ticket_comment_added", "bitrix", "Добавлен комментарий из Bitrix24")
	return bitrix.IncomingEventStatusDone, "", nil
}

func (s *bitrixIncomingService) handleTimelineCommentUpdate(ctx context.Context, commentID int64) (string, string, error) {
	if s.isSuppressedComment(ctx, commentID) {
		return bitrix.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}
	comment, ticket, reason, err := s.resolveCommentAndTicket(ctx, commentID)
	if err != nil {
		return "", "", err
	}
	if comment == nil || ticket == nil {
		return bitrix.IncomingEventStatusIgnored, reason, nil
	}
	if ticket.IsArchived {
		return bitrix.IncomingEventStatusIgnored, "тикет находится в архиве", nil
	}
	if isClosedTicketStatus(ticket.Status) {
		return bitrix.IncomingEventStatusIgnored, "тикет закрыт/решён, обновление комментария пропущено", nil
	}
	if err := s.addOrUpdateCommentFromBitrix(ctx, ticket, comment, true); err != nil {
		return "", "", err
	}
	s.publishTicketUpdated(ticket.ID, "ticket_comment_updated", "bitrix", "Обновлён комментарий из Bitrix24")
	return bitrix.IncomingEventStatusDone, "", nil
}

func (s *bitrixIncomingService) handleTimelineCommentDelete(ctx context.Context, commentID int64) (string, string, error) {
	if s.isSuppressedComment(ctx, commentID) {
		return bitrix.IncomingEventStatusIgnored, "подавлено anti-loop ключом", nil
	}
	link, err := s.repo.GetCommentLinkByB24ID(ctx, commentID)
	if err != nil {
		return "", "", err
	}
	if link == nil {
		return bitrix.IncomingEventStatusIgnored, "comment_link не найден", nil
	}
	if err := s.ticketRepo.MarkCommentDeletedInBitrix(ctx, link.EtalonCommentID, time.Now()); err != nil {
		return "", "", err
	}
	if s.history != nil {
		s.history.Write(ctx, TicketHistoryWriteRequest{
			TicketID: link.TicketID,
			Action:   tickets.HistoryActionCommentDeleted,
			Field:    tickets.HistoryFieldComment,
			Source:   tickets.HistorySourceBitrix,
			Meta: map[string]interface{}{
				"comment_id":        link.EtalonCommentID,
				"bitrix_comment_id": commentID,
			},
		})
	}
	s.publishTicketUpdated(link.TicketID, "ticket_comment_deleted", "bitrix", "Комментарий удалён в Bitrix24")
	return bitrix.IncomingEventStatusDone, "", nil
}

func (s *bitrixIncomingService) resolveCommentAndTicket(ctx context.Context, commentID int64) (*b24.TimelineComment, *tickets.Ticket, string, error) {
	comment, err := s.client.TimelineCommentGet(ctx, commentID)
	if err != nil {
		return nil, nil, "", err
	}
	if comment == nil {
		return nil, nil, "комментарий не найден", nil
	}
	if strings.TrimSpace(strings.ToLower(comment.EntityType)) != "deal" || comment.EntityID <= 0 {
		return nil, nil, "комментарий не относится к сделке", nil
	}
	ticket, err := s.resolveTicketByDealID(ctx, comment.EntityID)
	if err != nil {
		return nil, nil, "", err
	}
	if ticket == nil {
		return nil, nil, "локальный тикет для комментария не найден", nil
	}
	return comment, ticket, "", nil
}

func (s *bitrixIncomingService) addOrUpdateCommentFromBitrix(ctx context.Context, ticket *tickets.Ticket, comment *b24.TimelineComment, forceUpdate bool) error {
	if ticket == nil || comment == nil {
		return nil
	}
	commentText, extractedAuthorID := normalizeBitrixCommentForEtalon(comment.Comment, comment.AuthorID, s.cfg.BitrixIntegrationUserID)
	authorSourceID := comment.AuthorID
	if extractedAuthorID != nil {
		authorSourceID = extractedAuthorID
	}
	authorName := s.resolveBitrixAuthorName(ctx, authorSourceID)
	link, err := s.repo.GetCommentLinkByB24ID(ctx, comment.ID)
	if err != nil {
		return err
	}
	commentLocalID := fmt.Sprintf("b24-%d", comment.ID)
	if link != nil {
		commentLocalID = link.EtalonCommentID
	}
	oldText := ""
	existingComments, _ := s.ticketRepo.GetComments(ctx, ticket.ID)
	for i := range existingComments {
		if strings.TrimSpace(existingComments[i].ID) == commentLocalID {
			oldText = existingComments[i].Text
			break
		}
	}
	if link != nil || forceUpdate {
		if err := s.ticketRepo.UpdateCommentFromBitrix(ctx, commentLocalID, commentText, authorName); err == nil {
			if s.history != nil && oldText != commentText {
				s.history.Write(ctx, TicketHistoryWriteRequest{
					TicketID: ticket.ID,
					Action:   tickets.HistoryActionCommentUpdated,
					Field:    tickets.HistoryFieldComment,
					Source:   tickets.HistorySourceBitrix,
					OldValue: oldText,
					NewValue: commentText,
					Meta: map[string]interface{}{
						"comment_id":        commentLocalID,
						"bitrix_comment_id": comment.ID,
					},
				})
			}
			return nil
		}
	}

	if link == nil {
		newComment := tickets.TicketComment{
			ID:              commentLocalID,
			TicketID:        ticket.ID,
			ServiceDeskUUID: commentLocalID,
			Text:            commentText,
			AuthorName:      authorName,
			CreationDate:    time.Now(),
			IsInternal:      false,
			IsPrivate:       false,
		}
		if err := s.ticketRepo.AddComments(ctx, []tickets.TicketComment{newComment}); err != nil {
			return err
		}
		if s.history != nil {
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				Action:   tickets.HistoryActionCommentAdded,
				Field:    tickets.HistoryFieldComment,
				Source:   tickets.HistorySourceBitrix,
				NewValue: commentText,
				Meta: map[string]interface{}{
					"comment_id":        commentLocalID,
					"bitrix_comment_id": comment.ID,
				},
			})
		}
		return s.repo.UpsertCommentLink(ctx, &bitrix.CommentLink{
			EtalonCommentID: commentLocalID,
			B24CommentID:    comment.ID,
			TicketID:        ticket.ID,
			Direction:       "b24_to_etalon",
		})
	}

	if err := s.ticketRepo.UpdateCommentFromBitrix(ctx, commentLocalID, commentText, authorName); err != nil {
		return err
	}
	if s.history != nil && oldText != commentText {
		s.history.Write(ctx, TicketHistoryWriteRequest{
			TicketID: ticket.ID,
			Action:   tickets.HistoryActionCommentUpdated,
			Field:    tickets.HistoryFieldComment,
			Source:   tickets.HistorySourceBitrix,
			OldValue: oldText,
			NewValue: commentText,
			Meta: map[string]interface{}{
				"comment_id":        commentLocalID,
				"bitrix_comment_id": comment.ID,
			},
		})
	}
	return nil
}

func (s *bitrixIncomingService) resolveTicketByDealID(ctx context.Context, dealID int64) (*tickets.Ticket, error) {
	link, err := s.repo.GetDealLinkByDealID(ctx, dealID)
	if err != nil {
		return nil, err
	}
	if link != nil {
		ticket, getErr := s.ticketRepo.GetByID(ctx, link.TicketID)
		if getErr != nil && !errors.Is(getErr, domain.ErrNotFound) {
			return nil, getErr
		}
		if ticket != nil {
			return ticket, nil
		}
	}

	deal, err := s.client.DealGet(ctx, dealID)
	if err != nil || deal == nil {
		return nil, err
	}
	if !s.isOurDeal(deal) {
		return nil, nil
	}

	ticket, created, err := s.resolveOrCreateTicketForDeal(ctx, deal)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, nil
	}
	_ = s.repo.UpsertDealLink(ctx, &bitrix.DealLink{TicketID: ticket.ID, B24DealID: deal.ID, LastSyncAt: time.Now()})
	if created {
		if err = s.syncAllTimelineCommentsForDeal(ctx, ticket); err != nil {
			return nil, err
		}
	}
	return ticket, nil
}
func (s *bitrixIncomingService) resolveBitrixAuthorName(ctx context.Context, authorID *int64) string {
	if authorID == nil || *authorID <= 0 {
		return "Сотрудник Bitrix24"
	}
	if userMap, _ := s.repo.GetUserMapByB24ID(ctx, *authorID); userMap != nil {
		if u, _ := s.userRepo.GetByID(ctx, userMap.EtalonUserID); u != nil && strings.TrimSpace(u.FullName) != "" {
			return strings.TrimSpace(u.FullName)
		}
	}
	return "Bitrix24 #" + strconv.FormatInt(*authorID, 10)
}

func (s *bitrixIncomingService) isOurDeal(deal *b24.Deal) bool {
	if deal == nil {
		return false
	}
	if deal.CategoryID != s.cfg.BitrixCategoryID {
		return false
	}
	originator := strings.TrimSpace(deal.Originator)
	if originator == "" {
		return true
	}
	return originator == strings.TrimSpace(s.cfg.BitrixOriginatorID)
}

func (s *bitrixIncomingService) resolveOrCreateTicketForDeal(ctx context.Context, deal *b24.Deal) (*tickets.Ticket, bool, error) {
	if deal == nil {
		return nil, false, nil
	}
	originID := strings.TrimSpace(deal.OriginID)
	if originID != "" {
		existing, err := s.ticketRepo.GetByID(ctx, originID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return nil, false, err
		}
		if existing != nil {
			return existing, false, nil
		}
		return s.createTicketFromDeal(ctx, deal, originID)
	}

	link, err := s.repo.GetDealLinkByDealID(ctx, deal.ID)
	if err != nil {
		return nil, false, err
	}
	if link != nil {
		existing, getErr := s.ticketRepo.GetByID(ctx, link.TicketID)
		if getErr != nil && !errors.Is(getErr, domain.ErrNotFound) {
			return nil, false, getErr
		}
		if existing != nil {
			return existing, false, nil
		}
	}

	sdUUID := fmt.Sprintf("b24:deal:%d", deal.ID)
	byUUID, err := s.ticketRepo.GetByServiceDeskUUID(ctx, sdUUID)
	if err != nil {
		return nil, false, err
	}
	if byUUID != nil {
		return byUUID, false, nil
	}
	return s.createTicketFromDeal(ctx, deal, "")
}

func (s *bitrixIncomingService) createTicketFromDeal(ctx context.Context, deal *b24.Deal, forcedID string) (*tickets.Ticket, bool, error) {
	if deal == nil {
		return nil, false, nil
	}
	subject := strings.TrimSpace(deal.Title)
	if subject == "" {
		subject = fmt.Sprintf("Сделка Bitrix24 #%d", deal.ID)
	}
	description := convertBitrixDescriptionForEtalon(toString(deal.Raw["UF_CRM_1766060620"]))
	status := mapStageToTicketStatus(deal.StageID)
	if status == "" {
		status = tickets.StatusNew
	}
	pointID := int64FromAny(deal.Raw["UF_CRM_1766062398"])

	ticket := &tickets.Ticket{
		Subject:         subject,
		Description:     description,
		Status:          status,
		Priority:        tickets.PriorityMedium,
		Type:            tickets.TypeIncident,
		ServiceDeskUUID: fmt.Sprintf("b24:deal:%d", deal.ID),
		SyncWithBitrix:  true,
		BitrixDealTitle: subject,
		ReporterName:    "Bitrix24",
	}
	if forcedID != "" {
		ticket.ID = forcedID
	}
	if pointID > 0 {
		ticket.BitrixServicePointID = &pointID
	}
	if err := s.ticketRepo.Create(ctx, ticket); err != nil {
		return nil, false, err
	}
	return ticket, true, nil
}

func (s *bitrixIncomingService) applyDealSnapshotToTicket(ctx context.Context, ticket *tickets.Ticket, deal *b24.Deal) error {
	if ticket == nil || deal == nil {
		return nil
	}
	changed := false
	oldStatus := strings.TrimSpace(ticket.Status)
	oldDescription := strings.TrimSpace(ticket.Description)
	oldAssigneeID := uint(0)
	if ticket.AssigneeID != nil {
		oldAssigneeID = *ticket.AssigneeID
	}

	subject := strings.TrimSpace(deal.Title)
	if subject != "" && strings.TrimSpace(ticket.Subject) != subject {
		ticket.Subject = subject
		changed = true
	}
	description := convertBitrixDescriptionForEtalon(toString(deal.Raw["UF_CRM_1766060620"]))
	if description != "" && strings.TrimSpace(ticket.Description) != description {
		ticket.Description = description
		changed = true
	}
	nextStatus := mapStageToTicketStatus(deal.StageID)
	if nextStatus != "" && strings.TrimSpace(ticket.Status) != nextStatus {
		ticket.Status = nextStatus
		changed = true
	}
	pointID := int64FromAny(deal.Raw["UF_CRM_1766062398"])
	if pointID > 0 {
		if ticket.BitrixServicePointID == nil || *ticket.BitrixServicePointID != pointID {
			ticket.BitrixServicePointID = &pointID
			changed = true
		}
	}
	if deal.AssignedBy != nil && *deal.AssignedBy > 0 {
		etalonAssigneeID, resolved, err := s.resolveEtalonUserIDByBitrixUserID(ctx, *deal.AssignedBy)
		if err != nil {
			return err
		}
		if resolved {
			if ticket.AssigneeID == nil || *ticket.AssigneeID != etalonAssigneeID {
				assigneeID := etalonAssigneeID
				ticket.AssigneeID = &assigneeID
				changed = true
			}
			s.log.Info("Bitrix24: исполнитель сопоставлен и применён к тикету", "ticket_id", ticket.ID, "b24_user_id", *deal.AssignedBy, "etalon_user_id", etalonAssigneeID)
		} else {
			s.log.Warn("Bitrix24: сопоставление ASSIGNED_BY_ID не найдено, исполнитель не изменён", "ticket_id", ticket.ID, "b24_user_id", *deal.AssignedBy)
		}
	} else if ticket.AssigneeID != nil {
		ticket.AssigneeID = nil
		changed = true
	}
	if !changed {
		return nil
	}
	ticket.LastUpdatedBy = "bitrix_webhook"
	if err := s.ticketRepo.Update(ctx, ticket); err != nil {
		return err
	}
	if s.history != nil {
		if oldStatus != strings.TrimSpace(ticket.Status) {
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				Action:   tickets.HistoryActionFieldChanged,
				Field:    tickets.HistoryFieldStatus,
				Source:   tickets.HistorySourceBitrix,
				OldValue: oldStatus,
				NewValue: strings.TrimSpace(ticket.Status),
			})
		}
		if oldDescription != strings.TrimSpace(ticket.Description) {
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				Action:   tickets.HistoryActionFieldChanged,
				Field:    tickets.HistoryFieldDescription,
				Source:   tickets.HistorySourceBitrix,
				OldValue: oldDescription,
				NewValue: strings.TrimSpace(ticket.Description),
			})
		}
		newAssigneeID := uint(0)
		if ticket.AssigneeID != nil {
			newAssigneeID = *ticket.AssigneeID
		}
		if oldAssigneeID != newAssigneeID {
			oldAssigneeName := s.resolveEtalonAssigneeName(ctx, oldAssigneeID)
			newAssigneeName := s.resolveEtalonAssigneeName(ctx, newAssigneeID)
			meta := map[string]interface{}{}
			if deal.AssignedBy != nil {
				meta["bitrix_user_id"] = *deal.AssignedBy
			}
			s.history.Write(ctx, TicketHistoryWriteRequest{
				TicketID: ticket.ID,
				Action:   tickets.HistoryActionFieldChanged,
				Field:    tickets.HistoryFieldAssignee,
				Source:   tickets.HistorySourceBitrix,
				OldValue: oldAssigneeName,
				NewValue: newAssigneeName,
				Meta:     meta,
			})
		}
	}
	return nil
}

func (s *bitrixIncomingService) resolveEtalonUserIDByBitrixUserID(ctx context.Context, b24UserID int64) (uint, bool, error) {
	if b24UserID <= 0 {
		return 0, false, nil
	}

	userMap, err := s.repo.GetUserMapByB24ID(ctx, b24UserID)
	if err != nil {
		return 0, false, err
	}
	if userMap != nil {
		return userMap.EtalonUserID, true, nil
	}

	targetExternalID := strconv.FormatInt(b24UserID, 10)
	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return 0, false, err
	}

	matchedIDs := make([]uint, 0, 1)
	addMatch := func(userID uint) {
		for _, existing := range matchedIDs {
			if existing == userID {
				return
			}
		}
		matchedIDs = append(matchedIDs, userID)
	}

	for i := range users {
		u := users[i]
		if u.ExternalType != nil && u.ExternalID != nil &&
			strings.TrimSpace(strings.ToLower(*u.ExternalType)) == user.ExternalTypeBitrix24 &&
			strings.TrimSpace(*u.ExternalID) == targetExternalID {
			addMatch(u.ID)
		}
		for j := range u.Integrations {
			integration := u.Integrations[j]
			if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeBitrix24 {
				continue
			}
			if strings.TrimSpace(integration.ExternalID) != targetExternalID {
				continue
			}
			if !integration.IsVerified && !integration.IsLocked {
				continue
			}
			addMatch(u.ID)
		}
	}

	if len(matchedIDs) == 0 {
		cacheItems, cacheErr := s.repo.ListUserCache(ctx)
		if cacheErr == nil {
			var targetCache *bitrix.UserCache
			for i := range cacheItems {
				if cacheItems[i].B24UserID == b24UserID {
					targetCache = &cacheItems[i]
					break
				}
			}
			if targetCache != nil {
				targetFirst := normalizePersonToken(targetCache.FirstName)
				targetLast := normalizePersonToken(targetCache.LastName)
				targetFull := normalizePersonToken(strings.TrimSpace(strings.Join([]string{targetCache.LastName, targetCache.FirstName, targetCache.SecondName}, " ")))
				for i := range users {
					userFirst := normalizePersonToken(users[i].FirstName)
					userLast := normalizePersonToken(users[i].LastName)
					userFull := normalizePersonToken(users[i].FullName)
					if targetFirst != "" && targetLast != "" && userFirst == targetFirst && userLast == targetLast {
						addMatch(users[i].ID)
						continue
					}
					if targetFull != "" && userFull == targetFull {
						addMatch(users[i].ID)
					}
				}
			}
		}
	}

	if len(matchedIDs) == 0 {
		return 0, false, nil
	}
	if len(matchedIDs) > 1 {
		s.log.Warn("Bitrix24: найдено несколько кандидатов для ASSIGNED_BY_ID, сопоставление не выполнено", "b24_user_id", b24UserID, "candidate_count", len(matchedIDs))
		return 0, false, nil
	}

	resolvedUserID := matchedIDs[0]
	if upsertErr := s.repo.UpsertUserMap(ctx, &bitrix.UserMap{
		EtalonUserID: resolvedUserID,
		B24UserID:    b24UserID,
	}); upsertErr != nil {
		s.log.Warn("Bitrix24: не удалось сохранить auto-mapping пользователя", "b24_user_id", b24UserID, "etalon_user_id", resolvedUserID, "error", upsertErr)
	}
	return resolvedUserID, true, nil
}
func (s *bitrixIncomingService) resolveEtalonAssigneeName(ctx context.Context, userID uint) string {
	if userID == 0 {
		return "Не назначен"
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || u == nil {
		return fmt.Sprintf("#%d", userID)
	}
	if strings.TrimSpace(u.FullName) != "" {
		return strings.TrimSpace(u.FullName)
	}
	return strings.TrimSpace(strings.Join([]string{u.LastName, u.FirstName}, " "))
}

func (s *bitrixIncomingService) syncAllTimelineCommentsForDeal(ctx context.Context, ticket *tickets.Ticket) error {
	if ticket == nil {
		return nil
	}
	link, err := s.repo.GetDealLinkByTicketID(ctx, ticket.ID)
	if err != nil || link == nil {
		return err
	}
	start := 0
	for {
		items, next, listErr := s.client.TimelineCommentList(ctx, link.B24DealID, start)
		if listErr != nil {
			return listErr
		}
		for i := range items {
			comment := items[i]
			if comment.ID <= 0 {
				continue
			}
			if isClosedTicketStatus(ticket.Status) {
				continue
			}
			item := comment
			if err = s.addOrUpdateCommentFromBitrix(ctx, ticket, &item, false); err != nil {
				return err
			}
		}
		if next <= 0 {
			break
		}
		start = next
	}
	return nil
}

func (s *bitrixIncomingService) publishTicketUpdated(ticketID, action, source, message string) {
	if s.eventBus == nil || strings.TrimSpace(ticketID) == "" {
		return
	}
	s.eventBus.Publish(eventbus.Event{
		Type: events.TicketUpdated,
		Payload: events.TicketUpdatedPayload{
			TicketID:   ticketID,
			Action:     strings.TrimSpace(action),
			Source:     strings.TrimSpace(source),
			Message:    strings.TrimSpace(message),
			OccurredAt: time.Now(),
		},
	})
}

func (s *bitrixIncomingService) isSuppressedDeal(ctx context.Context, dealID int64) bool {
	if s.redis == nil || dealID <= 0 {
		return false
	}
	ok, err := s.redis.Exists(ctx, fmt.Sprintf("b24:suppress:deal:%d", dealID)).Result()
	if err != nil || ok == 0 {
		return false
	}
	if s.cfg.BitrixIntegrationUserID <= 0 || s.client == nil {
		return true
	}
	deal, dealErr := s.client.DealGet(ctx, dealID)
	if dealErr != nil || deal == nil || deal.ModifiedBy == nil {
		return true
	}
	return *deal.ModifiedBy == s.cfg.BitrixIntegrationUserID
}

func (s *bitrixIncomingService) isSuppressedComment(ctx context.Context, commentID int64) bool {
	if s.redis == nil || commentID <= 0 {
		return false
	}
	ok, err := s.redis.Exists(ctx, fmt.Sprintf("b24:suppress:comment:%d", commentID)).Result()
	return err == nil && ok > 0
}

func isClosedTicketStatus(status string) bool {
	normalized := strings.TrimSpace(strings.ToLower(status))
	return normalized == tickets.StatusResolved || normalized == tickets.StatusClosed
}

func (s *bitrixIncomingService) maxAttempts() int {
	if s.cfg.BitrixIncomingMaxAttempts <= 0 {
		return 10
	}
	return s.cfg.BitrixIncomingMaxAttempts
}

func (s *bitrixIncomingService) retryBaseDelay() time.Duration {
	if s.cfg.BitrixIncomingRetryBase <= 0 {
		return 500 * time.Millisecond
	}
	return s.cfg.BitrixIncomingRetryBase
}

func (s *bitrixIncomingService) retryMaxDelay() time.Duration {
	if s.cfg.BitrixIncomingRetryMax <= 0 {
		return 30 * time.Second
	}
	return s.cfg.BitrixIncomingRetryMax
}

func (s *bitrixIncomingService) retryDelay(attempts int) time.Duration {
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

func (s *bitrixIncomingService) shouldApplyRetryBackoff(item *bitrix.IncomingEvent) bool {
	if item == nil {
		return false
	}
	return item.Attempts < s.maxAttempts()
}

func (s *bitrixIncomingService) shouldEnqueueNow(item *bitrix.IncomingEvent) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Status) == bitrix.IncomingEventStatusFailed {
		if item.Attempts >= s.maxAttempts() {
			return false
		}
		nextTryAt := item.UpdatedAt.Add(s.retryDelay(item.Attempts))
		if time.Now().Before(nextTryAt) {
			return false
		}
	}
	return true
}

func parseInt64(v string) (int64, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func safeEntityID(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func int64FromAny(v interface{}) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case float32:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
