package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"fmt"
	"hash/fnv"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"etalon-server/pkg/eventbus"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrMegafonVATSWebhookUnauthorized = errors.New("неверный crm_token вебхука Мегафон ВАТС")
	ErrMegafonVATSWebhookBadRequest   = errors.New("некорректный payload вебхука Мегафон ВАТС")
)

var supportedMegafonVATSCommands = map[string]struct{}{
	telephony.IncomingEventCommandEvent:   {},
	telephony.IncomingEventCommandHistory: {},
}

type MegafonVATSIncomingService interface {
	HandleWebhook(ctx context.Context, rawBody []byte, form url.Values) error
	Start(ctx context.Context)
	ReplayEvent(ctx context.Context, id string) error
}

type megafonVATSIncomingService struct {
	cfg              *config.Config
	log              logger.LoggerInterface
	redis            *redis.Client
	repo             telephony.Repository
	userRepo         user.Repository
	ticketRepo       tickets.TicketRepository
	eventBus         eventbus.EventBus
	recordingService MegafonVATSRecordingService

	consumerName string
	callLocks    keyedMutex
}

func NewMegafonVATSIncomingService(
	cfg *config.Config,
	log logger.LoggerInterface,
	redisClient *redis.Client,
	repo telephony.Repository,
	userRepo user.Repository,
	ticketRepo tickets.TicketRepository,
	eventBus eventbus.EventBus,
	recordingService MegafonVATSRecordingService,
) MegafonVATSIncomingService {
	host, _ := os.Hostname()
	return &megafonVATSIncomingService{
		cfg:              cfg,
		log:              log,
		redis:            redisClient,
		repo:             repo,
		userRepo:         userRepo,
		ticketRepo:       ticketRepo,
		eventBus:         eventBus,
		recordingService: recordingService,
		consumerName:     fmt.Sprintf("%s-%d-%s", strings.TrimSpace(host), os.Getpid(), uuid.NewString()),
	}
}

func (s *megafonVATSIncomingService) HandleWebhook(ctx context.Context, rawBody []byte, form url.Values) error {
	if s == nil || s.cfg == nil || !s.cfg.EnableMegafonVATS {
		return ErrMegafonVATSWebhookBadRequest
	}
	if s.repo == nil {
		return errors.New("репозиторий телефонии не настроен")
	}

	crmToken := strings.TrimSpace(form.Get("crm_token"))
	if crmToken == "" || crmToken != strings.TrimSpace(s.cfg.MegafonVATSCRMToken) {
		return ErrMegafonVATSWebhookUnauthorized
	}

	cmd := strings.ToLower(strings.TrimSpace(form.Get("cmd")))
	if _, ok := supportedMegafonVATSCommands[cmd]; !ok {
		return ErrMegafonVATSWebhookBadRequest
	}

	callID := strings.TrimSpace(form.Get("callid"))
	if callID == "" {
		return ErrMegafonVATSWebhookBadRequest
	}

	eventName := strings.TrimSpace(form.Get("type"))
	if cmd == telephony.IncomingEventCommandEvent && eventName == "" {
		return ErrMegafonVATSWebhookBadRequest
	}

	incomingEvent := &telephony.IncomingEvent{
		ID:             uuid.NewString(),
		Provider:       telephony.ProviderMegafonVATS,
		Cmd:            cmd,
		EventName:      eventName,
		ExternalCallID: callID,
		PayloadRaw:     string(rawBody),
		PayloadHash:    buildMegafonVATSPayloadHash(form),
		Status:         telephony.IncomingEventStatusNew,
		ReceivedAt:     time.Now(),
	}

	created, err := s.repo.InsertIncomingEventIfNotExists(ctx, incomingEvent)
	if err != nil {
		return err
	}
	if !created {
		if s.log != nil {
			s.log.Debug(
				"Мегафон ВАТС: входящее событие отброшено как дубликат",
				"cmd", incomingEvent.Cmd,
				"event_name", incomingEvent.EventName,
				"external_call_id", incomingEvent.ExternalCallID,
				"payload_hash", incomingEvent.PayloadHash,
			)
		}
		return nil
	}

	if err = s.enqueueEvent(ctx, incomingEvent.ID); err != nil {
		if s.log != nil {
			s.log.Warn(
				"Мегафон ВАТС: не удалось поставить событие в Redis Streams, обработка останется на recovery-цикле Postgres",
				"event_id", incomingEvent.ID,
				"error", err,
			)
		}
		return nil
	}
	if err = s.repo.MarkIncomingQueued(ctx, incomingEvent.ID); err != nil && s.log != nil {
		s.log.Warn("Мегафон ВАТС: не удалось отметить событие как queued", "event_id", incomingEvent.ID, "error", err)
	}

	return nil
}

func (s *megafonVATSIncomingService) ReplayEvent(ctx context.Context, id string) error {
	if s == nil || s.repo == nil {
		return fmt.Errorf("Megafon VATS incoming service не настроен")
	}
	eventID := strings.TrimSpace(id)
	if eventID == "" {
		return fmt.Errorf("не указан event_id")
	}
	if err := s.repo.ResetIncomingEventForReplay(ctx, eventID); err != nil {
		return err
	}
	if err := s.enqueueEvent(ctx, eventID); err != nil {
		if s.log != nil {
			s.log.Warn("Мегафон ВАТС: replay не удалось сразу отправить в Redis, событие подберёт recovery-цикл", "event_id", eventID, "error", err)
		}
		return nil
	}
	return s.repo.MarkIncomingQueued(ctx, eventID)
}

func (s *megafonVATSIncomingService) Start(ctx context.Context) {
	if s == nil || s.cfg == nil || !s.cfg.EnableMegafonVATS {
		if s != nil && s.log != nil {
			s.log.Info("Мегафон ВАТС webhook worker отключен")
		}
		return
	}
	if s.repo == nil {
		if s.log != nil {
			s.log.Warn("Мегафон ВАТС webhook worker отключен: репозиторий телефонии не настроен")
		}
		return
	}

	if s.log != nil {
		s.log.Info(
			"Мегафон ВАТС webhook worker запущен",
			"redis_enabled", s.redis != nil,
			"stream", s.cfg.MegafonVATSEventsStreamName,
			"consumer_group", s.cfg.MegafonVATSEventsConsumerGroup,
			"consumer_name", s.consumerName,
		)
	}

	if err := s.ensureRedisAvailable(ctx); err != nil {
		if s.log != nil {
			s.log.Error("Мегафон ВАТС: Redis недоступен, webhook worker остановлен", "error", err)
		}
		return
	}
	if err := s.ensureConsumerGroup(ctx); err != nil {
		if s.log != nil {
			s.log.Error("Мегафон ВАТС: не удалось подготовить consumer group", "error", err)
		}
		return
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
		s.recoveryLoop(ctx)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.claimPendingLoop(ctx)
	}()

	<-ctx.Done()
	wg.Wait()
}

func (s *megafonVATSIncomingService) ensureConsumerGroup(ctx context.Context) error {
	if s.redis == nil {
		return errors.New("redis не настроен")
	}
	err := s.redis.XGroupCreateMkStream(ctx, s.cfg.MegafonVATSEventsStreamName, s.cfg.MegafonVATSEventsConsumerGroup, "$").Err()
	if err != nil && strings.Contains(strings.ToUpper(err.Error()), "BUSYGROUP") {
		return nil
	}
	return err
}

func (s *megafonVATSIncomingService) ensureRedisAvailable(ctx context.Context) error {
	if s.redis == nil {
		return errors.New("redis не настроен")
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.redis.Ping(pingCtx).Err()
}

func (s *megafonVATSIncomingService) enqueueEvent(ctx context.Context, eventID string) error {
	if s.redis == nil {
		return errors.New("redis не настроен")
	}
	return s.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: s.cfg.MegafonVATSEventsStreamName,
		Values: map[string]any{
			"event_id": strings.TrimSpace(eventID),
			"ts":       time.Now().Unix(),
		},
	}).Err()
}

func (s *megafonVATSIncomingService) dispatchLoop(ctx context.Context) {
	if s.redis == nil {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		items, err := s.repo.ListIncomingNewOrFailedForEnqueue(ctx, 200, s.maxAttempts())
		if err != nil {
			if s.log != nil {
				s.log.Error("Мегафон ВАТС: не удалось получить входящие события для enqueue", "error", err)
			}
			continue
		}
		for i := range items {
			if !s.shouldProcessIncomingNow(&items[i]) {
				continue
			}
			if err = s.enqueueEvent(ctx, items[i].ID); err != nil {
				continue
			}
			_ = s.repo.MarkIncomingQueued(ctx, items[i].ID)
		}
	}
}

func (s *megafonVATSIncomingService) consumeLoop(ctx context.Context) {
	if s.redis == nil {
		return
	}
	parallelism := s.cfg.MegafonVATSIncomingParallelism
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
			Group:    s.cfg.MegafonVATSEventsConsumerGroup,
			Consumer: s.consumerName,
			Streams:  []string{s.cfg.MegafonVATSEventsStreamName, ">"},
			Count:    20,
			Block:    2 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if s.log != nil {
				s.log.Warn("Мегафон ВАТС: ошибка XREADGROUP", "error", err)
			}
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				eventID := strings.TrimSpace(megafonAnyToString(msg.Values["event_id"]))
				if eventID == "" {
					_ = s.redis.XAck(ctx, s.cfg.MegafonVATSEventsStreamName, s.cfg.MegafonVATSEventsConsumerGroup, msg.ID).Err()
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

func (s *megafonVATSIncomingService) claimPendingLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		pending, err := s.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
			Stream: s.cfg.MegafonVATSEventsStreamName,
			Group:  s.cfg.MegafonVATSEventsConsumerGroup,
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
				Stream:   s.cfg.MegafonVATSEventsStreamName,
				Group:    s.cfg.MegafonVATSEventsConsumerGroup,
				Consumer: s.consumerName,
				MinIdle:  60 * time.Second,
				Messages: []string{pending[i].ID},
			}).Result()
			if claimErr != nil {
				continue
			}
			for _, msg := range claimed {
				eventID := strings.TrimSpace(megafonAnyToString(msg.Values["event_id"]))
				if eventID == "" {
					_ = s.redis.XAck(ctx, s.cfg.MegafonVATSEventsStreamName, s.cfg.MegafonVATSEventsConsumerGroup, msg.ID).Err()
					continue
				}
				s.processAndAck(ctx, msg.ID, eventID)
			}
		}
	}
}

func (s *megafonVATSIncomingService) recoveryLoop(ctx context.Context) {
	parallelism := s.cfg.MegafonVATSIncomingParallelism
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

		pending, err := s.repo.ListIncomingNewOrFailedForEnqueue(ctx, parallelism*4, s.maxAttempts())
		if err != nil {
			if s.log != nil {
				s.log.Error("Мегафон ВАТС: не удалось получить pending-входящие события", "error", err)
			}
			continue
		}
		queued, err := s.repo.ListIncomingQueuedForRecovery(ctx, parallelism*2, time.Now().Add(-s.queuedRecoveryDelay()))
		if err != nil {
			if s.log != nil {
				s.log.Error("Мегафон ВАТС: не удалось получить застрявшие queued-события", "error", err)
			}
			continue
		}

		items := make([]telephony.IncomingEvent, 0, len(pending)+len(queued))
		seen := make(map[string]struct{}, len(pending)+len(queued))
		for i := range pending {
			if !s.shouldProcessIncomingNow(&pending[i]) {
				continue
			}
			if _, exists := seen[pending[i].ID]; exists {
				continue
			}
			seen[pending[i].ID] = struct{}{}
			items = append(items, pending[i])
		}
		for i := range queued {
			if _, exists := seen[queued[i].ID]; exists {
				continue
			}
			seen[queued[i].ID] = struct{}{}
			items = append(items, queued[i])
		}
		if len(items) == 0 {
			continue
		}

		sem := make(chan struct{}, parallelism)
		var wg sync.WaitGroup
		for i := range items {
			eventID := items[i].ID
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				s.processIncomingEvent(ctx, eventID)
			}()
		}
		wg.Wait()
	}
}

func (s *megafonVATSIncomingService) processAndAck(ctx context.Context, messageID string, eventID string) {
	defer func() {
		if s.redis != nil {
			_ = s.redis.XAck(ctx, s.cfg.MegafonVATSEventsStreamName, s.cfg.MegafonVATSEventsConsumerGroup, messageID).Err()
		}
	}()
	s.processIncomingEvent(ctx, eventID)
}

func (s *megafonVATSIncomingService) processIncomingEvent(ctx context.Context, eventID string) {
	item, err := s.repo.GetIncomingEventByID(ctx, eventID)
	if err != nil || item == nil {
		if err != nil && s.log != nil {
			s.log.Error("Мегафон ВАТС: не удалось получить входящее событие по id", "event_id", eventID, "error", err)
		}
		return
	}

	form, err := url.ParseQuery(item.PayloadRaw)
	if err != nil {
		_ = s.repo.MarkIncomingFailed(ctx, item.ID, err.Error())
		return
	}
	payload := buildMegafonVATSPayload(form)

	unlock := s.lockCallChain(item.ExternalCallID, payload.SecondCallID)
	defer unlock()

	marked, err := s.repo.TryMarkIncomingProcessing(ctx, item.ID)
	if err != nil {
		if s.log != nil {
			s.log.Warn("Мегафон ВАТС: не удалось отметить событие как processing", "event_id", item.ID, "error", err)
		}
		return
	}
	if !marked {
		return
	}

	status, reason, err := s.handleIncomingEvent(ctx, item, payload)
	if err != nil {
		_ = s.repo.MarkIncomingFailed(ctx, item.ID, err.Error())
		if s.log != nil {
			s.log.Error(
				"Мегафон ВАТС: ошибка обработки входящего события",
				"event_id", item.ID,
				"cmd", item.Cmd,
				"event_name", item.EventName,
				"external_call_id", item.ExternalCallID,
				"error", err,
			)
		}
		return
	}

	switch status {
	case telephony.IncomingEventStatusIgnored:
		_ = s.repo.MarkIncomingIgnored(ctx, item.ID, reason)
	case telephony.IncomingEventStatusDone:
		_ = s.repo.MarkIncomingDone(ctx, item.ID)
	default:
		_ = s.repo.MarkIncomingFailed(ctx, item.ID, "неподдерживаемый итог обработки")
	}

	if s.log != nil {
		s.log.Info(
			"Мегафон ВАТС: входящее событие обработано",
			"event_id", item.ID,
			"cmd", item.Cmd,
			"event_name", item.EventName,
			"external_call_id", item.ExternalCallID,
			"status", status,
			"reason", reason,
		)
	}
}

func (s *megafonVATSIncomingService) handleIncomingEvent(
	ctx context.Context,
	incomingEvent *telephony.IncomingEvent,
	payload megafonVATSPayload,
) (string, string, error) {
	if incomingEvent == nil {
		return telephony.IncomingEventStatusIgnored, "пустое событие", nil
	}

	if ok, reason := isMegafonExternalCall(payload, incomingEvent.Cmd); !ok {
		return telephony.IncomingEventStatusIgnored, reason, nil
	}

	call, mergedSourceID, err := s.buildCallSnapshot(ctx, incomingEvent, payload)
	if err != nil {
		return "", "", err
	}
	if call == nil {
		return telephony.IncomingEventStatusIgnored, "звонок не подходит под поддерживаемый сценарий", nil
	}

	if mergedSourceID != "" {
		if err = s.repo.MergeCalls(ctx, call, mergedSourceID); err != nil {
			return "", "", err
		}
	} else if err = s.repo.UpsertCall(ctx, call); err != nil {
		return "", "", err
	}

	eventType := strings.TrimSpace(payload.EventName)
	if incomingEvent.Cmd == telephony.IncomingEventCommandHistory {
		eventType = incomingEvent.Cmd
	}
	callEvent := &telephony.CallEvent{
		TelephonyCallID:     call.ID,
		EventType:           strings.TrimSpace(eventType),
		ExternalCallID:      incomingEvent.ExternalCallID,
		IncomingPayloadHash: incomingEvent.PayloadHash,
		PayloadRaw:          incomingEvent.PayloadRaw,
	}
	if secondCallID := strings.TrimSpace(payload.SecondCallID); secondCallID != "" {
		callEvent.SecondCallID = &secondCallID
	}
	if err = s.repo.AddCallEvent(ctx, callEvent); err != nil {
		return "", "", err
	}
	if s.recordingService != nil {
		if err = s.recordingService.SyncCallRecording(ctx, call.ID); err != nil && s.log != nil {
			s.log.Warn("Мегафон ВАТС: не удалось синхронизировать запись звонка", "call_id", call.ID, "external_call_id", incomingEvent.ExternalCallID, "error", err)
		}
	}
	if err = s.ensureCallContext(ctx, call); err != nil {
		return "", "", err
	}
	s.updateEmployeeStatusFromEvent(ctx, incomingEvent, payload)
	publishTelephonyLineUpdate(ctx, s.log, s.eventBus, s.repo, s.userRepo)

	return telephony.IncomingEventStatusDone, "", nil
}

func (s *megafonVATSIncomingService) ensureCallContext(ctx context.Context, call *telephony.Call) error {
	if s == nil || s.repo == nil || call == nil {
		return nil
	}
	phone := safeMegafonStringPointer(call.ClientPhone)
	if phone == "" {
		return nil
	}

	contact, err := s.repo.EnsureContact(ctx, phone, phone)
	if err != nil {
		return err
	}
	linkedTicket, err := autoBindTelephonyCallToActiveTicket(ctx, s.repo, s.ticketRepo, call, contact)
	if err != nil {
		return err
	}
	return s.ensurePendingContext(ctx, call, contact, linkedTicket)
}

func (s *megafonVATSIncomingService) ensurePendingContext(
	ctx context.Context,
	call *telephony.Call,
	_ *telephony.Contact,
	linkedTicket *tickets.Ticket,
) error {
	if linkedTicket != nil {
		existing, err := s.repo.GetPendingContextByExternalCallID(ctx, call.ExternalCallID)
		if err != nil || existing == nil {
			return err
		}
		ticketID := linkedTicket.ID
		reason := "звонок автоматически привязан к активному тикету по контакту"
		return s.repo.UpdatePendingContext(ctx, existing.ID, telephony.PendingContextStatusBound, &ticketID, &reason)
	}
	if !shouldCreateMegafonPendingContext(call) {
		return nil
	}

	existing, err := s.repo.GetPendingContextByExternalCallID(ctx, call.ExternalCallID)
	if err != nil {
		return err
	}
	if existing != nil && existing.Status != telephony.PendingContextStatusNew {
		return nil
	}

	expiresAt := megafonPendingContextExpiresAt(call)
	if existing != nil {
		existing.EmployeeUserID = *call.EmployeeUserID
		existing.ClientPhone = safeMegafonStringPointer(call.ClientPhone)
		existing.Status = telephony.PendingContextStatusNew
		existing.ExpiresAt = expiresAt
		existing.LinkedTicketID = nil
		existing.DecisionReason = nil
		return s.repo.UpsertPendingContext(ctx, existing)
	}

	return s.repo.UpsertPendingContext(ctx, &telephony.PendingContext{
		ID:             uuid.NewString(),
		EmployeeUserID: *call.EmployeeUserID,
		ExternalCallID: call.ExternalCallID,
		ClientPhone:    safeMegafonStringPointer(call.ClientPhone),
		Status:         telephony.PendingContextStatusNew,
		ExpiresAt:      expiresAt,
	})
}

func (s *megafonVATSIncomingService) buildCallSnapshot(
	ctx context.Context,
	incomingEvent *telephony.IncomingEvent,
	payload megafonVATSPayload,
) (*telephony.Call, string, error) {
	call, err := s.repo.GetCallByAnyExternalID(ctx, telephony.ProviderMegafonVATS, incomingEvent.ExternalCallID)
	if err != nil {
		return nil, "", err
	}
	if call == nil {
		call = &telephony.Call{
			ID:             uuid.NewString(),
			Provider:       telephony.ProviderMegafonVATS,
			ExternalCallID: incomingEvent.ExternalCallID,
		}
	}

	mergedSourceID := ""
	if shouldLinkTransferredCall(payload) {
		secondaryCall, secondaryErr := s.repo.GetCallByAnyExternalID(ctx, telephony.ProviderMegafonVATS, payload.SecondCallID)
		if secondaryErr != nil {
			return nil, "", secondaryErr
		}
		if secondaryCall != nil && secondaryCall.ID != call.ID {
			mergeMegafonCallSnapshots(call, secondaryCall)
			mergedSourceID = secondaryCall.ID
		}
	}

	call.RawSnapshot = incomingEvent.PayloadRaw
	call.UpdatedAt = time.Now()
	call.LastEventType = stringPtr(resolveMegafonEventType(incomingEvent.Cmd, payload))

	if direction := resolveMegafonDirection(incomingEvent.Cmd, payload); direction != "" {
		call.Direction = direction
	}
	if phone := normalizeMegafonPhone(payload.Phone); phone != "" {
		call.ClientPhone = &phone
	}
	if vatNumber := normalizeMegafonPhone(payload.Diversion); vatNumber != "" {
		call.VATNumber = &vatNumber
	}
	if employeeLogin := strings.TrimSpace(payload.User); employeeLogin != "" {
		call.EmployeeLogin = &employeeLogin
	}
	if groupName := strings.TrimSpace(payload.GroupRealName); groupName != "" {
		call.GroupName = &groupName
	}
	if missedStatus := strings.TrimSpace(payload.MissedStatus); missedStatus != "" {
		call.MissedStatus = &missedStatus
	}
	if recordingURL := strings.TrimSpace(payload.Link); recordingURL != "" {
		call.RecordingURL = &recordingURL
		call.HasRecording = true
	}

	if employeeUserID, ok, resolveErr := s.resolveMegafonUserID(ctx, payload.User); resolveErr != nil {
		return nil, "", resolveErr
	} else if ok {
		call.EmployeeUserID = &employeeUserID
	}

	switch incomingEvent.Cmd {
	case telephony.IncomingEventCommandEvent:
		s.applyEventSnapshot(call, payload.EventName, incomingEvent.ReceivedAt)
	case telephony.IncomingEventCommandHistory:
		s.applyHistorySnapshot(call, payload)
	}

	return call, mergedSourceID, nil
}

func (s *megafonVATSIncomingService) resolveMegafonUserID(ctx context.Context, login string) (uint, bool, error) {
	login = strings.TrimSpace(login)
	if login == "" || s.userRepo == nil {
		return 0, false, nil
	}
	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return 0, false, err
	}
	for i := range users {
		for j := range users[i].Integrations {
			integration := users[i].Integrations[j]
			if !integration.IsEnabled {
				continue
			}
			if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
				continue
			}
			if strings.TrimSpace(integration.ExternalID) != login {
				continue
			}
			return users[i].ID, true, nil
		}
	}
	return 0, false, nil
}

func (s *megafonVATSIncomingService) applyEventSnapshot(call *telephony.Call, eventName string, receivedAt time.Time) {
	if call == nil {
		return
	}

	normalizedEvent := strings.ToUpper(strings.TrimSpace(eventName))
	switch normalizedEvent {
	case "INCOMING", "OUTGOING":
		call.Status = mergeMegafonEventStatus(call.Status, strings.ToLower(normalizedEvent))
		call.StartedAt = earlierTime(call.StartedAt, &receivedAt)
	case "ACCEPTED":
		call.Status = mergeMegafonEventStatus(call.Status, "accepted")
		call.AnsweredAt = earlierTime(call.AnsweredAt, &receivedAt)
	case "TRANSFERRED":
		call.Status = mergeMegafonEventStatus(call.Status, "transferred")
	case "COMPLETED":
		call.Status = mergeMegafonEventStatus(call.Status, "completed")
		call.CompletedAt = laterTime(call.CompletedAt, &receivedAt)
	case "CANCELLED":
		call.Status = mergeMegafonEventStatus(call.Status, "cancelled")
		call.CompletedAt = laterTime(call.CompletedAt, &receivedAt)
	}
}

func (s *megafonVATSIncomingService) applyHistorySnapshot(call *telephony.Call, payload megafonVATSPayload) {
	applyMegafonHistorySnapshot(call, payload)
}

func (s *megafonVATSIncomingService) updateEmployeeStatusFromEvent(ctx context.Context, incomingEvent *telephony.IncomingEvent, payload megafonVATSPayload) {
	if s == nil || s.repo == nil || incomingEvent == nil || incomingEvent.Cmd != telephony.IncomingEventCommandEvent {
		return
	}
	login := strings.TrimSpace(payload.User)
	if login == "" {
		return
	}
	status := megafonEmployeeStatusFromEvent(payload.EventName)
	if status == "" {
		return
	}
	if err := s.repo.UpdateProviderEmployeeStatus(ctx, telephony.ProviderMegafonVATS, login, status, incomingEvent.ReceivedAt); err != nil && s.log != nil {
		s.log.Warn("Мегафон ВАТС: не удалось обновить состояние сотрудника по webhook", "login", login, "status", status, "error", err)
	}
}

func megafonEmployeeStatusFromEvent(eventName string) string {
	switch strings.ToUpper(strings.TrimSpace(eventName)) {
	case "INCOMING", "OUTGOING", "ACCEPTED", "TRANSFERRED":
		return "in_call"
	case "COMPLETED", "CANCELLED", "FAILED", "BUSY", "MISSED", "NOANSWER":
		return "online"
	default:
		return ""
	}
}

func applyMegafonHistorySnapshot(call *telephony.Call, payload megafonVATSPayload) {
	if call == nil {
		return
	}

	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status != "" {
		call.Status = status
	}

	startedAt := parseMegafonVATSTime(payload.Start)
	call.StartedAt = earlierTime(call.StartedAt, startedAt)

	if payload.WaitSeconds != nil {
		call.WaitSeconds = payload.WaitSeconds
	}
	if payload.DurationSeconds != nil {
		call.DurationSeconds = payload.DurationSeconds
	}

	if startedAt == nil {
		return
	}

	waitDuration := time.Duration(0)
	if payload.WaitSeconds != nil {
		waitDuration = time.Duration(*payload.WaitSeconds) * time.Second
	}

	if isMegafonAnsweredHistoryStatus(status, payload.DurationSeconds) {
		answeredAt := startedAt.Add(waitDuration)
		call.AnsweredAt = earlierTime(call.AnsweredAt, &answeredAt)
	}

	switch {
	case payload.DurationSeconds != nil && isMegafonAnsweredHistoryStatus(status, payload.DurationSeconds):
		completedAt := startedAt.Add(waitDuration + time.Duration(*payload.DurationSeconds)*time.Second)
		call.CompletedAt = laterTime(call.CompletedAt, &completedAt)
	case payload.WaitSeconds != nil:
		completedAt := startedAt.Add(waitDuration)
		call.CompletedAt = laterTime(call.CompletedAt, &completedAt)
	case payload.DurationSeconds != nil:
		completedAt := startedAt.Add(time.Duration(*payload.DurationSeconds) * time.Second)
		call.CompletedAt = laterTime(call.CompletedAt, &completedAt)
	}
}

func (s *megafonVATSIncomingService) lockCallChain(callIDs ...string) func() {
	if s == nil {
		return func() {}
	}

	keys := make([]string, 0, len(callIDs))
	seen := make(map[string]struct{}, len(callIDs))
	for _, item := range callIDs {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		keys = append(keys, trimmed)
	}
	if len(keys) == 0 {
		return func() {}
	}

	slices.Sort(keys)
	unlocks := make([]func(), 0, len(keys))
	for _, key := range keys {
		unlocks = append(unlocks, s.callLocks.Lock(callLockKey(key)))
	}

	return func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
}

func (s *megafonVATSIncomingService) maxAttempts() int {
	if s == nil || s.cfg == nil || s.cfg.MegafonVATSIncomingMaxAttempts <= 0 {
		return 10
	}
	return s.cfg.MegafonVATSIncomingMaxAttempts
}

func (s *megafonVATSIncomingService) retryBaseDelay() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.MegafonVATSRetryBase <= 0 {
		return 500 * time.Millisecond
	}
	return s.cfg.MegafonVATSRetryBase
}

func (s *megafonVATSIncomingService) retryMaxDelay() time.Duration {
	if s == nil || s.cfg == nil || s.cfg.MegafonVATSRetryMax <= 0 {
		return 30 * time.Second
	}
	return s.cfg.MegafonVATSRetryMax
}

func (s *megafonVATSIncomingService) retryDelay(attempts int) time.Duration {
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

func (s *megafonVATSIncomingService) shouldProcessIncomingNow(item *telephony.IncomingEvent) bool {
	if item == nil {
		return false
	}
	if strings.TrimSpace(item.Status) == telephony.IncomingEventStatusFailed {
		if item.Attempts >= s.maxAttempts() {
			return false
		}
		nextTryAt := item.UpdatedAt.Add(s.retryDelay(item.Attempts))
		return !time.Now().Before(nextTryAt)
	}
	return true
}

func (s *megafonVATSIncomingService) queuedRecoveryDelay() time.Duration {
	return 30 * time.Second
}

type megafonVATSPayload struct {
	EventName       string
	HistoryType     string
	CallID          string
	SecondCallID    string
	Phone           string
	User            string
	Direction       string
	Diversion       string
	GroupRealName   string
	Status          string
	Start           string
	Link            string
	MissedStatus    string
	WaitSeconds     *int
	DurationSeconds *int
}

func buildMegafonVATSPayload(form url.Values) megafonVATSPayload {
	return megafonVATSPayload{
		EventName:       strings.TrimSpace(form.Get("type")),
		HistoryType:     strings.TrimSpace(form.Get("type")),
		CallID:          strings.TrimSpace(form.Get("callid")),
		SecondCallID:    strings.TrimSpace(form.Get("second_callid")),
		Phone:           strings.TrimSpace(form.Get("phone")),
		User:            strings.TrimSpace(form.Get("user")),
		Direction:       strings.TrimSpace(form.Get("direction")),
		Diversion:       strings.TrimSpace(form.Get("diversion")),
		GroupRealName:   firstMegafonValue(form.Get("groupRealName"), form.Get("telnum_name")),
		Status:          strings.TrimSpace(form.Get("status")),
		Start:           strings.TrimSpace(form.Get("start")),
		Link:            strings.TrimSpace(form.Get("link")),
		MissedStatus:    strings.TrimSpace(form.Get("missedStatus")),
		WaitSeconds:     parseMegafonVATSInt(form.Get("wait")),
		DurationSeconds: parseMegafonVATSInt(form.Get("duration")),
	}
}

func buildMegafonVATSPayloadHash(form url.Values) string {
	normalized := make(url.Values, len(form))
	for key, values := range form {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey == "" || normalizedKey == "crm_token" {
			continue
		}
		normalizedValues := make([]string, 0, len(values))
		for _, value := range values {
			normalizedValues = append(normalizedValues, strings.TrimSpace(value))
		}
		slices.Sort(normalizedValues)
		normalized[normalizedKey] = normalizedValues
	}
	sum := sha256.Sum256([]byte(normalized.Encode()))
	return hex.EncodeToString(sum[:])
}

func isMegafonExternalCall(payload megafonVATSPayload, cmd string) (bool, string) {
	phone := normalizeMegafonPhone(payload.Phone)
	if phone == "" {
		return false, "не удалось определить номер клиента"
	}
	if !looksLikeExternalPhone(phone) {
		return false, "внутренний номер клиента не обрабатывается"
	}

	direction := resolveMegafonDirection(cmd, payload)
	switch direction {
	case "inner", "internal":
		return false, "внутренние звонки не входят в поддерживаемый сценарий"
	}

	return true, ""
}

func shouldLinkTransferredCall(payload megafonVATSPayload) bool {
	return strings.EqualFold(strings.TrimSpace(payload.EventName), "TRANSFERRED") &&
		strings.TrimSpace(payload.SecondCallID) != "" &&
		strings.TrimSpace(payload.SecondCallID) != strings.TrimSpace(payload.CallID)
}

func resolveMegafonDirection(cmd string, payload megafonVATSPayload) string {
	if direction := strings.ToLower(strings.TrimSpace(payload.Direction)); direction != "" {
		return direction
	}
	if strings.TrimSpace(cmd) == telephony.IncomingEventCommandHistory {
		return strings.ToLower(strings.TrimSpace(payload.HistoryType))
	}
	return ""
}

func resolveMegafonEventType(cmd string, payload megafonVATSPayload) string {
	if strings.TrimSpace(cmd) == telephony.IncomingEventCommandHistory {
		return telephony.IncomingEventCommandHistory
	}
	return strings.ToUpper(strings.TrimSpace(payload.EventName))
}

func mergeMegafonCallSnapshots(target *telephony.Call, source *telephony.Call) {
	if target == nil || source == nil {
		return
	}
	if target.Direction == "" {
		target.Direction = source.Direction
	}
	target.Status = mergeMegafonEventStatus(target.Status, source.Status)
	target.MissedStatus = firstStringPtr(target.MissedStatus, source.MissedStatus)
	target.ClientPhone = firstStringPtr(target.ClientPhone, source.ClientPhone)
	target.VATNumber = firstStringPtr(target.VATNumber, source.VATNumber)
	target.EmployeeLogin = firstStringPtr(target.EmployeeLogin, source.EmployeeLogin)
	target.EmployeeUserID = firstUintPtr(target.EmployeeUserID, source.EmployeeUserID)
	target.GroupName = firstStringPtr(target.GroupName, source.GroupName)
	target.StartedAt = earlierTime(target.StartedAt, source.StartedAt)
	target.AnsweredAt = earlierTime(target.AnsweredAt, source.AnsweredAt)
	target.CompletedAt = laterTime(target.CompletedAt, source.CompletedAt)
	target.WaitSeconds = firstIntPtr(target.WaitSeconds, source.WaitSeconds)
	if source.DurationSeconds != nil && (target.DurationSeconds == nil || *source.DurationSeconds > *target.DurationSeconds) {
		target.DurationSeconds = source.DurationSeconds
	}
	if !target.HasRecording && source.HasRecording {
		target.HasRecording = true
		target.RecordingURL = source.RecordingURL
	}
	target.LastEventType = firstStringPtr(target.LastEventType, source.LastEventType)
	if strings.TrimSpace(target.RawSnapshot) == "" {
		target.RawSnapshot = source.RawSnapshot
	}
}

func mergeMegafonEventStatus(current string, next string) string {
	current = strings.ToLower(strings.TrimSpace(current))
	next = strings.ToLower(strings.TrimSpace(next))
	if next == "" {
		return current
	}
	if current == "" || megafonStatusRank(next) >= megafonStatusRank(current) {
		return next
	}
	return current
}

func megafonStatusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "completed", "cancelled", "failed", "busy", "missed", "noanswer":
		return 50
	case "transferred":
		return 30
	case "accepted":
		return 20
	case "incoming", "outgoing", "in", "out":
		return 10
	default:
		return 0
	}
}

func normalizeMegafonPhone(value string) string {
	digits := make([]rune, 0, len(value))
	for _, item := range strings.TrimSpace(value) {
		if item >= '0' && item <= '9' {
			digits = append(digits, item)
		}
	}
	if len(digits) == 0 {
		return ""
	}
	normalized := string(digits)
	if len(normalized) == 10 && strings.HasPrefix(normalized, "9") {
		return "7" + normalized
	}
	if len(normalized) == 11 && strings.HasPrefix(normalized, "8") {
		return "7" + normalized[1:]
	}
	return normalized
}

func looksLikeExternalPhone(value string) bool {
	return len(strings.TrimSpace(value)) >= 10
}

func parseMegafonVATSTime(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	for _, layout := range []string{
		"20060102T150405Z",
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
	} {
		parsed, err := time.Parse(layout, trimmed)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func parseMegafonVATSInt(value string) *int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return nil
	}
	return &parsed
}

func earlierTime(current *time.Time, candidate *time.Time) *time.Time {
	switch {
	case current == nil:
		return candidate
	case candidate == nil:
		return current
	case candidate.Before(*current):
		return candidate
	default:
		return current
	}
}

func laterTime(current *time.Time, candidate *time.Time) *time.Time {
	switch {
	case current == nil:
		return candidate
	case candidate == nil:
		return current
	case candidate.After(*current):
		return candidate
	default:
		return current
	}
}

func firstStringPtr(current *string, candidate *string) *string {
	if current != nil && strings.TrimSpace(*current) != "" {
		return current
	}
	return stringPtr(safeMegafonStringPointer(candidate))
}

func firstUintPtr(current *uint, candidate *uint) *uint {
	if current != nil && *current > 0 {
		return current
	}
	if candidate == nil || *candidate == 0 {
		return nil
	}
	return candidate
}

func firstIntPtr(current *int, candidate *int) *int {
	if current != nil {
		return current
	}
	return candidate
}

func isMegafonAnsweredHistoryStatus(status string, duration *int) bool {
	if duration != nil && *duration > 0 {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "completed", "accepted", "transferred":
		return true
	default:
		return false
	}
}

func shouldCreateMegafonPendingContext(call *telephony.Call) bool {
	if call == nil || call.EmployeeUserID == nil || *call.EmployeeUserID == 0 {
		return false
	}
	if strings.TrimSpace(safeMegafonStringPointer(call.ClientPhone)) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(call.Direction)) {
	case "incoming", "in":
	default:
		return false
	}
	if call.AnsweredAt != nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(call.Status)) {
	case "accepted", "success", "completed":
		return true
	default:
		return false
	}
}

func megafonPendingContextExpiresAt(call *telephony.Call) time.Time {
	base := time.Now()
	switch {
	case call != nil && call.AnsweredAt != nil:
		base = *call.AnsweredAt
	case call != nil && call.StartedAt != nil:
		base = *call.StartedAt
	}
	return base.Add(24 * time.Hour)
}

func callLockKey(value string) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(strings.TrimSpace(value)))
	return int64(hasher.Sum64() & 0x7fffffffffffffff)
}

func stringPtr(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func megafonAnyToString(value any) string {
	switch item := value.(type) {
	case string:
		return item
	case []byte:
		return string(item)
	default:
		return fmt.Sprintf("%v", item)
	}
}

func safeMegafonStringPointer(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
