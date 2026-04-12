package services

import (
	"context"
	"errors"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/telephony"
	"fmt"
	"strings"
	"time"
)

var (
	ErrIntegrationProviderNotSupported = errors.New("провайдер интеграции не поддерживается")
	ErrIntegrationEventNotFound        = errors.New("событие интеграции не найдено")
)

type IntegrationSyncEventListFilter struct {
	Status []string
	Limit  int
	Offset int
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
			PayloadRaw:               item.PayloadRaw,
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
