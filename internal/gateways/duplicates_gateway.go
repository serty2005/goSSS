package gateways

import (
	"context"
	"etalon-server/internal/config"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/pkg/eventbus"
	"fmt"

	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// DuplicatesGateway периодически ищет дубликаты в базе данных и публикует события.
type DuplicatesGateway interface {
	Start(ctx context.Context)
}

type duplicatesGatewayImpl struct {
	cfg    *config.Config
	db     *gorm.DB
	bus    eventbus.EventBus
	logger *zap.Logger
}

// NewDuplicatesGateway создает новый экземпляр шлюза.
func NewDuplicatesGateway(cfg *config.Config, db *gorm.DB, bus eventbus.EventBus, logger *zap.Logger) DuplicatesGateway {
	return &duplicatesGatewayImpl{
		cfg:    cfg,
		db:     db,
		bus:    bus,
		logger: logger,
	}
}

// Start запускает периодический поиск дубликатов.
func (g *duplicatesGatewayImpl) Start(ctx context.Context) {
	// Для поиска дубликатов не нужен слишком частый интервал.
	// Возьмем интервал опроса серверов как ориентир.
	interval := g.cfg.ServerPollingInterval
	g.logger.Info("Запуск шлюза поиска дубликатов", zap.Duration("interval", interval))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Первый запуск сразу
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
		{&models.Server{}, "Server", []string{"ip"}},
		{&models.Workstation{}, "Workstation", []string{"anydesk", "teamviewer", "litemanager"}},
		{&models.FiscalRegister{}, "FiscalRegister", []string{"fr_serial_number", "rn_kkt"}},
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
	log := g.logger.With(zap.String("entityType", entityType), zap.String("field", field))

	var duplicateValues []struct{ Value string }
	err := g.db.WithContext(ctx).Model(model).
		Select(fmt.Sprintf("%s as value", field)).
		Where(fmt.Sprintf("%s IS NOT NULL AND %s != ''", field, field)).
		Group(field).
		Having("count(*) > 1").
		Find(&duplicateValues).Error

	if err != nil {
		log.Error("Ошибка при поиске групп дубликатов", zap.Error(err))
		return
	}

	if len(duplicateValues) == 0 {
		return
	}

	log.Info("Найдено групп дубликатов", zap.Int("count", len(duplicateValues)))

	for _, item := range duplicateValues {
		var uuids []string
		err := g.db.WithContext(ctx).Model(model).
			Where(fmt.Sprintf("%s = ?", field), item.Value).
			Pluck("service_desk_uuid", &uuids).Error

		if err != nil {
			log.Error("Не удалось получить UUID для группы дубликатов", zap.String("value", item.Value), zap.Error(err))
			continue
		}

		if len(uuids) > 1 {
			g.bus.Publish(eventbus.Event{
				Type: events.DuplicatesFound,
				Payload: events.DuplicatesFoundPayload{
					EntityType: entityType,
					Field:      field,
					Value:      item.Value,
					UUIDs:      uuids,
				},
			})
		}
	}
}