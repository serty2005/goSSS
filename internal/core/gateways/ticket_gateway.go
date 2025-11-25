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
		tickets.StatusResummed,
	}

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

	activeRemoteUUIDs := make(map[string]struct{})
	countUpserted := 0
	countRestored := 0

	for _, rawData := range rawTickets {
		ticket, err := g.sdClient.Mapper().DataToTicket(ctx, mapperCtx, rawData)
		if err != nil {
			g.logger.Warn("Ошибка маппинга заявки", "data", rawData, "error", err)
			continue
		}

		activeRemoteUUIDs[ticket.ServiceDeskUUID] = struct{}{}

		// 1. Ищем существующую связь
		link, _ := g.linkRepo.GetByExternalID(ctx, nil, "naumen", ticket.ServiceDeskUUID)

		if link != nil {
			// Связь есть, обновляем тикет
			ticket.ID = link.InternalID
		} else {
			// 2. Связи нет. Ищем по бизнес-ключу (Номеру), чтобы избежать дублей
			existingTicket, err := g.ticketRepo.GetByNumber(ctx, ticket.Number)
			if err == nil && existingTicket != nil {
				// Нашли "сироту"! Восстанавливаем связь
				g.logger.Info("Найден существующий тикет без связи. Восстановление...", "number", ticket.Number)
				ticket.ID = existingTicket.ID

				newLink := &models.ExternalSystemLink{
					InternalID:      ticket.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: ticket.ServiceDeskUUID,
					EntityType:      "Ticket",
					LastSyncedAt:    time.Now(),
				}
				if err := g.linkRepo.Create(ctx, nil, newLink); err == nil {
					link = newLink
					countRestored++
				}
			}
		}

		// 3. Сохраняем (Create или Update)
		if err := g.ticketRepo.Upsert(ctx, ticket); err != nil {
			g.logger.Error("Ошибка сохранения заявки в БД", "uuid", ticket.ServiceDeskUUID, "error", err)
			continue
		}

		// 4. Если тикет был создан с нуля (ticket.ID заполнился после Upsert), создаем связь
		if link == nil && ticket.ID != "" {
			newLink := &models.ExternalSystemLink{
				InternalID:      ticket.ID,
				SystemName:      "naumen",
				ServiceDeskUUID: ticket.ServiceDeskUUID,
				EntityType:      "Ticket",
				LastSyncedAt:    time.Now(),
			}
			g.linkRepo.Create(ctx, nil, newLink)
		} else if link != nil {
			link.LastSyncedAt = time.Now()
			g.db.Save(link)
		}

		countUpserted++
	}

	// --- Zombie Killer ---
	localActiveTickets, err := g.ticketRepo.GetActive(ctx)
	if err != nil {
		g.logger.Error("Не удалось получить список локальных активных заявок", "error", err)
		return
	}

	zombiesCount := 0
	for _, localT := range localActiveTickets {
		// Теперь ServiceDeskUUID будет заполнен благодаря gorm:"<-:false" и JOIN в GetActive
		if localT.ServiceDeskUUID == "" {
			continue
		}

		if _, exists := activeRemoteUUIDs[localT.ServiceDeskUUID]; !exists {
			g.logger.Info("Заявка исчезла из активных в SD. Проверка статуса...", "uuid", localT.ServiceDeskUUID)

			details, err := g.sdClient.FetchEntityDetails(ctx, localT.ServiceDeskUUID, "Ticket")
			if err != nil {
				g.logger.Error("Не удалось проверить статус зомби-заявки", "uuid", localT.ServiceDeskUUID, "error", err)
				continue
			}

			if newState, ok := details["state"].(string); ok && newState != localT.Status {
				g.logger.Info("Обновление статуса зомби-заявки", "uuid", localT.ServiceDeskUUID, "new_status", newState)
				localT.Status = newState
				if err := g.ticketRepo.Upsert(ctx, &localT); err == nil {
					zombiesCount++
					g.bus.Publish(eventbus.Event{
						Type: events.TicketUpdated,
						Payload: map[string]interface{}{
							"internal_id": localT.ID,
							"sd_uuid":     localT.ServiceDeskUUID,
							"new_status":  newState,
						},
					})
				}
			}
		}
	}

	g.logger.Info("Синхронизация заявок завершена",
		"upserted", countUpserted,
		"restored_links", countRestored,
		"zombies_fixed", zombiesCount)
}
