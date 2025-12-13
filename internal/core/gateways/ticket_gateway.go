package gateways

import (
	"context"
	"etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"time"

	"gorm.io/gorm"
)

// TicketGateway отвечает за синхронизацию заявок.
type TicketGateway interface {
	Start(ctx context.Context)
}

type ticketGatewayImpl struct {
	cfg        *config.Config
	logger     logger.LoggerInterface
	manager    *integrations.Manager
	ticketRepo tickets.TicketRepository
	bus        eventbus.EventBus
	db         *gorm.DB
	linkRepo   repositories.LinkRepo
}

func NewTicketGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	manager *integrations.Manager,
	ticketRepo tickets.TicketRepository,
	bus eventbus.EventBus,
	db *gorm.DB,
	linkRepo repositories.LinkRepo,
) TicketGateway {
	return &ticketGatewayImpl{
		cfg:        cfg,
		logger:     logger,
		manager:    manager,
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
	g.logger.Info("Начало синхронизации заявок...")

	// Статусы, которые мы хотим синхронизировать
	targetStatuses := []string{
		"registered", "inprogress", "waitClientAnswer", "resumed",
	}

	providers := g.manager.GetTicketProviders()
	for _, provider := range providers {
		g.processProvider(ctx, provider, targetStatuses)
	}
}

func (g *ticketGatewayImpl) processProvider(ctx context.Context, provider integration.TicketProvider, statuses []string) {
	log := g.logger.With("system", provider.SystemName())

	// Получаем Map: ExternalID -> Ticket Model
	receivedTickets, err := provider.GetTickets(ctx, statuses)
	if err != nil {
		log.Error("Ошибка получения списка заявок от провайдера", "error", err)
		return
	}

	log.Info("Получено заявок от провайдера", "count", len(receivedTickets))

	countUpserted := 0
	countRestored := 0

	for extUUID, ticket := range receivedTickets {
		if extUUID == "" {
			continue
		}

		// На всякий случай заполняем, если маппер пропустил
		if ticket.ServiceDeskUUID == "" {
			ticket.ServiceDeskUUID = extUUID
		}

		// 1. Ищем существующую связь
		link, _ := g.linkRepo.GetByExternalID(ctx, nil, provider.SystemName(), extUUID)

		if link != nil {
			ticket.ID = link.InternalID
			// Обновляем поля (статус, тема и т.д.)
			if err := g.ticketRepo.Update(ctx, ticket); err != nil {
				log.Error("Ошибка обновления заявки", "id", ticket.ID, "error", err)
			}
		} else {
			// 2. Связи нет. Ищем по бизнес-ключу (Номеру) для восстановления
			existingTicket, err := g.ticketRepo.GetByNumber(ctx, ticket.Number)
			if err == nil && existingTicket != nil {
				log.Info("Найден существующий тикет без связи. Восстановление...", "number", ticket.Number)
				ticket.ID = existingTicket.ID

				// Восстанавливаем связь
				newLink := &models.ExternalSystemLink{
					InternalID:      ticket.ID,
					SystemName:      provider.SystemName(),
					ServiceDeskUUID: extUUID,
					EntityType:      "Ticket",
					LastSyncedAt:    time.Now(),
				}
				g.linkRepo.Create(ctx, nil, newLink)
				countRestored++

				g.ticketRepo.Update(ctx, ticket)
			} else {
				// 3. Создаем новый
				if err := g.ticketRepo.Create(ctx, ticket); err != nil {
					log.Error("Ошибка создания заявки", "uuid", extUUID, "error", err)
					continue
				}
				// Создаем связь для нового
				newLink := &models.ExternalSystemLink{
					InternalID:      ticket.ID,
					SystemName:      provider.SystemName(),
					ServiceDeskUUID: extUUID,
					EntityType:      "Ticket",
					LastSyncedAt:    time.Now(),
				}
				g.linkRepo.Create(ctx, nil, newLink)
			}
		}
		countUpserted++
	}

	log.Info("Синхронизация заявок завершена", "upserted", countUpserted, "restored_links", countRestored)
}
