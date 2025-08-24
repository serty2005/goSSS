package gateways

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ContractGateway отвечает за синхронизацию контрактов и инициирование пересчета статусов компаний.
type ContractGateway interface {
	Start(ctx context.Context)
	RunSyncCycle(ctx context.Context) error // Оставляем публичным для ручного запуска через API
}

type contractGatewayImpl struct {
	cfg          *config.Config
	sdClient     services.ServiceDeskClient
	contractRepo repositories.ContractRepo
	logger       *zap.Logger
	db           *gorm.DB
	bus          eventbus.EventBus
	mu           sync.Mutex
	isSyncing    bool
}

// NewContractGateway создает новый экземпляр шлюза.
func NewContractGateway(
	cfg *config.Config,
	db *gorm.DB,
	sdClient services.ServiceDeskClient,
	contractRepo repositories.ContractRepo,
	bus eventbus.EventBus,
	logger *zap.Logger,
) ContractGateway {
	return &contractGatewayImpl{
		cfg:          cfg,
		db:           db,
		sdClient:     sdClient,
		contractRepo: contractRepo,
		bus:          bus,
		logger:       logger,
	}
}

// Start запускает воркер в фоновом режиме.
func (g *contractGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.ContractSyncInterval
	g.logger.Info("Запуск шлюза контрактов", zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if err := g.RunSyncCycle(ctx); err != nil {
		g.logger.Error("Первый запуск синхронизации контрактов завершился с ошибкой", zap.Error(err))
	}

	for {
		select {
		case <-ticker.C:
			if err := g.RunSyncCycle(ctx); err != nil {
				g.logger.Error("Цикл синхронизации контрактов завершился с ошибкой", zap.Error(err))
			}
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза контрактов...")
			return
		}
	}
}

// RunSyncCycle выполняет один цикл синхронизации.
func (g *contractGatewayImpl) RunSyncCycle(ctx context.Context) error {
	g.mu.Lock()
	if g.isSyncing {
		g.logger.Warn("Цикл синхронизации контрактов уже запущен. Пропуск.")
		g.mu.Unlock()
		return nil
	}
	g.isSyncing = true
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		g.isSyncing = false
		g.mu.Unlock()
	}()

	g.logger.Info("Начало нового цикла синхронизации контрактов.")

	// Шаг 1: Синхронизируем сами сущности контрактов (пишем в свою справочную таблицу).
	err := g.syncContractEntities(ctx)
	if err != nil {
		return err
	}

	// Шаг 2: Получаем актуальные данные о контрактах для пересчета статусов.
	companyStatusMap, err := g.calculateCompanyStatuses(ctx)
	if err != nil {
		return err
	}

	// Шаг 3: Публикуем событие для Оркестратора.
	g.bus.Publish(eventbus.Event{
		Type: events.ContractsStatusRecalculated,
		Payload: events.ContractsStatusPayload{
			CompanyActiveContract: companyStatusMap,
		},
	})
	g.logger.Info("Событие о пересчете статусов контрактов опубликовано", zap.Int("companies_affected", len(companyStatusMap)))

	g.logger.Info("Цикл синхронизации контрактов шлюзом завершен.")
	return nil
}

// syncContractEntities синхронизирует локальную таблицу контрактов с ServiceDesk.
// Эта операция разрешена шлюзу, т.к. таблица `contracts` является справочником для этого шлюза.
func (g *contractGatewayImpl) syncContractEntities(ctx context.Context) error {
	metaClass := "agreement$agreement"
	log := g.logger.With(zap.String("metaClass", metaClass))

	remoteList, err := g.sdClient.FetchEntityList(ctx, metaClass, true)
	if err != nil {
		log.Error("Не удалось получить список контрактов из ServiceDesk", zap.Error(err))
		return err
	}

	localMap, err := g.contractRepo.GetAllUUIDsAndDates(ctx)
	if err != nil {
		log.Error("Не удалось получить локальные контракты", zap.Error(err))
		return err
	}

	for _, remoteData := range remoteList {
		uuid, _ := remoteData["UUID"].(string)
		if uuid == "" {
			continue
		}

		contract, err := services.DataToContract(remoteData)
		if err != nil {
			log.Warn("Ошибка маппинга контракта, пропуск", zap.String("uuid", uuid), zap.Error(err))
			continue
		}

		localContract, exists := localMap[uuid]
		if !exists {
			if err := g.contractRepo.Create(ctx, nil, contract); err != nil {
				log.Error("Не удалось создать контракт", zap.String("uuid", uuid), zap.Error(err))
			}
		} else {
			remoteLMD := utils.ParseServiceDeskTime(remoteData["lastModifiedDate"].(string))
			if localContract.DeletedAt.Valid || (remoteLMD != nil && localContract.LastModifiedDate != nil && remoteLMD.After(*localContract.LastModifiedDate)) {
				updateData := map[string]interface{}{
					"state":              contract.State,
					"state_start_time":   contract.StateStartTime,
					"services":           contract.Services,
					"recipients":         contract.Recipients,
					"last_modified_date": contract.LastModifiedDate,
					"deleted_at":         gorm.Expr("NULL"),
				}
				if _, err := g.contractRepo.Update(ctx, nil, uuid, updateData); err != nil {
					log.Error("Не удалось обновить контракт", zap.String("uuid", uuid), zap.Error(err))
				}
			}
		}
	}
	return nil
}

// calculateCompanyStatuses определяет, у каких компаний есть активные контракты.
func (g *contractGatewayImpl) calculateCompanyStatuses(ctx context.Context) (map[string]bool, error) {
	remoteContracts, err := g.sdClient.FetchEntityList(ctx, "agreement$agreement", true)
	if err != nil {
		g.logger.Error("Не удалось получить контракты для пересчета статусов", zap.Error(err))
		return nil, err
	}

	activeCompanyUUIDs := make(map[string]bool)
	for _, contractData := range remoteContracts {
		state, _ := contractData["state"].(string)
		companyUUIDs := services.GetCompanyUUIDsFromContract(contractData)
		isActive := state == "active"
		for _, uuid := range companyUUIDs {
			// Если у компании уже есть активный контракт, не меняем на false
			if currentStatus, exists := activeCompanyUUIDs[uuid]; !exists || !currentStatus {
				activeCompanyUUIDs[uuid] = isActive
			}
		}
	}
	return activeCompanyUUIDs, nil
}