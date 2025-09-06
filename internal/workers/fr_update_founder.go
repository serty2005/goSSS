// internal/workers/fr_update_founder.go
package workers

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
	"time"

	"go.uber.org/zap"
)

// FRUpdateFounder (Fiscal Register Update Founder) - это фоновый воркер,
// который периодически сверяет данные о фискальных регистраторах в локальной
// базе данных с данными в ServiceDesk. При обнаружении расхождений он
// публикует событие в шину для последующего создания задачи оператору.
type FRUpdateFounder interface {
	Start(ctx context.Context)
}

type frUpdateFounderImpl struct {
	cfg      *config.Config
	logger   *zap.Logger
	bus      eventbus.EventBus
	frRepo   repositories.FiscalRegisterRepo
	sdClient services.ServiceDeskClient
}

// NewFRUpdateFounder создает новый экземпляр воркера.
func NewFRUpdateFounder(
	cfg *config.Config,
	logger *zap.Logger,
	bus eventbus.EventBus,
	frRepo repositories.FiscalRegisterRepo,
	sdClient services.ServiceDeskClient,
) FRUpdateFounder {
	return &frUpdateFounderImpl{
		cfg:      cfg,
		logger:   logger,
		bus:      bus,
		frRepo:   frRepo,
		sdClient: sdClient,
	}
}

// Start запускает воркер в фоновом режиме.
func (w *frUpdateFounderImpl) Start(ctx context.Context) {
	w.logger.Info("Запуск воркера поиска обновлений для ФР (FRUpdateFounder)", zap.Duration("interval", w.cfg.FRDiscrepancyCheckInterval))
	ticker := time.NewTicker(w.cfg.FRDiscrepancyCheckInterval)
	defer ticker.Stop()

	// Первый запуск сразу после старта, чтобы не ждать интервал.
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
	w.logger.Info("Начало нового цикла сверки данных ФР с ServiceDesk.")

	// 1. Получаем все ФР из ServiceDesk одним запросом.
	remoteFRsData, err := w.sdClient.FetchEntityList(ctx, "objectBase$FR", true)
	if err != nil {
		w.logger.Error("Не удалось получить список ФР из ServiceDesk, цикл прерван", zap.Error(err))
		return
	}

	// 2. Преобразуем данные из SD в мапу для быстрого доступа.
	remoteFRsMap := make(map[string]*models.FiscalRegister, len(remoteFRsData))
	for _, data := range remoteFRsData {
		fr, err := services.DataToFiscalRegister(data)
		if err != nil {
			uuid, _ := data["UUID"].(string)
			w.logger.Warn("Пропуск ФР из ServiceDesk из-за ошибки маппинга", zap.String("uuid", uuid), zap.Error(err))
			continue
		}
		remoteFRsMap[*fr.ServiceDeskUUID] = fr
	}
	w.logger.Info("Из ServiceDesk загружено и обработано ФР", zap.Int("count", len(remoteFRsMap)))

	// 3. Получаем все ФР из нашей локальной ("эталонной") базы.
	// Для простоты и производительности можно получать не все объекты, а только необходимые поля.
	// Но для начала получим целиком.
	localFRs, err := w.frRepo.Search(ctx, "", 10000, 0) // TODO: Сделать пагинацию для >10k записей
	if err != nil {
		w.logger.Error("Не удалось получить список ФР из локальной БД, цикл прерван", zap.Error(err))
		return
	}
	w.logger.Info("Из локальной БД загружено ФР для сверки", zap.Int("count", len(localFRs)))

	publishedEvents := 0
	// 4. Итерируем по локальным ФР и сравниваем их с данными из SD.
	for _, localFR := range localFRs {
		remoteFR, ok := remoteFRsMap[*localFR.ServiceDeskUUID]
		if !ok {
			w.logger.Debug("Пропуск ФР из локальной БД из-за отсутствия в ServiceDesk", zap.String("uuid", *localFR.ServiceDeskUUID))
			continue
		}

		discrepancies := w.compareFiscalRegisters(&localFR, remoteFR)

		if len(discrepancies) > 0 {
			// Если найдены расхождения, публикуем событие.
			payload := events.FiscalRegisterDiscrepancyPayload{
				FRServiceDeskUUID: *localFR.ServiceDeskUUID,
				Discrepancies:     discrepancies,
			}
			w.bus.Publish(eventbus.Event{
				Type:    events.FiscalRegisterDiscrepancyFound,
				Payload: payload,
			})
			publishedEvents++
		}
	}

	w.logger.Info("Цикл сверки данных ФР завершен.", zap.Int("published_events", publishedEvents))
}

// compareFiscalRegisters сравнивает два фискальных регистратора и возвращает карту расхождений.
// В СООТВЕТСТВИИ С НОВЫМИ ТРЕБОВАНИЯМИ, задача создается ТОЛЬКО при расхождении
// даты окончания срока действия ФН (FNExpireDate).
func (w *frUpdateFounderImpl) compareFiscalRegisters(local, remote *models.FiscalRegister) map[string]events.DiscrepancyDetail {
	discrepancies := make(map[string]events.DiscrepancyDetail)

	// --- КЛЮЧЕВАЯ ПРОВЕРКА: Срок окончания ФН ---
	// Сравниваем только дату, без времени, чтобы избежать ложных срабатываний из-за часовых поясов.
	if local.FNExpireDate != nil && (remote.FNExpireDate == nil || !local.FNExpireDate.Truncate(24*time.Hour).Equal(remote.FNExpireDate.Truncate(24*time.Hour))) {
		// ИСПРАВЛЕНИЕ: Передаем реальные значения, а не указатели, чтобы избежать вывода адресов памяти.
		discrepancies["FNExpireDate"] = events.DiscrepancyDetail{
			EtalonValue:      local.FNExpireDate.Format("2006-01-02"),
			ServiceDeskValue: remote.FNExpireDate.Format("2006-01-02"),
		}
	}

	// --- УДАЛЕННЫЕ ПРОВЕРКИ ---
	// Поля FRFirmware и FRDownloader больше не проверяются для создания задачи,
	// так как они слишком волатильны и создают лишний шум. Их обновление
	// будет происходить только по задаче `data_conflict` от агента.

	// --- ПРОВЕРКА RNKKT УДАЛЕНА ИЗ УСЛОВИЙ СОЗДАНИЯ ЗАДАЧИ ---
	// Сопоставление по ServiceDeskUUID уже гарантирует, что мы сравниваем одну и ту же сущность.
	// Расхождение в RNKKT будет обработано другим типом задач, если агент пришлет новые данные.

	return discrepancies
}
