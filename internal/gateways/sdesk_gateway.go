package gateways

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServiceDeskGateway отвечает за получение данных из ServiceDesk и публикацию событий.
type ServiceDeskGateway interface {
	Start(ctx context.Context)
}

// localEntityInfo - внутренняя структура для хранения минимально необходимых данных из локальной БД для сравнения.
type localEntityInfo struct {
	LastModifiedDate *time.Time
	DeletedAt        gorm.DeletedAt
}

type serviceDeskGatewayImpl struct {
	cfg             *config.Config
	sdClient        services.ServiceDeskClient
	bus             eventbus.EventBus
	logger          *zap.Logger
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	mu              sync.Mutex
	isSyncing       bool
}

// NewServiceDeskGateway создает новый экземпляр шлюза ServiceDesk.
func NewServiceDeskGateway(cfg *config.Config, sdClient services.ServiceDeskClient, bus eventbus.EventBus, logger *zap.Logger, companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) ServiceDeskGateway {
	return &serviceDeskGatewayImpl{
		cfg:             cfg,
		sdClient:        sdClient,
		bus:             bus,
		logger:          logger,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер в фоновом режиме.
func (g *serviceDeskGatewayImpl) Start(ctx context.Context) {
	g.logger.Info("Запуск шлюза ServiceDesk", zap.Duration("interval", g.cfg.SDeskSyncInterval))
	ticker := time.NewTicker(g.cfg.SDeskSyncInterval)
	defer ticker.Stop()

	g.runSyncCycle(ctx)

	for {
		select {
		case <-ticker.C:
			g.runSyncCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза ServiceDesk...")
			return
		}
	}
}

func (g *serviceDeskGatewayImpl) runSyncCycle(ctx context.Context) {
	g.mu.Lock()
	if g.isSyncing {
		g.logger.Warn("Цикл синхронизации шлюза ServiceDesk уже запущен. Пропуск.")
		g.mu.Unlock()
		return
	}
	g.isSyncing = true
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		g.isSyncing = false
		g.mu.Unlock()
	}()

	g.logger.Info("Начало нового цикла получения данных из ServiceDesk.")

	metaClasses := []string{"ou$company", "objectBase$Server", "objectBase$Workstation", "objectBase$FR"}
	for _, metaClass := range metaClasses {
		select {
		case <-ctx.Done():
			return
		default:
			g.processEntityType(ctx, metaClass)
		}
	}
	g.logger.Info("Цикл получения данных из ServiceDesk завершен.")
}

// processEntityType выполняет инкрементальную синхронизацию для одного типа сущности.
func (g *serviceDeskGatewayImpl) processEntityType(ctx context.Context, metaClass string) {
	log := g.logger.With(zap.String("metaClass", metaClass))
	log.Info("Начало синхронизации типа сущности")

	// Шаг 1: Получаем списки сущностей (минимальные данные) из SD и локальной БД.
	remoteList, err := g.sdClient.FetchEntityList(ctx, metaClass, false)
	if err != nil {
		log.Error("Не удалось получить список сущностей из ServiceDesk", zap.Error(err))
		return
	}

	localMap, err := g.getLocalEntities(ctx, metaClass)
	if err != nil {
		log.Error("Не удалось получить локальные сущности", zap.Error(err))
		return
	}

	// Шаг 2: Сравниваем списки и формируем задачи на создание, обновление и удаление.
	remoteUUIDs := make(map[string]struct{}, len(remoteList))
	var toCreate, toUpdate, toDelete []string

	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteUUIDs[remoteUUID] = struct{}{}
		remoteLMD := utils.ParseServiceDeskTime(remoteItem["lastModifiedDate"].(string))
		if remoteLMD == nil {
			continue
		}

		localEntity, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else if localEntity.DeletedAt.Valid || (localEntity.LastModifiedDate != nil && remoteLMD.After(*localEntity.LastModifiedDate)) {
			toUpdate = append(toUpdate, remoteUUID)
		}
	}

	for uuid, localInfo := range localMap {
		if _, exists := remoteUUIDs[uuid]; !exists && !localInfo.DeletedAt.Valid {
			toDelete = append(toDelete, uuid)
		}
	}

	log.Info("Сравнение завершено",
		zap.Int("remote_count", len(remoteList)),
		zap.Int("local_count", len(localMap)),
		zap.Int("to_create", len(toCreate)),
		zap.Int("to_update", len(toUpdate)),
		zap.Int("to_delete", len(toDelete)),
	)

	// Шаг 3: Обрабатываем задачи и публикуем события.
	if len(toDelete) > 0 {
		g.publishDeleteEvents(metaClass, toDelete, log)
	}

	if len(toCreate) > 0 || len(toUpdate) > 0 {
		uuidsToFetch := append(toCreate, toUpdate...)
		g.fetchAndPublishUpdateEvents(ctx, metaClass, uuidsToFetch, log)
	}
}

// publishDeleteEvents публикует события об удалении сущностей.
func (g *serviceDeskGatewayImpl) publishDeleteEvents(metaClass string, uuids []string, log *zap.Logger) {
	log.Info("Публикация событий об удалении...", zap.Int("count", len(uuids)))
	for _, uuid := range uuids {
		g.bus.Publish(eventbus.Event{
			Type: events.ServiceDeskEntityDeleted,
			Payload: events.ServiceDeskEntityDeletePayload{
				MetaClass: metaClass,
				UUID:      uuid,
			},
		})
	}
}

// fetchAndPublishUpdateEvents получает полные данные для сущностей и публикует события.
func (g *serviceDeskGatewayImpl) fetchAndPublishUpdateEvents(ctx context.Context, metaClass string, uuids []string, log *zap.Logger) {
	log.Info("Получение полных данных для новых/обновленных сущностей...", zap.Int("count", len(uuids)))
	var wg sync.WaitGroup
	tasks := make(chan string, len(uuids))

	for i := 0; i < g.cfg.ConcurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				select {
				case <-ctx.Done():
					return
				default:
					details, err := g.sdClient.FetchEntityDetails(ctx, uuid, metaClass)
					if err != nil {
						log.Error("Не удалось получить детали для сущности", zap.String("uuid", uuid), zap.Error(err))
						continue
					}
					g.bus.Publish(eventbus.Event{
						Type: events.ServiceDeskEntityUpdated,
						Payload: events.ServiceDeskEntityPayload{
							MetaClass: metaClass,
							UUID:      uuid,
							Data:      details,
						},
					})
				}
			}
		}()
	}

	for _, uuid := range uuids {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// getLocalEntities извлекает из БД мапу с минимальной информацией о локальных сущностях.
func (g *serviceDeskGatewayImpl) getLocalEntities(ctx context.Context, metaClass string) (map[string]localEntityInfo, error) {
	infoMap := make(map[string]localEntityInfo)
	var err error
	switch metaClass {
	case "ou$company":
		entities, e := g.companyRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Server":
		entities, e := g.serverRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$Workstation":
		entities, e := g.workstationRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	case "objectBase$FR":
		entities, e := g.frRepo.GetAllUUIDsAndDates(ctx)
		err = e
		if err == nil {
			for uuid, entity := range entities {
				infoMap[uuid] = localEntityInfo{LastModifiedDate: entity.LastModifiedDate, DeletedAt: entity.DeletedAt}
			}
		}
	default:
		return nil, fmt.Errorf("неизвестный metaClass: %s", metaClass)
	}
	return infoMap, err
}