package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"fmt"
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
	cfg    *config.Config
	log    logger.LoggerInterface
	client *pyrusplugin.Client
	redis  *redis.Client
	repo   pyrus.Repository
}

func NewPyrusSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client *pyrusplugin.Client,
	redisClient *redis.Client,
	repo pyrus.Repository,
) PyrusSyncService {
	return &pyrusSyncService{
		cfg:    cfg,
		log:    log,
		client: client,
		redis:  redisClient,
		repo:   repo,
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

	return s.repo.InsertOutgoingEvent(ctx, item)
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
		s.log.Warn("Pyrus: исходящая синхронизация отключена, клиент API не настроен")
		return
	}

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
	if err := s.repo.MarkOutgoingProcessing(ctx, item.ID); err != nil {
		s.log.Warn("Pyrus: не удалось отметить исходящее событие как processing", "event_id", item.ID, "error", err)
	}

	status, reason, err := s.handleOutgoingEvent(ctx, item)
	if err != nil {
		_ = s.repo.MarkOutgoingFailed(ctx, item.ID, err.Error())
		return
	}
	if status == pyrus.OutgoingEventStatusIgnored {
		_ = s.repo.MarkOutgoingIgnored(ctx, item.ID, reason)
		return
	}
	_ = s.repo.MarkOutgoingDone(ctx, item.ID)
}

func (s *pyrusSyncService) handleOutgoingEvent(ctx context.Context, item *pyrus.OutgoingEvent) (string, string, error) {
	payload := events.PyrusSyncEntityPayload{}
	if err := json.Unmarshal([]byte(item.PayloadJSON), &payload); err != nil {
		return "", "", err
	}

	switch strings.TrimSpace(item.EventName) {
	case events.PyrusTicketExtIDSyncRequested:
		return s.handleExtIDSync(ctx, item, payload)
	case events.PyrusTicketSyncRequested,
		events.PyrusCommentSyncRequested,
		events.PyrusTicketStatusSyncRequested,
		events.PyrusTicketAssigneeSyncRequested:
		return pyrus.OutgoingEventStatusIgnored, "исходящий сценарий будет завершён на phase 2", nil
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
	return pyrus.OutgoingEventStatusDone, "", nil
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
