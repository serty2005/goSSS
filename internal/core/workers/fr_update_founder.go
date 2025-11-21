// internal/workers/fr_update_founder.go
package workers

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	loggerPkg "etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"time"

	"github.com/google/uuid"
)

// FRUpdateFounder (Fiscal Register Update Founder) - это фоновый воркер,
// который периодически сверяет данные о фискальных регистраторах в локальной
// базе данных с данными в ServiceDesk.
type FRUpdateFounder interface {
	Start(ctx context.Context)
}

type frUpdateFounderImpl struct {
	cfg      *config.Config
	logger   loggerPkg.LoggerInterface
	bus      eventbus.EventBus
	frRepo   repositories.FiscalRegisterRepo
	linkRepo repositories.LinkRepo // Новая зависимость
	sdClient external.ExternalSystemClient
}

// NewFRUpdateFounder создает новый экземпляр воркера.
func NewFRUpdateFounder(
	cfg *config.Config,
	logger loggerPkg.LoggerInterface,
	bus eventbus.EventBus,
	frRepo repositories.FiscalRegisterRepo,
	linkRepo repositories.LinkRepo, // Новая зависимость
	sdClient external.ExternalSystemClient,
) FRUpdateFounder {
	return &frUpdateFounderImpl{
		cfg:      cfg,
		logger:   logger,
		bus:      bus,
		frRepo:   frRepo,
		linkRepo: linkRepo,
		sdClient: sdClient,
	}
}

// Start запускает воркер в фоновом режиме.
func (w *frUpdateFounderImpl) Start(ctx context.Context) {
	w.logger.Info("Запуск воркера поиска обновлений для ФР (FRUpdateFounder)", "interval", w.cfg.FRDiscrepancyCheckInterval)
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

// runCheckCycle выполняет один полный цикл сверки данных.
func (w *frUpdateFounderImpl) runCheckCycle(ctx context.Context) {
	requestID := uuid.New().String()
	cycleLogger := w.logger.With("request_id", requestID)

	cycleLogger.Info("Начало нового цикла сверки данных ФР с ServiceDesk.")

	// 1. Получаем все ФР из ServiceDesk.
	remoteFRsData, err := w.sdClient.FetchEntityList(ctx, "FiscalRegister")
	if err != nil {
		cycleLogger.Error("Не удалось получить список ФР из ServiceDesk, цикл прерван", "error", err)
		return
	}

	// 2. Преобразуем данные из SD в мапу [externalUUID] -> *models.FiscalRegister
	remoteFRsMap := make(map[string]*models.FiscalRegister, len(remoteFRsData))
	// MapperContext здесь не нужен, так как DataToFiscalRegister не ищет связей
	mapperCtx := &external.MapperContext{Logger: cycleLogger}
	for _, data := range remoteFRsData {
		fr, err := w.sdClient.Mapper().DataToFiscalRegister(ctx, mapperCtx, data)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			cycleLogger.Warn("Пропуск ФР из ServiceDesk из-за ошибки маппинга", "uuid", uuid, "error", err)
			continue
		}
		externalUUID, _ := data["UUID"].(string)
		remoteFRsMap[externalUUID] = fr
	}
	cycleLogger.Info("Из ServiceDesk загружено и обработано ФР", "count", len(remoteFRsMap))

	// 3. Получаем все ФР из нашей локальной ("эталонной") базы.
	localFRs, err := w.frRepo.Search(ctx, "", 10000, 0)
	if err != nil {
		cycleLogger.Error("Не удалось получить список ФР из локальной БД, цикл прерван", "error", err)
		return
	}
	cycleLogger.Info("Из локальной БД загружено ФР для сверки", "count", len(localFRs))

	publishedEvents := 0
	// 4. Итерируем по локальным ФР и сравниваем их с данными из SD.
	for _, localFR := range localFRs {
		// Находим внешний ID для нашего локального ФР
		link, err := w.linkRepo.GetByInternalID(ctx, nil, "naumen", localFR.ID)
		if err != nil {
			cycleLogger.Error("Ошибка получения связи для ФР", "internal_id", localFR.ID, "error", err)
			continue
		}
		if link == nil {
			cycleLogger.Debug("Пропуск ФР, для которого нет связи с внешней системой", "internal_id", localFR.ID)
			continue
		}

		remoteFR, ok := remoteFRsMap[link.ServiceDeskUUID]
		if !ok {
			cycleLogger.Debug("Пропуск локального ФР, так как он отсутствует во внешней системе", "internal_id", localFR.ID)
			continue
		}

		discrepancies := w.compareFiscalRegisters(&localFR, remoteFR)

		if len(discrepancies) > 0 {
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

	cycleLogger.Info("Цикл сверки данных ФР завершен.", "published_events", publishedEvents)
}

// compareFiscalRegisters сравнивает два фискальных регистратора и возвращает карту расхождений.
func (w *frUpdateFounderImpl) compareFiscalRegisters(local, remote *models.FiscalRegister) map[string]events.DiscrepancyDetail {
	discrepancies := make(map[string]events.DiscrepancyDetail)

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

	return discrepancies
}
