package integrations

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
)

func RegisterBitrixEventHandlers(
	cfg *config.Config,
	log logger.LoggerInterface,
	bus eventbus.EventBus,
	bitrixSync services.BitrixSyncService,
) {
	if cfg == nil || bus == nil || bitrixSync == nil {
		return
	}
	if !cfg.EnableBitrixGateway {
		log.Info("Bitrix24: мост событий отключен конфигурацией")
		return
	}
	if !bitrixSync.IsEnabled() {
		log.Info("Bitrix24: мост событий не запущен, синхронизация недоступна")
		return
	}

	bus.Subscribe(events.BitrixTicketSyncRequested, func(ctx context.Context, event eventbus.Event) {
		payload, ok := event.Payload.(events.BitrixSyncEntityPayload)
		if !ok || payload.TicketID == "" {
			return
		}
		if err := bitrixSync.SyncTicketByID(ctx, payload.TicketID); err != nil {
			log.Error("Bitrix24: ошибка синхронизации тикета по событию", "ticket_id", payload.TicketID, "reason", payload.Reason, "error", err)
		}
	})

	bus.Subscribe(events.BitrixCommentSyncRequested, func(ctx context.Context, event eventbus.Event) {
		payload, ok := event.Payload.(events.BitrixSyncEntityPayload)
		if !ok || payload.TicketID == "" || payload.Comment == nil || payload.Comment.ID == "" || payload.EtalonUserID == nil {
			return
		}
		if err := bitrixSync.SyncComment(ctx, payload.TicketID, payload.Comment, *payload.EtalonUserID); err != nil {
			log.Error("Bitrix24: ошибка синхронизации комментария по событию", "ticket_id", payload.TicketID, "comment_id", payload.Comment.ID, "error", err)
		}
	})

	log.Info("Bitrix24: мост событий зарегистрирован")
}
