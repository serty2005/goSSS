package gateways

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"
)

// DuplicatesGateway периодически ищет дубликаты в базе данных и публикует события.
type DuplicatesGateway interface {
	Start(ctx context.Context)
}

type duplicatesGatewayImpl struct {
	cfg         *config.Config
	db          *gorm.DB
	bus         eventbus.EventBus
	logger      logger.LoggerInterface
	deletionSvc services.EntityDeletionService
}

// NewDuplicatesGateway создает новый экземпляр шлюза.
func NewDuplicatesGateway(cfg *config.Config, db *gorm.DB, bus eventbus.EventBus, logger logger.LoggerInterface, deletionSvc services.EntityDeletionService) DuplicatesGateway {
	return &duplicatesGatewayImpl{
		cfg:         cfg,
		db:          db,
		bus:         bus,
		logger:      logger,
		deletionSvc: deletionSvc,
	}
}

// Start запускает периодический поиск дубликатов.
func (g *duplicatesGatewayImpl) Start(ctx context.Context) {
	interval := g.cfg.DuplicatesSearchInterval
	g.logger.Info("Запуск шлюза поиска дубликатов", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.runSearchCycle(ctx)

	for {
		select {
		case <-ticker.C:
			g.runSearchCycle(ctx)
		case <-ctx.Done():
			g.logger.Info("Остановка шлюза поиска дубликатов.")
			return
		}
	}
}

// runSearchCycle выполняет один полный цикл поиска по всем типам сущностей и полям.
func (g *duplicatesGatewayImpl) runSearchCycle(ctx context.Context) {
	g.logger.Info("Начало нового цикла поиска дубликатов.")

	entityConfigs := []struct {
		model      interface{}
		entityType string
		fields     []string
	}{
		{&server.Server{}, "Server", []string{"ip"}},
		{&workstation.Workstation{}, "Workstation", []string{"anydesk", "teamviewer", "litemanager", "rustdesk"}},
		{&fiscal.FiscalRegister{}, "FiscalRegister", []string{"fr_serial_number", "rn_kkt"}},
	}

	var wg sync.WaitGroup
	for _, config := range entityConfigs {
		for _, field := range config.fields {
			wg.Add(1)
			go func(m interface{}, et, f string) {
				defer wg.Done()
				g.findAndPublish(ctx, m, et, f)
			}(config.model, config.entityType, field)
		}
	}
	wg.Wait()
	g.logger.Info("Цикл поиска дубликатов завершен.")
}

// findAndPublish находит дубликаты для конкретной модели/поля и публикует события.
func (g *duplicatesGatewayImpl) findAndPublish(ctx context.Context, model interface{}, entityType, field string) {
	log := g.logger.With("entityType", entityType, "field", field)

	var duplicateValues []struct{ Value string }
	err := g.db.WithContext(ctx).Model(model).
		Select(fmt.Sprintf("%s as value", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Where("health_status != ?", "locked").
		Group(field).
		Having("count(*) > 1").
		Find(&duplicateValues).Error

	if err != nil {
		log.Error("Ошибка при поиске групп дубликатов", "error", err)
		return
	}

	if len(duplicateValues) == 0 {
		return
	}

	log.Info("Найдено групп дубликатов", "count", len(duplicateValues))

	for _, item := range duplicateValues {
		var internalIDs []string
		err := g.db.WithContext(ctx).Model(model).
			Where(fmt.Sprintf("%s = ?", field), item.Value).
			Pluck("id", &internalIDs).Error

		if err != nil {
			log.Error("Не удалось получить внутренние ID для группы дубликатов", "value", item.Value, "error", err)
			continue
		}

		if len(internalIDs) > 1 {
			if g.deletionSvc != nil {
				handled, mergeErr := g.deletionSvc.TryAutoMergeDuplicateGroup(ctx, entityType, field, item.Value, internalIDs)
				if mergeErr != nil {
					log.Error("Ошибка автосклейки дублей", "value", item.Value, "error", mergeErr)
				} else if handled {
					log.Info("Группа дублей обработана автосклейкой", "value", item.Value, "ids_count", len(internalIDs))
					continue
				}
			}
			g.bus.Publish(eventbus.Event{
				Type: events.DuplicatesFound,
				Payload: events.DuplicatesFoundPayload{
					EntityType:  entityType,
					Field:       field,
					Value:       item.Value,
					InternalIDs: internalIDs,
				},
			})
		}
	}
}
