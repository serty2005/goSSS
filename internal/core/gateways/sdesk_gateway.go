// internal/core/gateways/sdesk_gateway.go
package gateways

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/core/integrations" // Новый импорт
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/integration"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// ServiceDeskGateway отвечает за получение данных из внешних систем (через Provider) и публикацию событий.
type ServiceDeskGateway interface {
	Start(ctx context.Context)
}

// localEntityInfo - внутренняя структура для хранения минимально необходимых данных из локальной БД.
type localEntityInfo struct {
	InternalID       string
	LastModifiedDate *time.Time
	DeletedAt        gorm.DeletedAt
}

type serviceDeskGatewayImpl struct {
	cfg             *config.Config
	manager         *integrations.Manager // Используем менеджер вместо SDClient
	bus             eventbus.EventBus
	logger          logger.LoggerInterface
	db              *gorm.DB
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	mu              sync.Mutex
	isSyncing       bool
}

// NewServiceDeskGateway создает новый экземпляр шлюза.
func NewServiceDeskGateway(
	cfg *config.Config,
	manager *integrations.Manager, // Внедряем Manager
	bus eventbus.EventBus,
	logger logger.LoggerInterface,
	db *gorm.DB,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) ServiceDeskGateway {
	return &serviceDeskGatewayImpl{
		cfg:             cfg,
		manager:         manager,
		bus:             bus,
		logger:          logger,
		db:              db,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер в фоновом режиме.
func (g *serviceDeskGatewayImpl) Start(ctx context.Context) {
	g.logger.Info("Запуск универсального шлюза инвентаризации", "interval", g.cfg.SDeskSyncInterval)
	ticker := time.NewTicker(g.cfg.SDeskSyncInterval)
	defer ticker.Stop()

	g.runSyncCycle(ctx)

	for {
		select {
		case <-ticker.C:
			g.runSyncCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза инвентаризации...")
			return
		}
	}
}

// runSyncCycle выполняет один полный цикл синхронизации по всем провайдерам.
func (g *serviceDeskGatewayImpl) runSyncCycle(ctx context.Context) {
	g.mu.Lock()
	if g.isSyncing {
		g.logger.Warn("Цикл синхронизации уже запущен. Пропуск.")
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

	g.logger.Info("Начало нового цикла синхронизации по провайдерам.")

	providers := g.manager.GetInventoryProviders()
	for _, provider := range providers {
		select {
		case <-ctx.Done():
			return
		default:
			g.processProvider(ctx, provider)
		}
	}
	g.logger.Info("Цикл синхронизации завершен.")
}

// processProvider обрабатывает все типы сущностей для одного провайдера.
func (g *serviceDeskGatewayImpl) processProvider(ctx context.Context, provider integration.InventoryProvider) {
	log := g.logger.With("system", provider.SystemName())
	log.Info("Обработка провайдера")

	// Последовательно обрабатываем типы сущностей
	g.syncCompanies(ctx, provider, log)
	g.syncServers(ctx, provider, log)
	g.syncWorkstations(ctx, provider, log)
	g.syncFiscalRegisters(ctx, provider, log)
}

// syncCompanies синхронизирует компании.
func (g *serviceDeskGatewayImpl) syncCompanies(ctx context.Context, p integration.InventoryProvider, log logger.LoggerInterface) {
	summaries, err := p.GetCompanySummaries(ctx)
	if err != nil {
		log.Error("Не удалось получить список компаний", "error", err)
		return
	}
	g.processDiffs(ctx, p, "Company", summaries, func(extID string) (interface{}, error) {
		return p.GetCompany(ctx, extID)
	}, log)
}

// syncServers синхронизирует серверы.
func (g *serviceDeskGatewayImpl) syncServers(ctx context.Context, p integration.InventoryProvider, log logger.LoggerInterface) {
	summaries, err := p.GetServerSummaries(ctx)
	if err != nil {
		log.Error("Не удалось получить список серверов", "error", err)
		return
	}
	g.processDiffs(ctx, p, "Server", summaries, func(extID string) (interface{}, error) {
		return p.GetServer(ctx, extID)
	}, log)
}

// syncWorkstations синхронизирует рабочие станции.
func (g *serviceDeskGatewayImpl) syncWorkstations(ctx context.Context, p integration.InventoryProvider, log logger.LoggerInterface) {
	summaries, err := p.GetWorkstationSummaries(ctx)
	if err != nil {
		log.Error("Не удалось получить список рабочих станций", "error", err)
		return
	}
	g.processDiffs(ctx, p, "Workstation", summaries, func(extID string) (interface{}, error) {
		return p.GetWorkstation(ctx, extID)
	}, log)
}

// syncFiscalRegisters синхронизирует ФР.
func (g *serviceDeskGatewayImpl) syncFiscalRegisters(ctx context.Context, p integration.InventoryProvider, log logger.LoggerInterface) {
	summaries, err := p.GetFiscalRegisterSummaries(ctx)
	if err != nil {
		log.Error("Не удалось получить список ФР", "error", err)
		return
	}
	g.processDiffs(ctx, p, "FiscalRegister", summaries, func(extID string) (interface{}, error) {
		return p.GetFiscalRegister(ctx, extID)
	}, log)
}

// FetcherFunc - функция для получения полной модели по ID.
type FetcherFunc func(externalID string) (interface{}, error)

// processDiffs - универсальная функция сверки и публикации событий.
func (g *serviceDeskGatewayImpl) processDiffs(
	ctx context.Context,
	provider integration.InventoryProvider,
	entityType string,
	remoteList []integration.EntitySummary,
	fetcher FetcherFunc,
	log logger.LoggerInterface,
) {
	// 1. Получаем локальные связи для этой системы и типа сущности
	localMap, err := g.getLocalEntityLinks(ctx, provider.SystemName(), entityType)
	if err != nil {
		log.Error("Не удалось получить локальные связи", "entityType", entityType, "error", err)
		return
	}

	// 2. Сравниваем списки
	remoteExternalIDs := make(map[string]struct{}, len(remoteList))
	var toCreate, toUpdate, toDelete []string

	for _, remoteItem := range remoteList {
		remoteUUID := remoteItem.ExternalID
		remoteExternalIDs[remoteUUID] = struct{}{}

		localLink, exists := localMap[remoteUUID]
		if !exists {
			toCreate = append(toCreate, remoteUUID)
		} else if localLink.DeletedAt.Valid || (!remoteItem.UpdatedAt.IsZero() && localLink.LastModifiedDate != nil && remoteItem.UpdatedAt.After(*localLink.LastModifiedDate)) {
			toUpdate = append(toUpdate, remoteUUID)
		}
	}

	for externalUUID, localLink := range localMap {
		if _, exists := remoteExternalIDs[externalUUID]; !exists && !localLink.DeletedAt.Valid {
			toDelete = append(toDelete, externalUUID)
		}
	}

	log.Info("Сравнение завершено",
		"entityType", entityType,
		"remote_count", len(remoteList),
		"local_count", len(localMap),
		"to_create", len(toCreate),
		"to_update", len(toUpdate),
		"to_delete", len(toDelete),
	)

	// 3. Обрабатываем удаление
	if len(toDelete) > 0 {
		for _, uuid := range toDelete {
			g.bus.Publish(eventbus.Event{
				Type: events.ServiceDeskEntityDeleted,
				Payload: events.ServiceDeskEntityDeletePayload{
					EntityType:      entityType,
					ServiceDeskUUID: uuid,
				},
			})
		}
	}

	// 4. Обрабатываем создание и обновление
	if len(toCreate) > 0 || len(toUpdate) > 0 {
		uuidsToFetch := append(toCreate, toUpdate...)
		g.fetchAndPublishEvents(ctx, entityType, uuidsToFetch, fetcher, log)
	}
}

func (g *serviceDeskGatewayImpl) fetchAndPublishEvents(
	ctx context.Context,
	entityType string,
	externalUUIDs []string,
	fetcher FetcherFunc,
	log logger.LoggerInterface,
) {
	// Ограничение конкурентности
	limit := make(chan struct{}, g.cfg.ConcurrentRequests)
	var wg sync.WaitGroup

	for _, uuid := range externalUUIDs {
		wg.Add(1)
		limit <- struct{}{} // Acquire token

		go func(id string) {
			defer wg.Done()
			defer func() { <-limit }() // Release token

			// Используем переданный fetcher (он внутри вызывает Adapter.GetEntity)
			model, err := fetcher(id)
			if err != nil {
				log.Error("Не удалось получить детали сущности", "id", id, "error", err)
				return
			}

			// Публикуем событие с МОДЕЛЬЮ, а не картой
			g.bus.Publish(eventbus.Event{
				Type: events.ServiceDeskEntityUpdated,
				Payload: events.ServiceDeskEntityPayload{
					EntityType:      entityType,
					ServiceDeskUUID: id,
					Data:            model, // Теперь это struct pointer
				},
			})
		}(uuid)
	}
	wg.Wait()
}

// getLocalEntityLinks получает связи из БД с фильтром по systemName.
func (g *serviceDeskGatewayImpl) getLocalEntityLinks(ctx context.Context, systemName, entityType string) (map[string]localEntityInfo, error) {
	type result struct {
		ExternalID       string
		InternalID       string
		LastModifiedDate *time.Time
		DeletedAt        gorm.DeletedAt
	}

	var results []result
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
		return nil, fmt.Errorf("unknown entity type: %s", entityType)
	}

	// JOIN с конкретной таблицей сущности, чтобы получить LMD и DeletedAt
	err := g.db.WithContext(ctx).Table("external_system_links as l").
		Select("l.service_desk_uuid as external_id, l.internal_id, t.last_modified_date, t.deleted_at").
		Joins(fmt.Sprintf("JOIN %s as t ON l.internal_id = t.id", tableName)).
		Where("l.system_name = ? AND l.entity_type = ?", systemName, entityType).
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
