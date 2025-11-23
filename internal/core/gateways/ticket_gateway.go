package gateways

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"time"

	"gorm.io/gorm"
)

// TicketGateway отвечает за синхронизацию заявок из ServiceDesk.
type TicketGateway interface {
	Start(ctx context.Context)
}

type ticketGatewayImpl struct {
	cfg        *config.Config
	logger     logger.LoggerInterface
	sdClient   external.ExternalSystemClient
	ticketRepo tickets.TicketRepository
	// Зависимости для MapperContext (пока оставляем DB здесь только для маппера,
	// но бизнес-логику через него не делаем)
	bus      eventbus.EventBus
	db       *gorm.DB
	linkRepo repositories.LinkRepo
}

// NewTicketGateway создает новый экземпляр шлюза.
func NewTicketGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	sdClient external.ExternalSystemClient,
	ticketRepo tickets.TicketRepository,
	bus eventbus.EventBus,
	db *gorm.DB,
	linkRepo repositories.LinkRepo,
) TicketGateway {
	return &ticketGatewayImpl{
		cfg:        cfg,
		logger:     logger,
		sdClient:   sdClient,
		ticketRepo: ticketRepo,
		bus:        bus,
		db:         db,
		linkRepo:   linkRepo,
	}
}

func (g *ticketGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.SDeskSyncInterval
	if interval < 1*time.Minute {
		interval = 5 * time.Minute
	}

	g.logger.Info("Запуск шлюза синхронизации заявок (Tickets)", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Запускаем первый раз немедленно
	g.syncTickets(ctx)

	for {
		select {
		case <-ticker.C:
			g.syncTickets(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза заявок.")
			return
		}
	}
}

func (g *ticketGatewayImpl) syncTickets(ctx context.Context) {
	g.logger.Info("Начало синхронизации заявок из ServiceDesk...")

	targetStatuses := []string{
		tickets.StatusRegistered,
		tickets.StatusInProgress,
		tickets.StatusWaitClientAnswer,
	}

	// 1. Получаем активные заявки из внешней системы
	rawTickets, err := g.sdClient.FetchTickets(ctx, targetStatuses)
	if err != nil {
		g.logger.Error("Ошибка получения списка заявок из SD", "error", err)
		return
	}

	g.logger.Info("Получено активных заявок из SD", "count", len(rawTickets))

	mapperCtx := &external.MapperContext{
		DB:       g.db,
		LinkRepo: g.linkRepo,
		Logger:   g.logger,
	}

	// Сет для хранения UUID активных заявок, которые пришли от Naumen
	activeRemoteUUIDs := make(map[string]struct{})
	countUpserted := 0

	for _, rawData := range rawTickets {
		ticket, err := g.sdClient.Mapper().DataToTicket(ctx, mapperCtx, rawData)
		if err != nil {
			g.logger.Warn("Ошибка маппинга заявки", "data", rawData, "error", err)
			continue
		}

		// Сохраняем UUID в сет для проверки зомби
		activeRemoteUUIDs[ticket.ServiceDeskUUID] = struct{}{}

		// 1. Ищем связь в локальной БД
		link, _ := g.linkRepo.GetByExternalID(ctx, nil, "naumen", ticket.ServiceDeskUUID)
		if link != nil {
			// 2a. Если связь есть - обновляем существующий тикет
			ticket.ID = link.InternalID
		}
		// 2b. Если связи нет - ticket.ID останется пустым, Upsert создаст новый.

		if err := g.ticketRepo.Upsert(ctx, ticket); err != nil {
			g.logger.Error("Ошибка сохранения заявки в БД", "uuid", ticket.ServiceDeskUUID, "error", err)
			continue
		}

		// 3. Если создали новый (link был nil), нужно создать связь
		if link == nil && ticket.ID != "" {
			newLink := &models.ExternalSystemLink{
				InternalID:      ticket.ID,
				SystemName:      "naumen",
				ServiceDeskUUID: ticket.ServiceDeskUUID,
				EntityType:      "Ticket", // Или serviceCall, главное консистентно
				LastSyncedAt:    time.Now(),
			}
			g.linkRepo.Create(ctx, nil, newLink)
		} else if link != nil {
			// Обновим время синхронизации
			link.LastSyncedAt = time.Now()
			g.db.Save(link)
		}

		countUpserted++
	}

	// --- ЛОГИКА ZOMBIE KILLER ---
	// Ищем в локальной БД заявки, которые у нас числятся активными,
	// но которых нет в списке, пришедшем от Naumen.

	// ИСПОЛЬЗУЕМ РЕПОЗИТОРИЙ ВМЕСТО ПРЯМОГО SQL
	localActiveTickets, err := g.ticketRepo.GetActive(ctx)
	if err != nil {
		g.logger.Error("Не удалось получить список локальных активных заявок", "error", err)
		return
	}

	zombiesCount := 0
	for _, localT := range localActiveTickets {
		if _, exists := activeRemoteUUIDs[localT.ServiceDeskUUID]; !exists {
			// Заявка была активна у нас, но не пришла в списке активных из Naumen.
			// Значит, она либо закрыта, либо удалена, либо перешла в другой статус.

			g.logger.Info("Обнаружена заявка, исчезнувшая из активных. Проверка статуса...", "uuid", localT.ServiceDeskUUID)

			// Точечно запрашиваем статус из Naumen, чтобы узнать точный конечный статус
			link, _ := g.linkRepo.GetByInternalID(ctx, nil, "naumen", localT.ID)
			if link == nil {
				g.logger.Warn("Связь не найдена для заявки, пропускаем обработку", "internal_id", localT.ID)
				continue
			}
			details, err := g.sdClient.FetchEntityDetails(ctx, link.ServiceDeskUUID, "Ticket")
			if err != nil {
				g.logger.Error("Не удалось проверить статус исчезнувшей заявки", "uuid", localT.ServiceDeskUUID, "error", err)
				continue
			}

			// Обновляем статус в БД
			if newState, ok := details["state"].(string); ok {
				g.logger.Info("Обновление статуса исчезнувшей заявки", "uuid", localT.ServiceDeskUUID, "old_status", localT.Status, "new_status", newState)

				localT.Status = newState
				// Используем Upsert для сохранения нового статуса
				if err := g.ticketRepo.Upsert(ctx, &localT); err != nil {
					g.logger.Error("Не удалось обновить статус зомби-заявки", "uuid", localT.ServiceDeskUUID, "error", err)
				} else {
					zombiesCount++

					g.bus.Publish(eventbus.Event{
						Type: events.TicketUpdated,
						Payload: map[string]interface{}{
							"internal_id": localT.ID,
							"sd_uuid":     link.ServiceDeskUUID,
							"new_status":  newState,
							"updated_by":  "ticket_gateway",
						},
					})
				}
			}
		}
	}

	g.logger.Info("Синхронизация заявок завершена", "upserted", countUpserted, "zombies_fixed", zombiesCount)
}
