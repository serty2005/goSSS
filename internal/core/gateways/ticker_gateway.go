package gateways

import (
	"context"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
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
	// Зависимости для MapperContext
	db       *gorm.DB
	linkRepo repositories.LinkRepo
}

// NewTicketGateway создает новый экземпляр шлюза.
func NewTicketGateway(
	cfg *config.Config,
	logger logger.LoggerInterface,
	sdClient external.ExternalSystemClient,
	ticketRepo tickets.TicketRepository,
	db *gorm.DB,
	linkRepo repositories.LinkRepo,
) TicketGateway {
	return &ticketGatewayImpl{
		cfg:        cfg,
		logger:     logger,
		sdClient:   sdClient,
		ticketRepo: ticketRepo,
		db:         db,
		linkRepo:   linkRepo,
	}
}

func (g *ticketGatewayImpl) Start(ctx context.Context) {
	// Используем тот же интервал, что и для других SD синхронизаций, или задаем свой
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

		// Сохраняем UUID в сет
		activeRemoteUUIDs[ticket.ServiceDeskUUID] = struct{}{}

		if err := g.ticketRepo.Upsert(ctx, ticket); err != nil {
			g.logger.Error("Ошибка сохранения заявки в БД", "uuid", ticket.ServiceDeskUUID, "error", err)
			continue
		}
		countUpserted++
	}

	// --- ЛОГИКА ZOMBIE KILLER ---
	// 1. Ищем в локальной БД заявки, которые у нас числятся активными
	// (не closed и не resolved), но которых нет в списке activeRemoteUUIDs.

	// Получаем список локальных активных заявок (нужен метод в репозитории или простой SQL запрос)
	// Для простоты используем gorm напрямую через g.db (так как gateway имеет доступ к db)
	var localActiveTickets []tickets.Ticket
	g.db.WithContext(ctx).Select("service_desk_uuid").
		Where("status NOT IN ?", []string{tickets.StatusClosed, tickets.StatusResolved}).
		Find(&localActiveTickets)

	zombiesCount := 0
	for _, localT := range localActiveTickets {
		if _, exists := activeRemoteUUIDs[localT.ServiceDeskUUID]; !exists {
			// Заявка была активна у нас, но не пришла в списке активных из Naumen.
			// Значит, она либо закрыта, либо удалена, либо перешла в другой статус.

			g.logger.Info("Обнаружена заявка, исчезнувшая из активных. Проверка статуса...", "uuid", localT.ServiceDeskUUID)

			// Точечно запрашиваем статус из Naumen
			details, err := g.sdClient.FetchEntityDetails(ctx, localT.ServiceDeskUUID, "Ticket")
			if err != nil {
				g.logger.Error("Не удалось проверить статус исчезнувшей заявки", "uuid", localT.ServiceDeskUUID, "error", err)
				continue
			}

			// Обновляем статус в БД
			if newState, ok := details["state"].(string); ok {
				g.logger.Info("Обновление статуса исчезнувшей заявки", "uuid", localT.ServiceDeskUUID, "old_status", "active", "new_status", newState)
				g.db.Model(&tickets.Ticket{}).Where("service_desk_uuid = ?", localT.ServiceDeskUUID).Update("status", newState)
				zombiesCount++
			}
		}
	}

	g.logger.Info("Синхронизация заявок завершена", "upserted", countUpserted, "zombies_fixed", zombiesCount)
}
