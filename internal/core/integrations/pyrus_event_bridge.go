package integrations

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
)

func RegisterPyrusEventHandlers(
	cfg *config.Config,
	log logger.LoggerInterface,
	bus eventbus.EventBus,
	pyrusSync services.PyrusSyncService,
) {
	if cfg == nil || bus == nil || pyrusSync == nil {
		return
	}
	if !cfg.EnablePyrusGateway {
		log.Info("Pyrus: мост событий отключен конфигурацией")
		return
	}
	if !pyrusSync.IsEnabled() {
		log.Info("Pyrus: мост событий не запущен, синхронизация недоступна")
		return
	}

	register := func(eventType string) {
		bus.Subscribe(eventType, func(ctx context.Context, event eventbus.Event) {
			payload, ok := event.Payload.(events.PyrusSyncEntityPayload)
			if !ok {
				return
			}
			if err := pyrusSync.EnqueueEvent(ctx, eventType, payload); err != nil {
				log.Error("Pyrus: ошибка enqueue исходящего события", "event_type", eventType, "ticket_id", payload.TicketID, "task_id", payload.TaskID, "error", err)
			}
		})
	}

	register(events.PyrusTicketSyncRequested)
	register(events.PyrusCommentSyncRequested)
	register(events.PyrusTicketStatusSyncRequested)
	register(events.PyrusTicketAssigneeSyncRequested)
	register(events.PyrusTicketExtIDSyncRequested)

	log.Info("Pyrus: мост событий зарегистрирован")
}
