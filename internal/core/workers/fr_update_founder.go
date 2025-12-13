package workers

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/pkg/eventbus"
	"time"

	"github.com/google/uuid"
)

type FRUpdateFounder interface {
	Start(ctx context.Context)
}

type frUpdateFounderImpl struct {
	cfg      *config.Config
	logger   logger.LoggerInterface
	bus      eventbus.EventBus
	frRepo   fiscal.Repository
	linkRepo repositories.LinkRepo
	manager  *integrations.Manager
}

func NewFRUpdateFounder(
	cfg *config.Config,
	logger logger.LoggerInterface,
	bus eventbus.EventBus,
	frRepo fiscal.Repository,
	linkRepo repositories.LinkRepo,
	manager *integrations.Manager,
) FRUpdateFounder {
	return &frUpdateFounderImpl{
		cfg:      cfg,
		logger:   logger,
		bus:      bus,
		frRepo:   frRepo,
		linkRepo: linkRepo,
		manager:  manager,
	}
}

func (w *frUpdateFounderImpl) Start(ctx context.Context) {
	initialDelay := 30 * time.Second
	w.logger.Info("Запуск воркера FRUpdateFounder (ожидание перед первым циклом)",
		"initial_delay", initialDelay,
		"interval", w.cfg.FRDiscrepancyCheckInterval)

	select {
	case <-time.After(initialDelay):
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(w.cfg.FRDiscrepancyCheckInterval)
	defer ticker.Stop()

	w.runCheckCycle(ctx)

	for {
		select {
		case <-ticker.C:
			w.runCheckCycle(ctx)
		case <-ctx.Done():
			w.logger.Info("Остановка воркера FRUpdateFounder.")
			return
		}
	}
}

func (w *frUpdateFounderImpl) runCheckCycle(ctx context.Context) {
	requestID := uuid.New().String()
	cycleLogger := w.logger.With("request_id", requestID)

	cycleLogger.Info("Начало нового цикла сверки данных ФР.")

	providers := w.manager.GetInventoryProviders()
	for _, p := range providers {
		w.processProvider(ctx, p, cycleLogger)
	}
}

func (w *frUpdateFounderImpl) processProvider(ctx context.Context, p integration.InventoryProvider, log logger.LoggerInterface) {
	// 1. Получаем все ФР из провайдера (полные модели) в виде Map[ExternalID]Model
	remoteFRsMap, err := p.GetAllFiscalRegisters(ctx)
	if err != nil {
		log.Error("Не удалось получить список ФР из провайдера", "system", p.SystemName(), "error", err)
		return
	}
	log.Info("Загружено ФР из провайдера", "system", p.SystemName(), "count", len(remoteFRsMap))

	// 2. Получаем все ФР из нашей локальной базы.
	localFRs, err := w.frRepo.Search(ctx, "", 10000, 0)
	if err != nil {
		log.Error("Не удалось получить список ФР из локальной БД", "error", err)
		return
	}

	publishedEvents := 0
	// 3. Итерируем по локальным ФР
	for _, localFR := range localFRs {
		// Находим внешний ID для нашего локального ФР и текущей системы
		link, err := w.linkRepo.GetByInternalID(ctx, nil, p.SystemName(), localFR.ID)
		if err != nil || link == nil {
			continue
		}

		// Ищем в карте по ExternalUUID
		remoteFR, ok := remoteFRsMap[link.ServiceDeskUUID]
		if !ok {
			// ФР есть локально, но нет в выгрузке (удалена?)
			continue
		}

		discrepancies := w.compareFiscalRegisters(&localFR, remoteFR)

		if len(discrepancies) > 0 {
			log.Info("Обнаружено расхождение данных ФР", "internal_id", localFR.ID, "diffs", len(discrepancies))

			payload := events.FiscalRegisterDiscrepancyPayload{
				FRInternalUUID:    link.InternalID,
				FRServiceDeskUUID: link.ServiceDeskUUID,
				Discrepancies:     discrepancies,
			}
			w.bus.Publish(eventbus.Event{
				Type:    events.FiscalRegisterDiscrepancyFound,
				Payload: payload,
			})
			publishedEvents++
		}
	}

	log.Info("Цикл сверки данных ФР завершен.", "system", p.SystemName(), "published_events", publishedEvents)
}

func (w *frUpdateFounderImpl) compareFiscalRegisters(local, remote *fiscal.FiscalRegister) map[string]events.DiscrepancyDetail {
	discrepancies := make(map[string]events.DiscrepancyDetail)

	// 1. Дата окончания ФН
	if local.FNExpireDate != nil && (remote.FNExpireDate == nil || !local.FNExpireDate.Truncate(24*time.Hour).Equal(remote.FNExpireDate.Truncate(24*time.Hour))) {
		var remoteDateStr string
		if remote.FNExpireDate != nil {
			remoteDateStr = remote.FNExpireDate.Format("2006-01-02")
		} else {
			remoteDateStr = "<nil>"
		}
		discrepancies["FNExpireDate"] = events.DiscrepancyDetail{
			EtalonValue:      local.FNExpireDate.Format("2006-01-02"),
			ServiceDeskValue: remoteDateStr,
		}
	}

	// 2. РН ККТ (RNKKT)
	localRN := utils.NormalizeRNKKT(utils.SafeStringDereference(local.RNKKT))
	remoteRN := utils.NormalizeRNKKT(utils.SafeStringDereference(remote.RNKKT))

	if localRN != "" && localRN != remoteRN {
		discrepancies["RNKKT"] = events.DiscrepancyDetail{
			EtalonValue:      utils.SafeStringDereference(local.RNKKT),
			ServiceDeskValue: utils.SafeStringDereference(remote.RNKKT),
		}
	}

	// 3. Заводской номер (FRSerialNumber)
	if local.FRSerialNumber != nil && *local.FRSerialNumber != "" {
		if remote.FRSerialNumber == nil || *local.FRSerialNumber != *remote.FRSerialNumber {
			discrepancies["FRSerialNumber"] = events.DiscrepancyDetail{
				EtalonValue:      *local.FRSerialNumber,
				ServiceDeskValue: utils.SafeStringDereference(remote.FRSerialNumber),
			}
		}
	}

	// 4. Номер ФН (FNNumber)
	if local.FNNumber != nil && *local.FNNumber != "" {
		if remote.FNNumber == nil || *local.FNNumber != *remote.FNNumber {
			discrepancies["FNNumber"] = events.DiscrepancyDetail{
				EtalonValue:      *local.FNNumber,
				ServiceDeskValue: utils.SafeStringDereference(remote.FNNumber),
			}
		}
	}

	return discrepancies
}
