package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/telephony"
	"fmt"
	"regexp"
	"strings"
	"time"

	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
)

var (
	ErrIntegrationProviderNotSupported = errors.New("провайдер интеграции не поддерживается")
	ErrIntegrationEventNotFound        = errors.New("событие интеграции не найдено")
)

type IntegrationSyncEventListFilter struct {
	Status []string
	// TaskID ограничивает входящие события Pyrus одной задачей; 0 - без ограничения.
	TaskID int64
	Limit  int
	Offset int
}

// PyrusIncomingTaskItem - сводка входящих событий Pyrus по одной задаче с данными, нужными оператору для разбора.
type PyrusIncomingTaskItem struct {
	TaskID          int64            `json:"task_id"`
	Subject         string           `json:"subject,omitempty"`
	CRMID           string           `json:"crm_id,omitempty"`
	ClientName      string           `json:"client_name,omitempty"`
	TicketID        *string          `json:"ticket_id,omitempty"`
	EventsTotal     int64            `json:"events_total"`
	StatusCounts    map[string]int64 `json:"status_counts"`
	NeedsAttention  bool             `json:"needs_attention"`
	FirstReceivedAt time.Time        `json:"first_received_at"`
	LastReceivedAt  time.Time        `json:"last_received_at"`
	LastError       *string          `json:"last_error,omitempty"`
	NextRetryAt     *time.Time       `json:"next_retry_at,omitempty"`
}

// PyrusIncomingTaskListFilter ограничивает выборку сводки по задачам Pyrus.
type PyrusIncomingTaskListFilter struct {
	OnlyProblem bool
	Limit       int
	Offset      int
}

type IntegrationSyncEventItem struct {
	ID               string     `json:"id"`
	Provider         string     `json:"provider"`
	Direction        string     `json:"direction"`
	EventName        string     `json:"event_name"`
	TicketID         *string    `json:"ticket_id,omitempty"`
	ExternalEntityID *string    `json:"external_entity_id,omitempty"`
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	LastError        *string    `json:"last_error,omitempty"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	ReplayCount      int        `json:"replay_count,omitempty"`
	ReceivedAt       *time.Time `json:"received_at,omitempty"`
	QueuedAt         *time.Time `json:"queued_at,omitempty"`
	ProcessedAt      *time.Time `json:"processed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type IntegrationSyncEventDetail struct {
	IntegrationSyncEventItem
	PayloadRaw  string `json:"payload_raw,omitempty"`
	PayloadJSON string `json:"payload_json,omitempty"`
}

type IntegrationSyncControlService interface {
	ListIncomingEvents(ctx context.Context, provider string, filter IntegrationSyncEventListFilter) ([]IntegrationSyncEventItem, int64, error)
	ListOutgoingEvents(ctx context.Context, provider string, filter IntegrationSyncEventListFilter) ([]IntegrationSyncEventItem, int64, error)
	GetIncomingEvent(ctx context.Context, provider string, id string) (*IntegrationSyncEventDetail, error)
	GetOutgoingEvent(ctx context.Context, provider string, id string) (*IntegrationSyncEventDetail, error)
	ReplayIncomingEvent(ctx context.Context, provider string, id string) error
	// ListIncomingTasks возвращает сводку входящих событий по задачам внешней системы (поддерживается только Pyrus).
	ListIncomingTasks(ctx context.Context, provider string, filter PyrusIncomingTaskListFilter) ([]PyrusIncomingTaskItem, int64, error)
	// ReplayIncomingTask повторно запускает все неуспешные события одной задачи и возвращает их число.
	ReplayIncomingTask(ctx context.Context, provider string, taskID int64) (int, error)
	// ReplayProblemIncomingTasks повторно запускает неуспешные события всех задач; возвращает число задач и событий.
	ReplayProblemIncomingTasks(ctx context.Context, provider string) (int, int, error)
}

type integrationSyncControlService struct {
	pyrusRepo           pyrus.Repository
	pyrusIncoming       PyrusIncomingService
	telephonyRepo       telephony.Repository
	megafonVATSIncoming MegafonVATSIncomingService
}

func NewIntegrationSyncControlService(
	pyrusRepo pyrus.Repository,
	pyrusIncoming PyrusIncomingService,
	telephonyRepo telephony.Repository,
	megafonVATSIncoming MegafonVATSIncomingService,
) IntegrationSyncControlService {
	return &integrationSyncControlService{
		pyrusRepo:           pyrusRepo,
		pyrusIncoming:       pyrusIncoming,
		telephonyRepo:       telephonyRepo,
		megafonVATSIncoming: megafonVATSIncoming,
	}
}

func (s *integrationSyncControlService) ListIncomingEvents(
	ctx context.Context,
	provider string,
	filter IntegrationSyncEventListFilter,
) ([]IntegrationSyncEventItem, int64, error) {
	switch normalizeIntegrationProvider(provider) {
	case "pyrus":
		if s.pyrusRepo == nil {
			return nil, 0, fmt.Errorf("репозиторий Pyrus не настроен")
		}
		items, total, err := s.pyrusRepo.ListIncomingEvents(ctx, pyrus.IncomingEventListFilter{
			Status: filter.Status,
			TaskID: filter.TaskID,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		result := make([]IntegrationSyncEventItem, 0, len(items))
		for i := range items {
			result = append(result, mapPyrusIncomingEvent(items[i]))
		}
		return result, total, nil
	case "megafon-vats":
		if s.telephonyRepo == nil {
			return nil, 0, fmt.Errorf("репозиторий телефонии не настроен")
		}
		items, total, err := s.telephonyRepo.ListIncomingEvents(ctx, telephony.IncomingEventListFilter{
			Status: filter.Status,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		result := make([]IntegrationSyncEventItem, 0, len(items))
		for i := range items {
			result = append(result, mapMegafonIncomingEvent(items[i]))
		}
		return result, total, nil
	default:
		return nil, 0, ErrIntegrationProviderNotSupported
	}
}

func (s *integrationSyncControlService) ListOutgoingEvents(
	ctx context.Context,
	provider string,
	filter IntegrationSyncEventListFilter,
) ([]IntegrationSyncEventItem, int64, error) {
	switch normalizeIntegrationProvider(provider) {
	case "pyrus":
		if s.pyrusRepo == nil {
			return nil, 0, fmt.Errorf("репозиторий Pyrus не настроен")
		}
		items, total, err := s.pyrusRepo.ListOutgoingEvents(ctx, pyrus.OutgoingEventListFilter{
			Status: filter.Status,
			Limit:  filter.Limit,
			Offset: filter.Offset,
		})
		if err != nil {
			return nil, 0, err
		}
		result := make([]IntegrationSyncEventItem, 0, len(items))
		for i := range items {
			result = append(result, mapPyrusOutgoingEvent(items[i]))
		}
		return result, total, nil
	default:
		return nil, 0, ErrIntegrationProviderNotSupported
	}
}

func (s *integrationSyncControlService) GetIncomingEvent(
	ctx context.Context,
	provider string,
	id string,
) (*IntegrationSyncEventDetail, error) {
	switch normalizeIntegrationProvider(provider) {
	case "pyrus":
		if s.pyrusRepo == nil {
			return nil, fmt.Errorf("репозиторий Pyrus не настроен")
		}
		item, err := s.pyrusRepo.GetIncomingEventByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, ErrIntegrationEventNotFound
		}
		result := &IntegrationSyncEventDetail{
			IntegrationSyncEventItem: mapPyrusIncomingEvent(*item),
			PayloadRaw:               redactPyrusPayload(item.PayloadRaw),
		}
		return result, nil
	case "megafon-vats":
		if s.telephonyRepo == nil {
			return nil, fmt.Errorf("репозиторий телефонии не настроен")
		}
		item, err := s.telephonyRepo.GetIncomingEventByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, ErrIntegrationEventNotFound
		}
		return &IntegrationSyncEventDetail{
			IntegrationSyncEventItem: mapMegafonIncomingEvent(*item),
			PayloadRaw:               item.PayloadRaw,
		}, nil
	default:
		return nil, ErrIntegrationProviderNotSupported
	}
}

func (s *integrationSyncControlService) GetOutgoingEvent(
	ctx context.Context,
	provider string,
	id string,
) (*IntegrationSyncEventDetail, error) {
	switch normalizeIntegrationProvider(provider) {
	case "pyrus":
		if s.pyrusRepo == nil {
			return nil, fmt.Errorf("репозиторий Pyrus не настроен")
		}
		item, err := s.pyrusRepo.GetOutgoingEventByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, ErrIntegrationEventNotFound
		}
		result := &IntegrationSyncEventDetail{
			IntegrationSyncEventItem: mapPyrusOutgoingEvent(*item),
			PayloadJSON:              item.PayloadJSON,
		}
		return result, nil
	default:
		return nil, ErrIntegrationProviderNotSupported
	}
}

func (s *integrationSyncControlService) ReplayIncomingEvent(ctx context.Context, provider string, id string) error {
	switch normalizeIntegrationProvider(provider) {
	case "pyrus":
		if s.pyrusIncoming == nil {
			return fmt.Errorf("входящий Pyrus worker не настроен")
		}
		return s.pyrusIncoming.ReplayEvent(ctx, id)
	case "megafon-vats":
		if s.megafonVATSIncoming == nil {
			return fmt.Errorf("входящий worker Мегафон ВАТС не настроен")
		}
		return s.megafonVATSIncoming.ReplayEvent(ctx, id)
	default:
		return ErrIntegrationProviderNotSupported
	}
}

func (s *integrationSyncControlService) ListIncomingTasks(
	ctx context.Context,
	provider string,
	filter PyrusIncomingTaskListFilter,
) ([]PyrusIncomingTaskItem, int64, error) {
	if normalizeIntegrationProvider(provider) != "pyrus" {
		return nil, 0, ErrIntegrationProviderNotSupported
	}
	if s.pyrusRepo == nil {
		return nil, 0, fmt.Errorf("репозиторий Pyrus не настроен")
	}
	groups, total, err := s.pyrusRepo.ListIncomingTaskGroups(ctx, pyrus.IncomingTaskGroupFilter{
		OnlyProblem: filter.OnlyProblem,
		Limit:       filter.Limit,
		Offset:      filter.Offset,
	})
	if err != nil {
		return nil, 0, err
	}

	lastEventIDs := make([]string, 0, len(groups))
	for i := range groups {
		lastEventIDs = append(lastEventIDs, groups[i].LastEventID)
	}
	lastEvents, err := s.pyrusRepo.GetIncomingEventsByIDs(ctx, lastEventIDs)
	if err != nil {
		return nil, 0, err
	}
	eventsByID := make(map[string]pyrus.IncomingEvent, len(lastEvents))
	for i := range lastEvents {
		eventsByID[lastEvents[i].ID] = lastEvents[i]
	}

	result := make([]PyrusIncomingTaskItem, 0, len(groups))
	for i := range groups {
		group := groups[i]
		item := PyrusIncomingTaskItem{
			TaskID:      group.PyrusTaskID,
			TicketID:    group.TicketID,
			EventsTotal: group.EventsTotal,
			StatusCounts: map[string]int64{
				pyrus.IncomingEventStatusNew:        group.NewCount,
				pyrus.IncomingEventStatusQueued:     group.QueuedCount,
				pyrus.IncomingEventStatusProcessing: group.ProcessingCount,
				pyrus.IncomingEventStatusWaiting:    group.WaitingCount,
				pyrus.IncomingEventStatusFailed:     group.FailedCount,
				pyrus.IncomingEventStatusDone:       group.DoneCount,
				pyrus.IncomingEventStatusIgnored:    group.IgnoredCount,
			},
			NeedsAttention:  group.FailedCount+group.WaitingCount > 0,
			FirstReceivedAt: group.FirstReceivedAt,
			LastReceivedAt:  group.LastReceivedAt,
			LastError:       group.LastError,
			NextRetryAt:     group.NextRetryAt,
		}
		if event, ok := eventsByID[group.LastEventID]; ok {
			if payload, parseErr := pyrusplugin.ParseWebhookPayload([]byte(event.PayloadRaw)); parseErr == nil {
				taskContext := buildPyrusTaskContext(&payload.Task)
				item.Subject = strings.TrimSpace(taskContext.Subject)
				item.CRMID = strings.TrimSpace(taskContext.CRMID)
				item.ClientName = resolvePyrusTaskClientNameFromContext(taskContext)
			}
		}
		result = append(result, item)
	}
	return result, total, nil
}

func (s *integrationSyncControlService) ReplayIncomingTask(ctx context.Context, provider string, taskID int64) (int, error) {
	if normalizeIntegrationProvider(provider) != "pyrus" {
		return 0, ErrIntegrationProviderNotSupported
	}
	if s.pyrusIncoming == nil {
		return 0, fmt.Errorf("входящий Pyrus worker не настроен")
	}
	return s.pyrusIncoming.ReplayTask(ctx, taskID)
}

func (s *integrationSyncControlService) ReplayProblemIncomingTasks(ctx context.Context, provider string) (int, int, error) {
	if normalizeIntegrationProvider(provider) != "pyrus" {
		return 0, 0, ErrIntegrationProviderNotSupported
	}
	if s.pyrusIncoming == nil {
		return 0, 0, fmt.Errorf("входящий Pyrus worker не настроен")
	}
	return s.pyrusIncoming.ReplayProblemTasks(ctx)
}

var pyrusAccessTokenPattern = regexp.MustCompile(`("access_token"\s*:\s*)"[^"]*"`)

// redactPyrusPayload скрывает одноразовый access_token Pyrus из payload, который показывается в административном интерфейсе.
func redactPyrusPayload(raw string) string {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err == nil {
		if _, ok := fields["access_token"]; !ok {
			return raw
		}
		fields["access_token"] = json.RawMessage(`"[скрыто]"`)
		if redacted, marshalErr := json.Marshal(fields); marshalErr == nil {
			return string(redacted)
		}
	}
	return pyrusAccessTokenPattern.ReplaceAllString(raw, `$1"[скрыто]"`)
}

func normalizeIntegrationProvider(provider string) string {
	normalized := strings.TrimSpace(strings.ToLower(provider))
	normalized = strings.ReplaceAll(normalized, "_", "-")
	switch normalized {
	case "megafonvats":
		return "megafon-vats"
	default:
		return normalized
	}
}

func mapPyrusIncomingEvent(item pyrus.IncomingEvent) IntegrationSyncEventItem {
	result := IntegrationSyncEventItem{
		ID:          item.ID,
		Provider:    "pyrus",
		Direction:   "incoming",
		EventName:   item.EventName,
		Status:      item.Status,
		Attempts:    item.Attempts,
		LastError:   item.LastError,
		NextRetryAt: item.NextRetryAt,
		ReplayCount: item.ReplayCount,
		ReceivedAt:  &item.ReceivedAt,
		ProcessedAt: item.ProcessedAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
	if item.PyrusTaskID != nil && *item.PyrusTaskID > 0 {
		externalID := fmt.Sprintf("task:%d", *item.PyrusTaskID)
		result.ExternalEntityID = &externalID
	}
	return result
}

func mapPyrusOutgoingEvent(item pyrus.OutgoingEvent) IntegrationSyncEventItem {
	result := IntegrationSyncEventItem{
		ID:          item.ID,
		Provider:    "pyrus",
		Direction:   "outgoing",
		EventName:   item.EventName,
		TicketID:    item.TicketID,
		Status:      item.Status,
		Attempts:    item.Attempts,
		LastError:   item.LastError,
		QueuedAt:    &item.QueuedAt,
		ProcessedAt: item.ProcessedAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
	if item.PyrusTaskID != nil && *item.PyrusTaskID > 0 {
		externalID := fmt.Sprintf("task:%d", *item.PyrusTaskID)
		result.ExternalEntityID = &externalID
	}
	return result
}

func mapMegafonIncomingEvent(item telephony.IncomingEvent) IntegrationSyncEventItem {
	result := IntegrationSyncEventItem{
		ID:          item.ID,
		Provider:    "megafon-vats",
		Direction:   "incoming",
		EventName:   mapMegafonIncomingEventName(item),
		Status:      item.Status,
		Attempts:    item.Attempts,
		LastError:   item.LastError,
		ReceivedAt:  &item.ReceivedAt,
		ProcessedAt: item.ProcessedAt,
		CreatedAt:   item.CreatedAt,
		UpdatedAt:   item.UpdatedAt,
	}
	if strings.TrimSpace(item.ExternalCallID) != "" {
		externalID := "call:" + strings.TrimSpace(item.ExternalCallID)
		result.ExternalEntityID = &externalID
	}
	return result
}

func mapMegafonIncomingEventName(item telephony.IncomingEvent) string {
	cmd := strings.TrimSpace(item.Cmd)
	eventName := strings.TrimSpace(item.EventName)
	if cmd == "" {
		return eventName
	}
	if eventName == "" {
		return cmd
	}
	return cmd + ":" + eventName
}
