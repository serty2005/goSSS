// internal/core/workers/fr_update_founder.go
package workers

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/external"
	loggerPkg "etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
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
	frRepo   fiscal.Repository
	linkRepo repositories.LinkRepo
	sdClient external.ExternalSystemClient
}

// NewFRUpdateFounder создает новый экземпляр воркера.
func NewFRUpdateFounder(
	cfg *config.Config,
	logger loggerPkg.LoggerInterface,
	bus eventbus.EventBus,
	frRepo fiscal.Repository,
	linkRepo repositories.LinkRepo,
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
	// ВАЖНО: Добавляем начальную задержку, чтобы AgentFTPGateway и SDeskGateway
	// успели обновить локальную базу данных при старте сервера.
	// Иначе мы будем сравнивать пустую или устаревшую базу и не найдем расхождений.
	initialDelay := 30 * time.Second
	w.logger.Info("Запуск воркера FRUpdateFounder (ожидание перед первым циклом)",
		"initial_delay", initialDelay,
		"interval", w.cfg.FRDiscrepancyCheckInterval)

	select {
	case <-time.After(initialDelay):
		// Продолжаем запуск
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(w.cfg.FRDiscrepancyCheckInterval)
	defer ticker.Stop()

	// Запускаем первый цикл сразу после задержки
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
	remoteFRsMap := make(map[string]*fiscal.FiscalRegister, len(remoteFRsData))
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
			// Если связи нет, это может быть новый ФР, который еще не создан в SD.
			// Этим занимается ProcessingEngine (add_equipment), здесь пропускаем.
			continue
		}

		remoteFR, ok := remoteFRsMap[link.ServiceDeskUUID]
		if !ok {
			cycleLogger.Debug("Локальный ФР имеет связь, но отсутствует в выгрузке ServiceDesk (возможно, удален или архивирован)", "internal_id", localFR.ID, "sd_uuid", link.ServiceDeskUUID)
			continue
		}

		discrepancies := w.compareFiscalRegisters(&localFR, remoteFR)

		if len(discrepancies) > 0 {
			cycleLogger.Info("Обнаружено расхождение данных ФР", "internal_id", localFR.ID, "diffs", len(discrepancies))

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
// Local - это эталон (из БД), Remote - это данные из SD.
func (w *frUpdateFounderImpl) compareFiscalRegisters(local, remote *fiscal.FiscalRegister) map[string]events.DiscrepancyDetail {
	discrepancies := make(map[string]events.DiscrepancyDetail)

	// 1. Дата окончания ФН (самое важное)
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
	// Сравниваем очищенные значения, так как форматирование может отличаться
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
