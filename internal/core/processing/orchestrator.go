// Файл: internal/core/processing/orchestrator.go
// Package processing содержит компоненты для обработки данных и бизнес-логики.
package processing

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Orchestrator — тонкий фасад над набором обработчиков событий.
//
// Он отвечает только за регистрацию подписок на EventBus и делегирование
// входящих событий соответствующим Command/Handler-обработчикам.
type Orchestrator struct {
	logger   logger.LoggerInterface
	bus      eventbus.EventBus
	handlers orchestratorHandlers
}

// NewOrchestrator создаёт новый экземпляр Оркестратора.
func NewOrchestrator(
	logger logger.LoggerInterface,
	db *gorm.DB,
	bus eventbus.EventBus,
	sdClient external.ExternalSystemClient,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	taskRepo repositories.TaskRepo,
	linkRepo repositories.LinkRepo,
	engine ProcessingEngine,
	obsService services.AgentObservationService,
) *Orchestrator {
	return &Orchestrator{
		logger: logger,
		bus:    bus,
		handlers: newOrchestratorHandlers(
			logger,
			db,
			bus,
			sdClient,
			companyRepo,
			serverRepo,
			workstationRepo,
			frRepo,
			taskRepo,
			linkRepo,
			engine,
			obsService,
		),
	}
}

// Start запускает Оркестратор, подписывая его на события системы.
func (o *Orchestrator) Start(ctx context.Context) {
	_ = ctx

	if o.bus == nil {
		o.logger.Warn("Шина событий не инициализирована, подписки Оркестратора не зарегистрированы")
		return
	}

	o.logger.Info("Оркестратор запущен и подписан на события")

	o.subscribe(events.ServiceDeskEntityUpdated, o.handleServiceDeskEntityUpdate)
	o.subscribe(events.ServiceDeskEntityDeleted, o.handleServiceDeskEntityDelete)
	o.subscribe(events.DuplicatesFound, o.handleDuplicatesFound)
	o.subscribe(events.AgentDataReceived, o.handleAgentDataReceived)
	o.subscribe(events.AgentObservationRequested, o.handleAgentObservationRequested)
	o.subscribe(events.ServerPollingSucceeded, o.handleServerPollingSucceeded)
	o.subscribe(events.ServerPollingFailed, o.handleServerPollingFailed)
	o.subscribe(events.FiscalRegisterDiscrepancyFound, o.handleFiscalRegisterDiscrepancy)

	o.logger.Debug("Подписки на события зарегистрированы",
		"events", []string{
			events.ServiceDeskEntityUpdated,
			events.ServiceDeskEntityDeleted,
			events.DuplicatesFound,
			events.AgentDataReceived,
			events.AgentObservationRequested,
			events.ServerPollingSucceeded,
			events.ServerPollingFailed,
			events.FiscalRegisterDiscrepancyFound,
		},
	)
}

func (o *Orchestrator) subscribe(eventType string, handler eventbus.EventHandler) {
	if o.bus == nil || handler == nil {
		return
	}
	o.bus.Subscribe(eventType, handler)
}

func (o *Orchestrator) dispatch(ctx context.Context, handler orchestratorEventHandler, event eventbus.Event) {
	if handler == nil {
		return
	}
	handler.Handle(ctx, event)
}

func (o *Orchestrator) handleAgentObservationRequested(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.agentObservationRequested, event)
}

func (o *Orchestrator) handleServiceDeskEntityUpdate(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.serviceDeskEntityUpdated, event)
}

func (o *Orchestrator) handleServiceDeskEntityDelete(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.serviceDeskEntityDeleted, event)
}

func (o *Orchestrator) handleContractsStatusRecalculated(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.contractsStatusRecalculated, event)
}

func (o *Orchestrator) handleDuplicatesFound(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.duplicatesFound, event)
}

func (o *Orchestrator) handleAgentDataReceived(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.agentDataReceived, event)
}

func (o *Orchestrator) handleServerPollingSucceeded(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.serverPollingSucceeded, event)
}

func (o *Orchestrator) handleServerPollingFailed(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.serverPollingFailed, event)
}

func (o *Orchestrator) handleFiscalRegisterDiscrepancy(ctx context.Context, event eventbus.Event) {
	o.dispatch(ctx, o.handlers.fiscalRegisterDiscrepancy, event)
}

func parseFlexibleEventTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"02.01.2006 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			parsed = parsed.UTC()
			return &parsed
		}
	}
	return nil
}

func trimStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func isUUIDValue(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

// getLMDFromModel извлекает LastModifiedDate из модели сущности.
func getLMDFromModel(entity any) *time.Time {
	switch v := entity.(type) {
	case *company.Company:
		return v.LastModifiedDate
	case *server.Server:
		return v.LastModifiedDate
	case *workstation.Workstation:
		return v.LastModifiedDate
	case *fiscal.FiscalRegister:
		return v.LastModifiedDate
	}
	return nil
}
