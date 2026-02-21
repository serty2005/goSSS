package workers

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
	"strings"
	"time"
)

type DeferredTicketWorker interface {
	Start(ctx context.Context)
}

type deferredTicketWorkerImpl struct {
	log           logger.LoggerInterface
	ticketService services.TicketService
	bus           eventbus.EventBus
	interval      time.Duration
}

func NewDeferredTicketWorker(
	log logger.LoggerInterface,
	ticketService services.TicketService,
	bus eventbus.EventBus,
	interval time.Duration,
) DeferredTicketWorker {
	if interval <= 0 {
		interval = time.Minute
	}
	return &deferredTicketWorkerImpl{
		log:           log,
		ticketService: ticketService,
		bus:           bus,
		interval:      interval,
	}
}

func (w *deferredTicketWorkerImpl) Start(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.runCycle(ctx)

	for {
		select {
		case <-ticker.C:
			w.runCycle(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *deferredTicketWorkerImpl) runCycle(ctx context.Context) {
	activations, err := w.ticketService.ProcessExpiredDeferred(ctx, time.Now(), 200)
	if err != nil {
		w.log.Error("Не удалось обработать просроченные отложенные тикеты", "error", err)
		return
	}
	if len(activations) == 0 {
		return
	}
	for _, item := range activations {
		ticketID := strings.TrimSpace(item.TicketID)
		if ticketID == "" || w.bus == nil {
			continue
		}

		w.bus.Publish(eventbus.Event{
			Type: events.BitrixTicketSyncRequested,
			Payload: events.BitrixSyncEntityPayload{
				TicketID: ticketID,
				Reason:   "ticket_deferred_due",
			},
		})

		if item.RecipientUserID == 0 {
			continue
		}
		recipientID := item.RecipientUserID
		w.bus.Publish(eventbus.Event{
			Type: events.TicketUpdated,
			Payload: events.TicketUpdatedPayload{
				TicketID:        ticketID,
				Action:          "ticket_deferred_due",
				Source:          "system",
				Message:         "Истекло время статуса \"Отложено\", тикет переведён в работу",
				OccurredAt:      time.Now(),
				RecipientUserID: &recipientID,
			},
		})
	}
}
