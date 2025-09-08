// internal/gateways/sdesk_gateway.go
package gateways

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/external"
	"etalon-server/internal/repositories"
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
	InternalID       string
	LastModifiedDate *time.Time
	DeletedAt        gorm.DeletedAt
}

type serviceDeskGatewayImpl struct {
	cfg             *config.Config
	sdClient        external.ExternalSystemClient
	bus             eventbus.EventBus
	logger          *zap.Logger
	db              *gorm.DB // Добавляем прямое подключение для работы со связями
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	mu              sync.Mutex
	isSyncing       bool
}

// NewServiceDeskGateway создает новый экземпляр шлюза ServiceDesk.
func NewServiceDeskGateway(cfg *config.Config, sdClient external.ExternalSystemClient, bus eventbus.EventBus, logger *zap.Logger, db *gorm.DB, companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) ServiceDeskGateway {
	return &serviceDeskGatewayImpl{
		cfg:             cfg,
		sdClient:        sdClient,
		bus:             bus,
		logger:          logger,
		db:              db, // Инициализируем
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

// runSyncCycle выполняет один полный цикл синхронизации.
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

	entityTypes := []string{"Company", "Server", "Workstation", "FiscalRegister"}
	for _, entityType := range entityTypes {
		select {
		case <-ctx.Done():
			return
		default:
			g.processEntityType(ctx, entityType)
		}
	}
	g.logger.Info("Цикл получения данных из ServiceDesk завершен.")
}

// processEntityType выполняет инкрементальную синхронизацию для одного типа сущности.
func (g *serviceDeskGatewayImpl) processEntityType(ctx context.Context, entityType string) {
	log := g.logger.With(zap.String("entityType", entityType))
	log.Info("Начало синхронизации типа сущности")

	// 1. Получаем КРАТКИЙ список сущностей из внешней системы.
	remoteList, err := g.sdClient.FetchEntitySummaries(ctx, entityType)
	if err != nil {
		log.Error("Не удалось получить список сущностей из ServiceDesk", zap.Error(err))
		return
	}

	// 2. Получаем все существующие связи для этого типа сущности.
	localMap, err := g.getLocalEntityLinks(ctx, entityType)
	if err != nil {
		log.Error("Не удалось получить локальные связи для сущностей", zap.Error(err))
		return
	}

	// 3. Сравниваем списки и формируем задачи на создание, обновление и удаление.
	remoteExternalIDs := make(map[string]struct{}, len(remoteList))
	var toCreate, toUpdate, toDelete []string

	for _, remoteItem := range remoteList {
		remoteUUID, _ := remoteItem["UUID"].(string)
		if remoteUUID == "" {
			continue
		}
		remoteExternalIDs[remoteUUID] = struct{}{}
		remoteLMD := utils.ParseServiceDeskTime(remoteItem["lastModifiedDate"].(string))
		if remoteLMD == nil {
			continue // Пропускаем сущности без даты модификации (можно добавить creationDate?)
		}

		localLink, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else if localLink.DeletedAt.Valid || (localLink.LastModifiedDate != nil && remoteLMD.After(*localLink.LastModifiedDate)) {
			toUpdate = append(toUpdate, remoteUUID)
		}
	}

	for externalUUID, localLink := range localMap {
		if _, exists := remoteExternalIDs[externalUUID]; !exists && !localLink.DeletedAt.Valid {
			toDelete = append(toDelete, externalUUID)
		}
	}

	log.Info("Сравнение завершено",
		zap.Int("remote_count", len(remoteList)),
		zap.Int("local_count", len(localMap)),
		zap.Int("to_create", len(toCreate)),
		zap.Int("to_update", len(toUpdate)),
		zap.Int("to_delete", len(toDelete)),
	)

	// 4. Обрабатываем задачи и публикуем события.
	if len(toDelete) > 0 {
		g.publishDeleteEvents(entityType, toDelete, log)
	}

	if len(toCreate) > 0 || len(toUpdate) > 0 {
		uuidsToFetch := append(toCreate, toUpdate...)
		g.fetchAndPublishUpdateEvents(ctx, entityType, uuidsToFetch, log)
	}
}

// publishDeleteEvents публикует события об удалении сущностей.
func (g *serviceDeskGatewayImpl) publishDeleteEvents(entityType string, externalUUIDs []string, log *zap.Logger) {
	log.Info("Публикация событий об удалении...", zap.Int("count", len(externalUUIDs)))
	for _, uuid := range externalUUIDs {
		g.bus.Publish(eventbus.Event{
			Type: events.ServiceDeskEntityDeleted,
			Payload: events.ServiceDeskEntityDeletePayload{
				EntityType:   entityType,
				ExternalUUID: uuid,
			},
		})
	}
}

// fetchAndPublishUpdateEvents получает полные данные для сущностей и публикует события.
func (g *serviceDeskGatewayImpl) fetchAndPublishUpdateEvents(ctx context.Context, entityType string, externalUUIDs []string, log *zap.Logger) {
	log.Info("Получение полных данных для новых/обновленных сущностей...", zap.Int("count", len(externalUUIDs)))
	var wg sync.WaitGroup
	tasks := make(chan string, len(externalUUIDs))

	for i := 0; i < g.cfg.ConcurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for uuid := range tasks {
				select {
				case <-ctx.Done():
					return
				default:
					details, err := g.sdClient.FetchEntityDetails(ctx, uuid, entityType)
					if err != nil {
						log.Error("Не удалось получить детали для сущности", zap.String("external_uuid", uuid), zap.Error(err))
						continue
					}
					g.bus.Publish(eventbus.Event{
						Type: events.ServiceDeskEntityUpdated,
						Payload: events.ServiceDeskEntityPayload{
							EntityType:   entityType,
							ExternalUUID: uuid,
							Data:         details,
						},
					})
				}
			}
		}()
	}

	for _, uuid := range externalUUIDs {
		tasks <- uuid
	}
	close(tasks)
	wg.Wait()
}

// getLocalEntityLinks извлекает из БД мапу с информацией о связях для данного типа сущности.
func (g *serviceDeskGatewayImpl) getLocalEntityLinks(ctx context.Context, entityType string) (map[string]localEntityInfo, error) {

	type result struct {
		ExternalID       string
		InternalID       string
		LastModifiedDate *time.Time
		DeletedAt        gorm.DeletedAt
	}

	var results []result
	var err error

	// Выбираем таблицу для JOIN в зависимости от entityType
	var tableName string
	switch entityType {
	case "Company":
		tableName = "companies"
	case "Server":
		tableName = "servers"
	case "Workstation":
		tableName = "workstations"
	case "FiscalRegister":
		tableName = "fiscal_registers"
	default:
		return nil, fmt.Errorf("неизвестный тип сущности для получения связей: %s", entityType)
	}

	// Выполняем запрос с JOIN
	err = g.db.WithContext(ctx).Table("external_system_links as l").
		Select("l.external_id, l.internal_id, t.last_modified_date, t.deleted_at").
		Joins(fmt.Sprintf("JOIN %s as t ON l.internal_id = t.id", tableName)).
		Where("l.system_name = ? AND l.entity_type = ?", "naumen", entityType).
		Scan(&results).Error

	if err != nil {
		return nil, err
	}

	infoMap := make(map[string]localEntityInfo, len(results))
	for _, res := range results {
		infoMap[res.ExternalID] = localEntityInfo{
			InternalID:       res.InternalID,
			LastModifiedDate: res.LastModifiedDate,
			DeletedAt:        res.DeletedAt,
		}
	}

	return infoMap, nil
}
