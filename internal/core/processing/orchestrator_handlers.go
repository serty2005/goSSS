package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	dbpkg "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	processingEntityTypeCompany        = "Company"
	processingEntityTypeServer         = "Server"
	processingEntityTypeWorkstation    = "Workstation"
	processingEntityTypeFiscalRegister = "FiscalRegister"
	processingSourceAgent              = "agent"
	processingSourceRMSPolling         = "rms_polling"
	processingSourceContractGateway    = "contract_gateway"
	processingSystemNaumen             = "naumen"
	processingTaskTypeNeedUpdate       = "need_update"
)

type orchestratorEventHandler interface {
	Handle(ctx context.Context, event eventbus.Event)
}

type orchestratorHandlers struct {
	agentObservationRequested   orchestratorEventHandler
	serviceDeskEntityUpdated    orchestratorEventHandler
	serviceDeskEntityDeleted    orchestratorEventHandler
	contractsStatusRecalculated orchestratorEventHandler
	duplicatesFound             orchestratorEventHandler
	agentDataReceived           orchestratorEventHandler
	serverPollingSucceeded      orchestratorEventHandler
	serverPollingFailed         orchestratorEventHandler
	fiscalRegisterDiscrepancy   orchestratorEventHandler
}

func newOrchestratorHandlers(
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
) orchestratorHandlers {
	transactor := dbpkg.NewGormTransactor(db)
	entityExecutor := newProcessingEntityExecutor(
		logger.With("component", "processing_entity_executor"),
		companyRepo,
		serverRepo,
		workstationRepo,
		frRepo,
	)
	planExecutor := newProcessingPlanExecutor(
		logger.With("component", "processing_plan_executor"),
		db,
		entityExecutor,
	)

	return orchestratorHandlers{
		agentObservationRequested: &agentObservationRequestedHandler{
			logger:          logger.With("handler", "agent_observation_requested"),
			bus:             bus,
			obsService:      obsService,
			workstationRepo: workstationRepo,
			frRepo:          frRepo,
		},
		serviceDeskEntityUpdated: &serviceDeskEntityUpdatedHandler{
			logger:          logger.With("handler", "servicedesk_entity_updated"),
			tm:              transactor,
			sdClient:        sdClient,
			companyRepo:     companyRepo,
			serverRepo:      serverRepo,
			workstationRepo: workstationRepo,
			frRepo:          frRepo,
			linkRepo:        linkRepo,
			engine:          engine,
			planExecutor:    planExecutor,
		},
		serviceDeskEntityDeleted: &serviceDeskEntityDeletedHandler{
			logger:         logger.With("handler", "servicedesk_entity_deleted"),
			tm:             transactor,
			linkRepo:       linkRepo,
			entityExecutor: entityExecutor,
		},
		contractsStatusRecalculated: &contractsStatusRecalculatedHandler{
			logger:         logger.With("handler", "contracts_status_recalculated"),
			tm:             transactor,
			db:             db,
			entityExecutor: entityExecutor,
		},
		duplicatesFound: &duplicatesFoundHandler{
			logger:       logger.With("handler", "duplicates_found"),
			tm:           transactor,
			engine:       engine,
			planExecutor: planExecutor,
		},
		agentDataReceived: &agentDataReceivedHandler{
			logger:       logger.With("handler", "agent_data_received"),
			tm:           transactor,
			engine:       engine,
			planExecutor: planExecutor,
		},
		serverPollingSucceeded: &serverPollingSucceededHandler{
			logger:         logger.With("handler", "server_polling_succeeded"),
			entityExecutor: entityExecutor,
		},
		serverPollingFailed: &serverPollingFailedHandler{
			logger:         logger.With("handler", "server_polling_failed"),
			entityExecutor: entityExecutor,
		},
		fiscalRegisterDiscrepancy: &fiscalRegisterDiscrepancyHandler{
			logger:   logger.With("handler", "fiscal_register_discrepancy"),
			db:       db,
			taskRepo: taskRepo,
		},
	}
}

type processingEntityExecutor struct {
	logger          logger.LoggerInterface
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

func newProcessingEntityExecutor(
	logger logger.LoggerInterface,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) *processingEntityExecutor {
	return &processingEntityExecutor{
		logger:          logger,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

func (e *processingEntityExecutor) Create(ctx context.Context, entity any) (string, error) {
	switch v := entity.(type) {
	case *company.Company:
		if e.companyRepo == nil {
			return "", fmt.Errorf("репозиторий компаний не инициализирован")
		}
		if err := e.companyRepo.Create(ctx, v); err != nil {
			return "", err
		}
		return v.ID, nil
	case *server.Server:
		if e.serverRepo == nil {
			return "", fmt.Errorf("репозиторий серверов не инициализирован")
		}
		if err := e.serverRepo.Create(ctx, nil, v); err != nil {
			return "", err
		}
		return v.ID, nil
	case *workstation.Workstation:
		if e.workstationRepo == nil {
			return "", fmt.Errorf("репозиторий рабочих станций не инициализирован")
		}
		if err := e.workstationRepo.Create(ctx, nil, v); err != nil {
			return "", err
		}
		return v.ID, nil
	case *fiscal.FiscalRegister:
		if e.frRepo == nil {
			return "", fmt.Errorf("репозиторий фискальных регистраторов не инициализирован")
		}
		if err := e.frRepo.Create(ctx, nil, v); err != nil {
			return "", err
		}
		return v.ID, nil
	default:
		return "", fmt.Errorf("неподдерживаемый тип для создания: %T", entity)
	}
}

func (e *processingEntityExecutor) Update(ctx context.Context, entityType, internalID string, updates map[string]any) error {
	switch entityType {
	case processingEntityTypeCompany:
		if e.companyRepo == nil {
			return fmt.Errorf("репозиторий компаний не инициализирован")
		}
		_, err := e.companyRepo.Update(ctx, internalID, updates)
		return err
	case processingEntityTypeServer:
		if e.serverRepo == nil {
			return fmt.Errorf("репозиторий серверов не инициализирован")
		}
		_, err := e.serverRepo.Update(ctx, nil, internalID, updates)
		return err
	case processingEntityTypeWorkstation:
		if e.workstationRepo == nil {
			return fmt.Errorf("репозиторий рабочих станций не инициализирован")
		}
		_, err := e.workstationRepo.Update(ctx, nil, internalID, updates)
		return err
	case processingEntityTypeFiscalRegister:
		if e.frRepo == nil {
			return fmt.Errorf("репозиторий фискальных регистраторов не инициализирован")
		}
		_, err := e.frRepo.Update(ctx, nil, internalID, updates)
		return err
	default:
		return fmt.Errorf("неподдерживаемый тип для обновления: %s", entityType)
	}
}

func (e *processingEntityExecutor) Delete(ctx context.Context, entityType, internalID string) error {
	switch entityType {
	case processingEntityTypeCompany:
		if e.companyRepo == nil {
			return fmt.Errorf("репозиторий компаний не инициализирован")
		}
		_, err := e.companyRepo.Delete(ctx, internalID)
		return err
	case processingEntityTypeServer:
		if e.serverRepo == nil {
			return fmt.Errorf("репозиторий серверов не инициализирован")
		}
		_, err := e.serverRepo.Delete(ctx, nil, internalID)
		return err
	case processingEntityTypeWorkstation:
		if e.workstationRepo == nil {
			return fmt.Errorf("репозиторий рабочих станций не инициализирован")
		}
		_, err := e.workstationRepo.Delete(ctx, nil, internalID)
		return err
	case processingEntityTypeFiscalRegister:
		if e.frRepo == nil {
			return fmt.Errorf("репозиторий фискальных регистраторов не инициализирован")
		}
		_, err := e.frRepo.Delete(ctx, nil, internalID)
		return err
	default:
		return fmt.Errorf("неподдерживаемый тип для удаления: %s", entityType)
	}
}

func (e *processingEntityExecutor) AddAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	if e.serverRepo == nil {
		return fmt.Errorf("репозиторий серверов не инициализирован")
	}
	return e.serverRepo.AddAdditionalOwner(ctx, serverID, companyID)
}

func (e *processingEntityExecutor) LockEquipmentByOwners(ctx context.Context, ownerIDs []string) error {
	for _, ownerID := range ownerIDs {
		if e.serverRepo != nil {
			if err := e.serverRepo.LockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
		if e.workstationRepo != nil {
			if err := e.workstationRepo.LockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
		if e.frRepo != nil {
			if err := e.frRepo.LockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *processingEntityExecutor) UnlockEquipmentByOwners(ctx context.Context, ownerIDs []string) error {
	for _, ownerID := range ownerIDs {
		if e.serverRepo != nil {
			if err := e.serverRepo.UnlockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
		if e.workstationRepo != nil {
			if err := e.workstationRepo.UnlockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
		if e.frRepo != nil {
			if err := e.frRepo.UnlockByOwner(ctx, nil, ownerID); err != nil {
				return err
			}
		}
	}
	return nil
}

type processingPlanExecutionOptions struct {
	AllowCreateTask         bool
	AllowCreate             bool
	AllowUpdate             bool
	AllowAddAdditionalOwner bool
	UpdateSource            string
}

type processingPlanExecutionSummary struct {
	LastEntityID string
}

type processingPlanExecutor struct {
	logger         logger.LoggerInterface
	db             *gorm.DB
	entityExecutor *processingEntityExecutor
}

func newProcessingPlanExecutor(
	logger logger.LoggerInterface,
	db *gorm.DB,
	entityExecutor *processingEntityExecutor,
) *processingPlanExecutor {
	return &processingPlanExecutor{
		logger:         logger,
		db:             db,
		entityExecutor: entityExecutor,
	}
}

func (e *processingPlanExecutor) Execute(
	ctx context.Context,
	result *ProcessingResult,
	log logger.LoggerInterface,
	opts processingPlanExecutionOptions,
) (*processingPlanExecutionSummary, error) {
	summary := &processingPlanExecutionSummary{}
	if result == nil {
		return summary, nil
	}

	conn := dbpkg.ExtractDB(ctx, e.db)
	for i, action := range result.Actions {
		log.Debug("Выполнение действия",
			"action_index", i,
			"action_type", action.Type,
		)

		switch action.Type {
		case ActionCreateTask:
			if !opts.AllowCreateTask {
				return nil, fmt.Errorf("действие %s запрещено для текущего обработчика", action.Type)
			}
			if conn == nil {
				return nil, fmt.Errorf("транзакция для создания задачи не инициализирована")
			}
			if action.Task == nil {
				return nil, fmt.Errorf("полезная нагрузка действия %s не содержит задачу", action.Type)
			}
			if err := conn.WithContext(ctx).Create(action.Task).Error; err != nil {
				return nil, err
			}
		case ActionCreate:
			if !opts.AllowCreate {
				return nil, fmt.Errorf("действие %s запрещено для текущего обработчика", action.Type)
			}
			entityID, err := e.entityExecutor.Create(ctx, action.EntityToCreate)
			if err != nil {
				return nil, err
			}
			summary.LastEntityID = entityID
		case ActionUpdate:
			if !opts.AllowUpdate {
				return nil, fmt.Errorf("действие %s запрещено для текущего обработчика", action.Type)
			}
			updates := cloneStringAnyMap(action.Updates)
			if opts.UpdateSource != "" {
				if updates == nil {
					updates = make(map[string]any)
				}
				updates["last_updated_by"] = opts.UpdateSource
			}
			if err := e.entityExecutor.Update(ctx, action.EntityType, action.EntityUUID, updates); err != nil {
				return nil, err
			}
			if strings.TrimSpace(action.EntityUUID) != "" {
				summary.LastEntityID = action.EntityUUID
			}
		case ActionAddAdditionalOwner:
			if !opts.AllowAddAdditionalOwner {
				return nil, fmt.Errorf("действие %s запрещено для текущего обработчика", action.Type)
			}
			if err := e.entityExecutor.AddAdditionalOwner(ctx, action.EntityUUID, action.AdditionalOwnerUUID); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("неподдерживаемое действие плана: %s", action.Type)
		}
	}

	return summary, nil
}

type agentObservationRequestedHandler struct {
	logger          logger.LoggerInterface
	bus             eventbus.EventBus
	obsService      services.AgentObservationService
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

func (h *agentObservationRequestedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие AgentObservationRequested", "event_type", event.Type)

	payload, ok := event.Payload.(events.AgentObservationPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события AgentObservationRequested",
			"expected_type", "events.AgentObservationPayload",
		)
		return
	}
	if h.obsService == nil {
		h.logger.Error("Сервис наблюдений агента не инициализирован")
		return
	}

	traceID := payload.TraceID
	if strings.TrimSpace(traceID) == "" {
		traceID = uuid.New().String()
	}

	log := h.logger.With(
		"trace_id", traceID,
		"operation", "handle_observation",
		"source", payload.Source,
	)
	log.Debug("Начало обработки наблюдения агента",
		"has_server_data", payload.Data.URLRms != "",
		"has_workstation_data", payload.Data.Hostname != "",
		"has_fiscal_data", payload.Data.SerialNumber != "",
	)

	ctxWithTrace := contextkeys.WithTraceID(ctx, traceID)
	obs, err := h.obsService.ApplyObservation(ctxWithTrace, payload.Source, &payload.Data)
	if err != nil {
		log.Error("Не удалось применить наблюдение агента", "error", err)
		return
	}

	h.publishAgentObservationUpdate(ctxWithTrace, payload.Source, &payload.Data, obs)
	log.Debug("Наблюдение агента успешно применено")
}

func (h *agentObservationRequestedHandler) publishAgentObservationUpdate(
	ctx context.Context,
	source string,
	data *api.AgentDataDTO,
	obs *models.AgentObservation,
) {
	if obs == nil || h.bus == nil {
		return
	}

	agentUUID := strings.TrimSpace(source)
	if data != nil && strings.TrimSpace(data.AgentUUID) != "" {
		agentUUID = strings.TrimSpace(data.AgentUUID)
	}
	if !isUUIDValue(agentUUID) {
		agentUUID = ""
	}

	var ownerMatch *bool
	var wsOwner string
	var frOwner string
	var workstationName string
	var frName string

	if h.workstationRepo != nil && obs.WorkstationID != nil && strings.TrimSpace(*obs.WorkstationID) != "" {
		if ws, err := h.workstationRepo.GetByID(ctx, strings.TrimSpace(*obs.WorkstationID)); err == nil && ws != nil {
			if ws.OwnerID != nil {
				wsOwner = strings.TrimSpace(*ws.OwnerID)
			}
			if ws.DeviceName != nil {
				workstationName = strings.TrimSpace(*ws.DeviceName)
			}
		}
	}

	if h.frRepo != nil && obs.FRID != nil && strings.TrimSpace(*obs.FRID) != "" {
		if fr, err := h.frRepo.GetByID(ctx, strings.TrimSpace(*obs.FRID)); err == nil && fr != nil {
			if fr.OwnerID != nil {
				frOwner = strings.TrimSpace(*fr.OwnerID)
			}
			if fr.ModelKKT != nil && strings.TrimSpace(*fr.ModelKKT) != "" {
				frName = strings.TrimSpace(*fr.ModelKKT)
			} else if fr.RNKKT != nil && strings.TrimSpace(*fr.RNKKT) != "" {
				frName = strings.TrimSpace(*fr.RNKKT)
			} else if fr.FRSerialNumber != nil && strings.TrimSpace(*fr.FRSerialNumber) != "" {
				frName = strings.TrimSpace(*fr.FRSerialNumber)
			}
		}
	}

	if wsOwner != "" && frOwner != "" {
		match := wsOwner == frOwner
		ownerMatch = &match
	}

	currentRaw := ""
	vTimeRaw := ""
	agentVC := ""
	serverURL := ""
	if data != nil {
		currentRaw = strings.TrimSpace(data.CurrentTime)
		vTimeRaw = strings.TrimSpace(data.VTime)
		agentVC = strings.TrimSpace(data.VC)
		serverURL = strings.TrimSpace(data.URLRms)
	}

	payload := events.AgentObservationUpdatedPayload{
		ObservationID:   obs.ID,
		AgentUUID:       stringPtrOrNil(agentUUID),
		AgentVC:         stringPtrOrNil(agentVC),
		WorkstationID:   trimStringPtr(obs.WorkstationID),
		WorkstationName: stringPtrOrNil(workstationName),
		FRID:            trimStringPtr(obs.FRID),
		FRName:          stringPtrOrNil(frName),
		OwnerMatch:      ownerMatch,
		ObservedAt:      obs.ObservedAt,
		CurrentTime:     parseFlexibleEventTime(currentRaw),
		VTime:           parseFlexibleEventTime(vTimeRaw),
		CurrentRaw:      stringPtrOrNil(currentRaw),
		VTimeRaw:        stringPtrOrNil(vTimeRaw),
		ServerURL:       stringPtrOrNil(serverURL),
	}
	h.bus.Publish(eventbus.Event{Type: events.AgentObservationUpdated, Payload: payload})
}

type serviceDeskEntityUpdatedHandler struct {
	logger          logger.LoggerInterface
	tm              interfaces.Transactor
	sdClient        external.ExternalSystemClient
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	linkRepo        repositories.LinkRepo
	engine          ProcessingEngine
	planExecutor    *processingPlanExecutor
}

func (h *serviceDeskEntityUpdatedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие ServiceDeskEntityUpdated", "event_type", event.Type)

	payload, ok := event.Payload.(events.ServiceDeskEntityPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityUpdated",
			"expected_type", "events.ServiceDeskEntityPayload",
		)
		return
	}
	log := h.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)
	log.Debug("Начало обработки обновления сущности из ServiceDesk")

	var isNewEntity bool
	var internalID string
	err := h.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := dbpkg.ExtractDB(txCtx, nil)
		mapperCtx := &external.MapperContext{
			DB:       tx,
			LinkRepo: h.linkRepo,
			Logger:   log,
		}

		link, err := h.linkRepo.GetByExternalID(txCtx, tx, processingSystemNaumen, payload.ServiceDeskUUID)
		if err != nil {
			return fmt.Errorf("ошибка поиска связи по внешнему ID: %w", err)
		}
		isNewEntity = link == nil
		if link != nil {
			internalID = link.InternalID
		}

		newEntityModel, currentEntity, err := h.resolveServiceDeskEntity(txCtx, payload, mapperCtx, isNewEntity, internalID)
		if err != nil {
			log.Warn("Пропуск обработки сущности из-за ошибки маппинга", "error", err)
			return nil
		}

		result, err := h.engine.ProcessServiceDeskUpdate(txCtx, isNewEntity, payload.EntityType, currentEntity, newEntityModel)
		if err != nil {
			return fmt.Errorf("ошибка в движке обработки: %w", err)
		}

		summary, err := h.planExecutor.Execute(txCtx, result, log, processingPlanExecutionOptions{
			AllowCreate: true,
			AllowUpdate: true,
		})
		if err != nil {
			return err
		}
		if summary.LastEntityID != "" {
			internalID = summary.LastEntityID
		}

		if isNewEntity && internalID != "" {
			newLink := &models.ExternalSystemLink{
				InternalID:      internalID,
				SystemName:      processingSystemNaumen,
				ServiceDeskUUID: payload.ServiceDeskUUID,
				EntityType:      payload.EntityType,
				LastSyncedAt:    time.Now(),
			}
			if err := h.linkRepo.Create(txCtx, tx, newLink); err != nil {
				return fmt.Errorf("ошибка создания связи: %w", err)
			}
			log.Debug("Связь создана", "internal_id", internalID, "external_id", payload.ServiceDeskUUID)
		}

		return nil
	})

	if err != nil {
		log.Error("Ошибка в транзакции обработки обновления из SD", "error", err)
		return
	}
	if isNewEntity {
		log.Info("Новая сущность успешно создана", "internalID", internalID)
		return
	}
	log.Debug("Обработка существующей сущности завершена")
}

func (h *serviceDeskEntityUpdatedHandler) resolveServiceDeskEntity(
	ctx context.Context,
	payload events.ServiceDeskEntityPayload,
	mapperCtx *external.MapperContext,
	isNewEntity bool,
	internalID string,
) (any, any, error) {
	if dataMap, isMap := payload.Data.(map[string]any); isMap {
		newEntityModel, err := h.mapDataToEntity(ctx, payload.EntityType, mapperCtx, dataMap)
		if err != nil {
			return nil, nil, err
		}
		if isNewEntity {
			return newEntityModel, nil, nil
		}
		currentEntity, err := h.loadCurrentEntity(ctx, payload.EntityType, internalID)
		return newEntityModel, currentEntity, err
	}

	if isNewEntity {
		return payload.Data, nil, nil
	}
	currentEntity, err := h.loadCurrentEntity(ctx, payload.EntityType, internalID)
	return payload.Data, currentEntity, err
}

func (h *serviceDeskEntityUpdatedHandler) mapDataToEntity(
	ctx context.Context,
	entityType string,
	mapperCtx *external.MapperContext,
	data map[string]any,
) (any, error) {
	switch entityType {
	case processingEntityTypeCompany:
		return h.sdClient.Mapper().DataToCompany(ctx, mapperCtx, data)
	case processingEntityTypeServer:
		return h.sdClient.Mapper().DataToServer(ctx, mapperCtx, data)
	case processingEntityTypeWorkstation:
		return h.sdClient.Mapper().DataToWorkstation(ctx, mapperCtx, data)
	case processingEntityTypeFiscalRegister:
		return h.sdClient.Mapper().DataToFiscalRegister(ctx, mapperCtx, data)
	default:
		return nil, fmt.Errorf("неизвестный тип сущности для обработки: %s", entityType)
	}
}

func (h *serviceDeskEntityUpdatedHandler) loadCurrentEntity(ctx context.Context, entityType, internalID string) (any, error) {
	switch entityType {
	case processingEntityTypeCompany:
		return h.companyRepo.GetByIDUnscoped(ctx, internalID)
	case processingEntityTypeServer:
		return h.serverRepo.GetByIDUnscoped(ctx, internalID)
	case processingEntityTypeWorkstation:
		return h.workstationRepo.GetByIDUnscoped(ctx, internalID)
	case processingEntityTypeFiscalRegister:
		return h.frRepo.GetByIDUnscoped(ctx, internalID)
	default:
		return nil, fmt.Errorf("неизвестный тип сущности для загрузки: %s", entityType)
	}
}

type serviceDeskEntityDeletedHandler struct {
	logger         logger.LoggerInterface
	tm             interfaces.Transactor
	linkRepo       repositories.LinkRepo
	entityExecutor *processingEntityExecutor
}

func (h *serviceDeskEntityDeletedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие ServiceDeskEntityDeleted", "event_type", event.Type)

	payload, ok := event.Payload.(events.ServiceDeskEntityDeletePayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityDeleted")
		return
	}
	log := h.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)

	err := h.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := dbpkg.ExtractDB(txCtx, nil)
		link, err := h.linkRepo.GetByExternalID(txCtx, tx, processingSystemNaumen, payload.ServiceDeskUUID)
		if err != nil {
			return err
		}
		if link == nil {
			log.Warn("Связь для удаляемой сущности не найдена, возможно, она уже удалена")
			return nil
		}

		if err := h.entityExecutor.Delete(txCtx, payload.EntityType, link.InternalID); err != nil {
			return err
		}
		if err := tx.Delete(link).Error; err != nil {
			return fmt.Errorf("ошибка удаления связи: %w", err)
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при мягком удалении сущности", "error", err)
		return
	}
	log.Info("Сущность и ее связь успешно мягко удалены")
}

type contractsStatusRecalculatedHandler struct {
	logger         logger.LoggerInterface
	tm             interfaces.Transactor
	db             *gorm.DB
	entityExecutor *processingEntityExecutor
}

func (h *contractsStatusRecalculatedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие ContractsStatusRecalculated", "event_type", event.Type)

	payload, ok := event.Payload.(events.ContractsStatusPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события ContractsStatusRecalculated")
		return
	}
	log := h.logger.With("event", event.Type)

	activeIDs := make([]string, 0)
	inactiveIDs := make([]string, 0)
	for id, isActive := range payload.CompanyActiveContract {
		if isActive {
			activeIDs = append(activeIDs, id)
		} else {
			inactiveIDs = append(inactiveIDs, id)
		}
	}

	err := h.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		tx := dbpkg.ExtractDB(txCtx, h.db)
		if tx == nil {
			return fmt.Errorf("транзакция для обновления контрактов не инициализирована")
		}

		if len(activeIDs) > 0 {
			if err := tx.WithContext(txCtx).
				Model(&company.Company{}).
				Where("id IN ?", activeIDs).
				Updates(map[string]any{
					"active_contract": true,
					"last_updated_by": processingSourceContractGateway,
				}).Error; err != nil {
				return err
			}
		}
		if len(inactiveIDs) > 0 {
			if err := tx.WithContext(txCtx).
				Model(&company.Company{}).
				Where("id IN ?", inactiveIDs).
				Updates(map[string]any{
					"active_contract": false,
					"last_updated_by": processingSourceContractGateway,
				}).Error; err != nil {
				return err
			}
		}
		if len(inactiveIDs) > 0 {
			if err := h.entityExecutor.LockEquipmentByOwners(txCtx, inactiveIDs); err != nil {
				return err
			}
		}
		if len(activeIDs) > 0 {
			if err := h.entityExecutor.UnlockEquipmentByOwners(txCtx, activeIDs); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов контрактов и оборудования", "error", err)
		return
	}
	log.Info("Обновление статусов контрактов и оборудования успешно завершено")
}

type duplicatesFoundHandler struct {
	logger       logger.LoggerInterface
	tm           interfaces.Transactor
	engine       ProcessingEngine
	planExecutor *processingPlanExecutor
}

func (h *duplicatesFoundHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие DuplicatesFound", "event_type", event.Type)

	payload, ok := event.Payload.(events.DuplicatesFoundPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события DuplicatesFound")
		return
	}
	log := h.logger.With("entityType", payload.EntityType, "field", payload.Field, "value", payload.Value)

	result := h.engine.ProcessDuplicates(ctx, payload)
	if result == nil || len(result.Actions) == 0 {
		log.Debug("Движок не вернул действий для обработки дубликатов")
		return
	}

	err := h.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
		_, err := h.planExecutor.Execute(txCtx, result, log, processingPlanExecutionOptions{
			AllowUpdate: true,
		})
		return err
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов для дубликатов", "error", err)
		return
	}
	log.Info("Статусы для группы дубликатов успешно обновлены", "count", len(result.Actions))
}

type agentDataReceivedHandler struct {
	logger       logger.LoggerInterface
	tm           interfaces.Transactor
	engine       ProcessingEngine
	planExecutor *processingPlanExecutor
}

func (h *agentDataReceivedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие AgentDataReceived", "event_type", event.Type)

	payload, ok := event.Payload.(events.AgentDataPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события AgentDataReceived",
			"expected_type", "events.AgentDataPayload",
		)
		return
	}

	traceID := payload.TraceID
	if strings.TrimSpace(traceID) == "" {
		traceID = uuid.New().String()
	}

	log := h.logger.With(
		"trace_id", traceID,
		"operation", "handle_agent_data",
		"source", payload.Source,
	)
	log.Debug("Начало обработки события AgentDataReceived",
		"has_rms_url", payload.Data.URLRms != "",
		"has_hostname", payload.Data.Hostname != "",
		"has_serial", payload.Data.SerialNumber != "",
	)

	ctxWithTrace := contextkeys.WithTraceID(ctx, traceID)
	result := h.engine.ProcessAgentData(ctxWithTrace, payload.Source, &payload.Data)
	if result == nil || len(result.Actions) == 0 {
		log.Debug("Движок не вернул никаких действий для выполнения")
		return
	}

	err := h.tm.WithinTransaction(ctxWithTrace, func(txCtx context.Context) error {
		_, err := h.planExecutor.Execute(txCtx, result, log, processingPlanExecutionOptions{
			AllowCreateTask:         true,
			AllowCreate:             true,
			AllowUpdate:             true,
			AllowAddAdditionalOwner: true,
			UpdateSource:            processingSourceAgent,
		})
		return err
	})

	if err != nil {
		log.Error("Ошибка при выполнении плана действий от движка", "error", err)
		return
	}
	log.Info("План действий от движка успешно выполнен", "actions_count", len(result.Actions))
}

type serverPollingSucceededHandler struct {
	logger         logger.LoggerInterface
	entityExecutor *processingEntityExecutor
}

func (h *serverPollingSucceededHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие ServerPollingSucceeded", "event_type", event.Type)

	payload, ok := event.Payload.(events.ServerPollingSucceededPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события ServerPollingSucceeded")
		return
	}
	log := h.logger.With("request_id", payload.RequestID)

	updates := map[string]any{
		"server_name":     payload.ServerName,
		"server_edition":  payload.ServerEdition,
		"server_version":  payload.ServerVersion,
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": processingSourceRMSPolling,
	}
	if strings.TrimSpace(payload.CRMID) != "" {
		updates["crm_id"] = strings.TrimSpace(payload.CRMID)
	}

	if err := h.entityExecutor.Update(ctx, processingEntityTypeServer, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить данные сервера после успешного опроса", "error", err)
		return
	}
	log.Info("Данные сервера успешно обновлены", "new_status", payload.NewStatus)
}

type serverPollingFailedHandler struct {
	logger         logger.LoggerInterface
	entityExecutor *processingEntityExecutor
}

func (h *serverPollingFailedHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие ServerPollingFailed", "event_type", event.Type)

	payload, ok := event.Payload.(events.ServerPollingFailedPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события ServerPollingFailed")
		return
	}
	log := h.logger.With("request_id", payload.RequestID)

	updates := map[string]any{
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": processingSourceRMSPolling,
	}

	if err := h.entityExecutor.Update(ctx, processingEntityTypeServer, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить статус сервера после неудачного опроса", "error", err)
		return
	}
	log.Info("Статус сервера обновлен после неудачного опроса", "new_status", payload.NewStatus)
}

type fiscalRegisterDiscrepancyHandler struct {
	logger   logger.LoggerInterface
	db       *gorm.DB
	taskRepo repositories.TaskRepo
}

func (h *fiscalRegisterDiscrepancyHandler) Handle(ctx context.Context, event eventbus.Event) {
	h.logger.Debug("Получено событие FiscalRegisterDiscrepancyFound", "event_type", event.Type)

	payload, ok := event.Payload.(events.FiscalRegisterDiscrepancyPayload)
	if !ok {
		h.logger.Error("Некорректная полезная нагрузка для события FiscalRegisterDiscrepancyFound")
		return
	}
	log := h.logger.With("fr_internal_uuid", payload.FRInternalUUID)

	existingTask, err := h.taskRepo.FindActiveTask(ctx, processingTaskTypeNeedUpdate, payload.FRInternalUUID)
	if err != nil {
		log.Error("Ошибка при поиске существующей задачи need_update", "error", err)
		return
	}
	if existingTask != nil {
		log.Debug("Активная задача need_update для этого ФР уже существует, новая не создается",
			"existing_task_id", existingTask.ID,
		)
		return
	}

	var commentBuilder strings.Builder
	commentBuilder.WriteString(fmt.Sprintf(
		"Обнаружено расхождение данных для ФР (внутр. ID: %s, внешн. ID: %s) между эталонной БД и ServiceDesk. Требуется обновить данные в ServiceDesk.\n\nРасхождения:\n",
		payload.FRInternalUUID,
		payload.FRServiceDeskUUID,
	))
	for field, details := range payload.Discrepancies {
		commentBuilder.WriteString(fmt.Sprintf(
			"- Поле '%s':\n  - Эталон: %v\n  - ServiceDesk: %v\n",
			field,
			details.EtalonValue,
			details.ServiceDeskValue,
		))
	}

	detailsJSON, err := json.Marshal(payload)
	if err != nil {
		log.Error("Не удалось сериализовать детали расхождения ФР", "error", err)
		return
	}

	task := models.ReconciliationTask{
		TaskType:   processingTaskTypeNeedUpdate,
		EntityType: processingEntityTypeFiscalRegister,
		EntityUUID: payload.FRInternalUUID,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    commentBuilder.String(),
	}

	if h.db == nil {
		log.Error("База данных для создания задачи расхождения ФР не инициализирована")
		return
	}
	if err := h.db.WithContext(ctx).Create(&task).Error; err != nil {
		log.Error("Не удалось создать задачу need_update", "error", err)
		return
	}
	log.Info("Успешно создана задача need_update на основе расхождений данных ФР", "task_id", task.ID)
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]any, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func internalIDFromEntity(entity any) string {
	if entity == nil {
		return ""
	}
	value := reflect.ValueOf(entity)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		return ""
	}
	field := value.Elem().FieldByName("ID")
	if !field.IsValid() || field.Kind() != reflect.String {
		return ""
	}
	return strings.TrimSpace(field.String())
}
