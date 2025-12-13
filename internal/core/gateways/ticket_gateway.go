package gateways

import (
	"context"
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
	bus        eventbus.EventBus
	db         *gorm.DB
	linkRepo   repositories.LinkRepo
}

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

	// Используем строковые статусы из Naumen, маппинг происходит в сервисе или здесь
	targetStatuses := []string{
		"registered", "inprogress", "waitClientAnswer", "resumed",
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
			ticket.ID = link.InternalID
			// При обновлении используем Update метод репозитория
			if err := g.ticketRepo.Update(ctx, ticket); err != nil {
				g.logger.Error("Ошибка обновления заявки", "id", ticket.ID, "error", err)
			}
		} else {
			// 2. Связи нет. Ищем по бизнес-ключу (Номеру)
			existingTicket, err := g.ticketRepo.GetByNumber(ctx, ticket.Number)
			if err == nil && existingTicket != nil {
				g.logger.Info("Найден существующий тикет без связи. Восстановление...", "number", ticket.Number)
				ticket.ID = existingTicket.ID

				// Восстанавливаем связь
				newLink := &models.ExternalSystemLink{
					InternalID:      ticket.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: ticket.ServiceDeskUUID,
					EntityType:      "Ticket",
					LastSyncedAt:    time.Now(),
				}
				g.linkRepo.Create(ctx, nil, newLink)
				countRestored++

				g.ticketRepo.Update(ctx, ticket)
			} else {
				// 3. Создаем новый
				if err := g.ticketRepo.Create(ctx, ticket); err != nil {
					g.logger.Error("Ошибка создания заявки", "uuid", ticket.ServiceDeskUUID, "error", err)
					continue
				}
				// Создаем связь для нового
				newLink := &models.ExternalSystemLink{
					InternalID:      ticket.ID,
					SystemName:      "naumen",
					ServiceDeskUUID: ticket.ServiceDeskUUID,
					EntityType:      "Ticket",
					LastSyncedAt:    time.Now(),
				}
				g.linkRepo.Create(ctx, nil, newLink)
			}
		}
		countUpserted++
	}

	// --- Zombie Killer (Закрываем заявки, которые пропали из выдачи SD) ---
	// В ТЗ статус Closed = "closed"

	// Здесь потребуется доп. метод в репозитории для получения активных заявок (GetActive)
	// Пока пропустим этот шаг или реализуем его позже, чтобы не усложнять текущий фикс.

	g.logger.Info("Синхронизация заявок завершена", "upserted", countUpserted, "restored_links", countRestored)
}
