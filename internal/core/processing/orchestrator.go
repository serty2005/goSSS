// Файл: internal/core/processing/orchestrator.go
// Package processing содержит компоненты для обработки данных и бизнес-логики.
// Orchestrator — центральный координатор, который получает события от EventBus,
// делегирует анализ данных ProcessingEngine и выполняет полученный план действий.
package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// contextKey — тип для ключей контекста.
type contextKey string

// transactionKey — ключ для хранения транзакции *gorm.DB в context.Context.
// Используется для передачи транзакции между методами внутри одного запроса.
const transactionKey contextKey = "tx"

// Orchestrator — центральный сервис для обработки бизнес-логики на основе событий.
//
// Архитектурная роль:
//   - Является "Исполнителем" (Executor) в паттерне Command Pattern.
//   - Получает события от EventBus, анализирует их через ProcessingEngine.
//   - Выполняет полученный план действий (Actions) в транзакциях.
//
// Связи с компонентами:
//   - ProcessingEngine — анализирует данные и принимает решения.
//   - EventBus — источник событий (подписка в Start()).
//   - Repositories — выполняют CRUD-операции с БД.
//   - ExternalSystemClient — клиент для взаимодействия с ServiceDesk (Naumen).
type Orchestrator struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	engine          ProcessingEngine
	obsService      services.AgentObservationService
}

// NewOrchestrator создаёт новый экземпляр Оркестратора.
//
// Параметры:
//   - logger — интерфейс логирования для отладки и мониторинга.
//   - db — соединение с БД для выполнения транзакций.
//   - bus — шина событий для подписки на события системы.
//   - sdClient — клиент внешней системы ServiceDesk.
//   - companyRepo, serverRepo, workstationRepo, frRepo — репозитории для работы с сущностями.
//   - taskRepo — репозиторий задач согласования.
//   - linkRepo — репозиторий связей между внутренними и внешними сущностями.
//   - engine — движок обработки для анализа данных и принятия решений.
//   - obsService — сервис обработки наблюдений агентов.
//
// Возвращает готовый к работе Оркестратор (требуется вызов Start() для активации).
func NewOrchestrator(
	logger logger.LoggerInterface, db *gorm.DB, bus eventbus.EventBus, sdClient external.ExternalSystemClient,
	companyRepo company.Repository, serverRepo server.Repository,
	workstationRepo workstation.Repository, frRepo fiscal.Repository,
	taskRepo repositories.TaskRepo, linkRepo repositories.LinkRepo, engine ProcessingEngine, obsService services.AgentObservationService,
) *Orchestrator {
	return &Orchestrator{
		logger, db, bus, sdClient, companyRepo, serverRepo, workstationRepo,
		frRepo, taskRepo, linkRepo, engine, obsService,
	}
}

// Start запускает Оркестратор, подписывая его на события системы.
//
// Подписываемые события:
//   - ServiceDeskEntityUpdated — обновление сущности из ServiceDesk.
//   - ServiceDeskEntityDeleted — удаление сущности из ServiceDesk.
//   - DuplicatesFound — обнаружены дубликаты сущностей.
//   - AgentDataReceived — получены данные от агента мониторинга.
//   - AgentObservationRequested — запрос на применение наблюдения агента.
//   - ServerPollingSucceeded/Failed — результаты опроса сервера.
//   - FiscalRegisterDiscrepancyFound — расхождение данных ФР между БД и ServiceDesk.
//
// Метод должен вызываться один раз при старте приложения.
func (o *Orchestrator) Start(ctx context.Context) {
	o.logger.Info("Оркестратор запущен и подписан на события")

	// Подписка на события обновления сущностей из ServiceDesk
	o.bus.Subscribe(events.ServiceDeskEntityUpdated, o.handleServiceDeskEntityUpdate)
	o.bus.Subscribe(events.ServiceDeskEntityDeleted, o.handleServiceDeskEntityDelete)

	// Временно не подписываемся на ContractsStatusRecalculated:
	// текущий пересчет контрактов выполняется напрямую в contract service.
	// Подписка будет возвращена после обновления контура контрактной синхронизации.

	// Подписка на события дубликатов и агентских данных
	o.bus.Subscribe(events.DuplicatesFound, o.handleDuplicatesFound)
	o.bus.Subscribe(events.AgentDataReceived, o.handleAgentDataReceived)
	o.bus.Subscribe(events.AgentObservationRequested, o.handleAgentObservationRequested)

	// Подписка на события опроса серверов
	o.bus.Subscribe(events.ServerPollingSucceeded, o.handleServerPollingSucceeded)
	o.bus.Subscribe(events.ServerPollingFailed, o.handleServerPollingFailed)

	// Подписка на события расхождения данных ФР
	o.bus.Subscribe(events.FiscalRegisterDiscrepancyFound, o.handleFiscalRegisterDiscrepancy)

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

// handleAgentObservationRequested обрабатывает запрос на применение наблюдения агента.
//
// Это событие инициируется AgentFTPGateway или другими источниками агентских данных.
// Наблюдение содержит данные о сервере, рабочей станции и/или фискальном регистраторе.
//
// Параметры:
//   - ctx — контекст запроса.
//   - event — событие с полезной нагрузкой AgentObservationPayload.
//
// Делегирует обработку AgentObservationService.ApplyObservation().
func (o *Orchestrator) handleAgentObservationRequested(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие AgentObservationRequested",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.AgentObservationPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события AgentObservationRequested",
			"expected_type", "events.AgentObservationPayload",
		)
		return
	}

	if o.obsService == nil {
		o.logger.Error("Сервис наблюдений агента не инициализирован")
		return
	}

	traceID := payload.TraceID
	if strings.TrimSpace(traceID) == "" {
		traceID = uuid.New().String()
	}

	log := o.logger.With(
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
	obs, err := o.obsService.ApplyObservation(ctxWithTrace, payload.Source, &payload.Data)
	if err != nil {
		log.Error("Не удалось применить наблюдение агента", "error", err)
		return
	}

	o.publishAgentObservationUpdate(ctxWithTrace, payload.Source, &payload.Data, obs)

	log.Debug("Наблюдение агента успешно применено")
}

func (o *Orchestrator) publishAgentObservationUpdate(ctx context.Context, source string, data *api.AgentDataDTO, obs *models.AgentObservation) {
	if obs == nil || o.bus == nil {
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
	if o.workstationRepo != nil && obs.WorkstationID != nil && strings.TrimSpace(*obs.WorkstationID) != "" {
		if ws, err := o.workstationRepo.GetByID(ctx, strings.TrimSpace(*obs.WorkstationID)); err == nil && ws != nil {
			if ws.OwnerID != nil {
				wsOwner = strings.TrimSpace(*ws.OwnerID)
			}
			if ws.DeviceName != nil {
				workstationName = strings.TrimSpace(*ws.DeviceName)
			}
		}
	}
	if o.frRepo != nil && obs.FRID != nil && strings.TrimSpace(*obs.FRID) != "" {
		if fr, err := o.frRepo.GetByID(ctx, strings.TrimSpace(*obs.FRID)); err == nil && fr != nil {
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
	serverURL := ""
	if data != nil {
		currentRaw = strings.TrimSpace(data.CurrentTime)
		if raw, ok := data.AdditionalProperties["v_time"].(string); ok {
			vTimeRaw = strings.TrimSpace(raw)
		}
		serverURL = strings.TrimSpace(data.URLRms)
	}

	payload := events.AgentObservationUpdatedPayload{
		ObservationID:   obs.ID,
		AgentUUID:       stringPtrOrNil(agentUUID),
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
	o.bus.Publish(eventbus.Event{Type: events.AgentObservationUpdated, Payload: payload})
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

// handleServiceDeskEntityUpdate обрабатывает обновление сущности из внешней системы ServiceDesk.
//
// Алгоритм обработки:
//  1. Извлекает payload и определяет тип сущности (Company, Server, Workstation, FiscalRegister).
//  2. Проверяет, является ли сущность новой (по наличию связи в linkRepo).
//  3. Преобразует данные из map (Legacy/Webhook) или использует готовую модель (Adapter).
//  4. Делегирует анализ ProcessingEngine.ProcessServiceDeskUpdate().
//  5. Выполняет полученный план действий (ActionCreate или ActionUpdate).
//  6. Создаёт связь для новых сущностей.
//
// Все операции выполняются в транзакции.
func (o *Orchestrator) handleServiceDeskEntityUpdate(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие ServiceDeskEntityUpdated",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.ServiceDeskEntityPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityUpdated",
			"expected_type", "events.ServiceDeskEntityPayload",
		)
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)

	log.Debug("Начало обработки обновления сущности из ServiceDesk")

	var isNewEntity bool
	var internalID string

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, transactionKey, tx)
		mapperCtx := &external.MapperContext{DB: tx, LinkRepo: o.linkRepo, Logger: log}

		// Проверяем наличие связи — определяем, новая сущность или существующая
		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ServiceDeskUUID)
		if err != nil {
			return fmt.Errorf("ошибка поиска связи по внешнему ID: %w", err)
		}
		isNewEntity = link == nil

		log.Debug("Результат поиска связи",
			"is_new_entity", isNewEntity,
			"internal_id", func() string {
				if link != nil {
					return link.InternalID
				}
				return ""
			}(),
		)

		var newEntityModel, currentEntity interface{}

		// Проверяем формат данных: Map (Legacy/Webhook) или Model (Adapter)
		dataMap, isMap := payload.Data.(map[string]interface{})

		if isMap {
			// Данные пришли как map — используем Mapper для преобразования
			log.Debug("Данные получены в формате map, используется Mapper")

			switch payload.EntityType {
			case "Company":
				newEntityModel, err = o.sdClient.Mapper().DataToCompany(txCtx, mapperCtx, dataMap)
				if err == nil && !isNewEntity {
					currentEntity, _ = o.companyRepo.GetByIDUnscoped(txCtx, link.InternalID)
				}
			case "Server":
				newEntityModel, err = o.sdClient.Mapper().DataToServer(txCtx, mapperCtx, dataMap)
				if err == nil && !isNewEntity {
					currentEntity, _ = o.serverRepo.GetByIDUnscoped(txCtx, link.InternalID)
				}
			case "Workstation":
				newEntityModel, err = o.sdClient.Mapper().DataToWorkstation(txCtx, mapperCtx, dataMap)
				if err == nil && !isNewEntity {
					currentEntity, _ = o.workstationRepo.GetByIDUnscoped(txCtx, link.InternalID)
				}
			case "FiscalRegister":
				newEntityModel, err = o.sdClient.Mapper().DataToFiscalRegister(txCtx, mapperCtx, dataMap)
				if err == nil && !isNewEntity {
					currentEntity, _ = o.frRepo.GetByIDUnscoped(txCtx, link.InternalID)
				}
			default:
				return fmt.Errorf("неизвестный тип сущности для обработки (Map): %s", payload.EntityType)
			}
		} else {
			// Данные уже в формате модели от Адаптера
			log.Debug("Данные получены в формате модели от Адаптера")
			newEntityModel = payload.Data

			// Загружаем текущую сущность для сравнения
			if !isNewEntity {
				switch payload.EntityType {
				case "Company":
					currentEntity, _ = o.companyRepo.GetByIDUnscoped(txCtx, link.InternalID)
				case "Server":
					currentEntity, _ = o.serverRepo.GetByIDUnscoped(txCtx, link.InternalID)
				case "Workstation":
					currentEntity, _ = o.workstationRepo.GetByIDUnscoped(txCtx, link.InternalID)
				case "FiscalRegister":
					currentEntity, _ = o.frRepo.GetByIDUnscoped(txCtx, link.InternalID)
				}
			}
		}

		if err != nil {
			log.Warn("Пропуск обработки сущности из-за ошибки маппинга", "error", err)
			return nil
		}

		// Делегируем логику принятия решений движку
		log.Debug("Делегирование обработки движку",
			"is_new_entity", isNewEntity,
			"entity_type", payload.EntityType,
		)

		result, err := o.engine.ProcessServiceDeskUpdate(txCtx, isNewEntity, payload.EntityType, currentEntity, newEntityModel)
		if err != nil {
			return fmt.Errorf("ошибка в движке обработки: %w", err)
		}

		log.Debug("Движок вернул план действий",
			"actions_count", len(result.Actions),
		)

		// Исполняем план, полученный от движка
		for i, action := range result.Actions {
			log.Debug("Выполнение действия",
				"action_index", i,
				"action_type", action.Type,
			)

			switch action.Type {
			case ActionCreate:
				createdID, err := o.createEntity(txCtx, action.EntityToCreate)
				if err != nil {
					return err
				}
				internalID = createdID
				log.Debug("Сущность создана", "internal_id", createdID)

			case ActionUpdate:
				if err := o.performUpdate(txCtx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					return err
				}
				internalID = action.EntityUUID
				log.Debug("Сущность обновлена",
					"entity_uuid", action.EntityUUID,
					"fields_updated", len(action.Updates),
				)
			}
		}

		// Создаём связь для новой сущности
		if isNewEntity && internalID != "" {
			newLink := &models.ExternalSystemLink{
				InternalID: internalID, SystemName: "naumen", ServiceDeskUUID: payload.ServiceDeskUUID,
				EntityType: payload.EntityType, LastSyncedAt: time.Now(),
			}
			if err := o.linkRepo.Create(txCtx, tx, newLink); err != nil {
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
	} else {
		log.Debug("Обработка существующей сущности завершена")
	}
}

// handleServiceDeskEntityDelete обрабатывает удаление сущности из ServiceDesk.
//
// Выполняет "мягкое удаление" — помечает сущность как удалённую в БД,
// но сохраняет данные для возможного восстановления.
//
// Параметры:
//   - ctx — контекст запроса.
//   - event — событие с ServiceDeskEntityDeletePayload.
func (o *Orchestrator) handleServiceDeskEntityDelete(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие ServiceDeskEntityDeleted",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.ServiceDeskEntityDeletePayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityDeleted")
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)

	log.Debug("Начало обработки удаления сущности")

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, transactionKey, tx)

		// Ищем связь по внешнему ID
		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ServiceDeskUUID)
		if err != nil {
			return err
		}
		if link == nil {
			log.Warn("Связь для удаляемой сущности не найдена, возможно, она уже удалена")
			return nil
		}

		log.Debug("Найдена связь для удаления",
			"internal_id", link.InternalID,
		)

		// Выполняем мягкое удаление сущности
		if err := o.performDelete(txCtx, tx, payload.EntityType, link.InternalID); err != nil {
			return err
		}

		// Удаляем связь
		if err := tx.Delete(link).Error; err != nil {
			return fmt.Errorf("ошибка удаления связи: %w", err)
		}

		log.Debug("Связь удалена")
		return nil
	})

	if err != nil {
		log.Error("Ошибка при 'мягком удалении' сущности", "error", err)
	} else {
		log.Info("Сущность и ее связь успешно 'мягко удалены'")
	}
}

// handleContractsStatusRecalculated сохранён для быстрого возврата event-контура контрактов.
// В текущей конфигурации подписка на событие временно отключена в Start().
//
// Обновляет статусы контрактов у компаний и блокирует/разблокирует оборудование.
func (o *Orchestrator) handleContractsStatusRecalculated(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие ContractsStatusRecalculated",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.ContractsStatusPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ContractsStatusRecalculated")
		return
	}
	log := o.logger.With("event", event.Type)
	log.Info("Получено событие для обновления статусов контрактов у компаний", "count", len(payload.CompanyActiveContract))

	// Разделяем компании на активные и неактивные
	activeIDs := make([]string, 0)
	inactiveIDs := make([]string, 0)
	for id, isActive := range payload.CompanyActiveContract {
		if isActive {
			activeIDs = append(activeIDs, id)
		} else {
			inactiveIDs = append(inactiveIDs, id)
		}
	}

	log.Debug("Распределение компаний по статусам контрактов",
		"active_count", len(activeIDs),
		"inactive_count", len(inactiveIDs),
	)

	err := o.db.Transaction(func(tx *gorm.DB) error {
		source := "contract_gateway"

		// Обновляем статус active_contract для компаний
		if len(activeIDs) > 0 {
			if res := tx.Model(&company.Company{}).Where("id IN ?", activeIDs).Updates(map[string]interface{}{"active_contract": true, "last_updated_by": source}); res.Error != nil {
				return res.Error
			}
			log.Debug("Обновлены статусы контрактов", "active_count", len(activeIDs))
		}
		if len(inactiveIDs) > 0 {
			if res := tx.Model(&company.Company{}).Where("id IN ?", inactiveIDs).Updates(map[string]interface{}{"active_contract": false, "last_updated_by": source}); res.Error != nil {
				return res.Error
			}
			log.Debug("Обновлены статусы контрактов", "inactive_count", len(inactiveIDs))
		}

		// Блокируем оборудование неактивных компаний
		if len(inactiveIDs) > 0 {
			if err := o.lockEquipment(ctx, tx, inactiveIDs, log); err != nil {
				return err
			}
		}

		// Разблокируем оборудование активных компаний
		if len(activeIDs) > 0 {
			if err := o.unlockEquipment(ctx, tx, activeIDs, log); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов контрактов и оборудования", "error", err)
	} else {
		log.Info("Обновление статусов контрактов и оборудования успешно завершено")
	}
}

// handleDuplicatesFound обрабатывает обнаруженные дубликаты сущностей.
//
// Делегирует анализ ProcessingEngine.ProcessDuplicates() и выполняет
// полученный план действий (обычно ActionUpdate для пометки дубликатов).
func (o *Orchestrator) handleDuplicatesFound(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие DuplicatesFound",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.DuplicatesFoundPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события DuplicatesFound")
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "field", payload.Field, "value", payload.Value)

	log.Debug("Начало обработки дубликатов")

	// Делегируем анализ движку
	result := o.engine.ProcessDuplicates(ctx, payload)

	if len(result.Actions) == 0 {
		log.Debug("Движок не вернул действий для обработки дубликатов")
		return
	}

	log.Debug("Движок вернул план действий",
		"actions_count", len(result.Actions),
	)

	err := o.db.Transaction(func(tx *gorm.DB) error {
		for i, action := range result.Actions {
			if action.Type == ActionUpdate {
				log.Debug("Выполнение обновления дубликата",
					"action_index", i,
					"entity_uuid", action.EntityUUID,
				)

				if err := o.performUpdate(ctx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					log.Error("Не удалось обновить статус для сущности-дубликата",
						"internalID", action.EntityUUID,
						"error", err,
					)
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов для дубликатов", "error", err)
	} else {
		log.Info("Статусы для группы дубликатов успешно обновлены", "count", len(result.Actions))
	}
}

// handleAgentDataReceived обрабатывает полученные данные от агента мониторинга.
//
// Это основной обработчик для данных, поступающих от агентов через FTP или HTTP API.
// Делегирует анализ ProcessingEngine.ProcessAgentData() и выполняет план действий:
//   - ActionCreateTask — создание задачи согласования.
//   - ActionUpdate — обновление сущности.
//   - ActionAddAdditionalOwner — добавление дополнительного владельца сервера.
func (o *Orchestrator) handleAgentDataReceived(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие AgentDataReceived",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.AgentDataPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события AgentDataReceived",
			"expected_type", "events.AgentDataPayload",
		)
		return
	}

	traceID := payload.TraceID
	if strings.TrimSpace(traceID) == "" {
		traceID = uuid.New().String()
	}

	log := o.logger.With(
		"trace_id", traceID,
		"operation", "handle_agent_data",
		"source", payload.Source,
	)
	log.Debug("Оркестратор НАЧАЛ обработку события AgentDataReceived",
		"has_rms_url", payload.Data.URLRms != "",
		"has_hostname", payload.Data.Hostname != "",
		"has_serial", payload.Data.SerialNumber != "",
	)

	// Делегируем анализ данных движку
	ctxWithTrace := contextkeys.WithTraceID(ctx, traceID)
	result := o.engine.ProcessAgentData(ctxWithTrace, payload.Source, &payload.Data)

	if len(result.Actions) == 0 {
		log.Debug("Движок не вернул никаких действий для выполнения")
		return
	}

	log.Debug("Движок вернул план действий",
		"actions_count", len(result.Actions),
	)

	err := o.db.Transaction(func(tx *gorm.DB) error {
		for i, action := range result.Actions {
			log.Debug("Выполнение действия",
				"action_index", i,
				"action_type", action.Type,
			)

			switch action.Type {
			case ActionCreateTask:
				if err := tx.Create(action.Task).Error; err != nil {
					log.Error("Ошибка создания задачи",
						"task_type", action.Task.TaskType,
						"error", err,
					)
					return err
				}
				log.Debug("Задача создана",
					"task_id", action.Task.ID,
					"task_type", action.Task.TaskType,
				)

			case ActionUpdate:
				action.Updates["last_updated_by"] = "agent"
				if err := o.performUpdate(ctxWithTrace, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					log.Error("Ошибка обновления сущности",
						"entity_type", action.EntityType,
						"entity_uuid", action.EntityUUID,
						"error", err,
					)
					return err
				}
				log.Debug("Сущность обновлена",
					"entity_type", action.EntityType,
					"entity_uuid", action.EntityUUID,
					"fields_count", len(action.Updates),
				)

			case ActionAddAdditionalOwner:
				server := &server.Server{Base: common.Base{ID: action.EntityUUID}}
				company := &company.Company{Base: common.Base{ID: action.AdditionalOwnerUUID}}
				if err := tx.Model(server).Association("AdditionalOwners").Append(company); err != nil {
					log.Error("Не удалось добавить дополнительного владельца",
						"serverID", server.ID,
						"companyID", company.ID,
						"error", err,
					)
					return err
				}
				log.Debug("Добавлен дополнительный владелец",
					"server_id", server.ID,
					"company_id", company.ID,
				)
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при выполнении плана действий от движка", "error", err)
	} else {
		log.Info("План действий от движка успешно выполнен", "actions_count", len(result.Actions))
	}
}

// handleServerPollingSucceeded обрабатывает успешный результат опроса сервера.
//
// Обновляет данные сервера (имя, версия, редакция) и статус на основе
// ответа от RMS-сервера.
func (o *Orchestrator) handleServerPollingSucceeded(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие ServerPollingSucceeded",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.ServerPollingSucceededPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServerPollingSucceeded")
		return
	}
	log := o.logger.With("request_id", payload.RequestID)

	log.Debug("Обработка успешного опроса сервера",
		"server_uuid", payload.ServerUUID,
		"server_name", payload.ServerName,
		"new_status", payload.NewStatus,
	)

	updates := map[string]interface{}{
		"server_name":     payload.ServerName,
		"server_edition":  payload.ServerEdition,
		"server_version":  payload.ServerVersion,
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}

	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить данные сервера после успешного опроса", "error", err)
	} else {
		log.Info("Данные сервера успешно обновлены", "new_status", payload.NewStatus)
	}
}

// handleServerPollingFailed обрабатывает неудачный результат опроса сервера.
//
// Обновляет статус сервера на основе неудачного ответа (недоступен, ошибка и т.д.).
func (o *Orchestrator) handleServerPollingFailed(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие ServerPollingFailed",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.ServerPollingFailedPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServerPollingFailed")
		return
	}
	log := o.logger.With("request_id", payload.RequestID)

	log.Debug("Обработка неудачного опроса сервера",
		"server_uuid", payload.ServerUUID,
		"new_status", payload.NewStatus,
	)

	updates := map[string]interface{}{
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}

	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить статус сервера после неудачного опроса", "error", err)
	} else {
		log.Info("Статус сервера обновлен после неудачного опроса", "new_status", payload.NewStatus)
	}
}

// handleFiscalRegisterDiscrepancy обрабатывает событие о расхождении данных ФР.
//
// Создаёт задачу типа "need_update" для ручного разрешения расхождений между
// данными в эталонной БД и ServiceDesk.
//
// Проверяет наличие активной задачи для данного ФР, чтобы избежать дублирования.
func (o *Orchestrator) handleFiscalRegisterDiscrepancy(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Получено событие FiscalRegisterDiscrepancyFound",
		"event_type", event.Type,
	)

	payload, ok := event.Payload.(events.FiscalRegisterDiscrepancyPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события FiscalRegisterDiscrepancyFound")
		return
	}
	log := o.logger.With("fr_internal_uuid", payload.FRInternalUUID)

	log.Debug("Обработка расхождения данных ФР",
		"fr_service_desk_uuid", payload.FRServiceDeskUUID,
		"discrepancies_count", len(payload.Discrepancies),
	)

	// Проверяем наличие активной задачи для этого ФР
	existingTask, err := o.taskRepo.FindActiveTask(ctx, "need_update", payload.FRInternalUUID)
	if err != nil {
		log.Error("Ошибка при поиске существующей задачи 'need_update'", "error", err)
		return
	}
	if existingTask != nil {
		log.Debug("Активная задача 'need_update' для этого ФР уже существует, новая не создается",
			"existing_task_id", existingTask.ID,
		)
		return
	}

	// Формируем комментарий с описанием расхождений
	var commentBuilder strings.Builder
	commentBuilder.WriteString(fmt.Sprintf("Обнаружено расхождение данных для ФР (внутр. ID: %s, внешн. ID: %s) между эталонной БД и ServiceDesk. Требуется обновить данные в ServiceDesk.\n\nРасхождения:\n", payload.FRInternalUUID, payload.FRServiceDeskUUID))
	for field, details := range payload.Discrepancies {
		commentBuilder.WriteString(fmt.Sprintf("- Поле '%s':\n  - Эталон: %v\n  - ServiceDesk: %v\n", field, details.EtalonValue, details.ServiceDeskValue))
	}

	log.Debug("Сформирован комментарий задачи",
		"comment_length", commentBuilder.Len(),
	)

	// Сохраняем всю полезную нагрузку для контекста
	detailsJSON, _ := json.Marshal(payload)

	task := models.ReconciliationTask{
		TaskType:   "need_update",
		EntityType: "FiscalRegister",
		EntityUUID: payload.FRInternalUUID,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    commentBuilder.String(),
	}

	if err := o.db.WithContext(ctx).Create(&task).Error; err != nil {
		log.Error("Не удалось создать задачу 'need_update'", "error", err)
	} else {
		log.Info("Успешно создана задача 'need_update' на основе расхождений данных ФР",
			"task_id", task.ID,
		)
	}
}

// --- Вспомогательные функции-исполнители ---

// createEntity создаёт сущность в БД в рамках текущей транзакции.
//
// Поддерживаемые типы: Company, Server, Workstation, FiscalRegister.
// Извлекает транзакцию из контекста по ключу transactionKey.
//
// Параметры:
//   - ctx — контекст с транзакцией.
//   - entity — указатель на сущность для создания.
//
// Возвращает ID созданной сущности или ошибку.
func (o *Orchestrator) createEntity(ctx context.Context, entity interface{}) (string, error) {
	tx := ctx.Value(transactionKey).(*gorm.DB)
	var id string
	var err error

	switch v := entity.(type) {
	case *company.Company:
		err = o.companyRepo.Create(ctx, v)
		id = v.ID
	case *server.Server:
		err = o.serverRepo.Create(ctx, tx, v)
		id = v.ID
	case *workstation.Workstation:
		err = o.workstationRepo.Create(ctx, tx, v)
		id = v.ID
	case *fiscal.FiscalRegister:
		err = o.frRepo.Create(ctx, tx, v)
		id = v.ID
	default:
		return "", fmt.Errorf("неподдерживаемый тип для создания: %T", entity)
	}

	if err != nil {
		o.logger.Debug("Ошибка создания сущности",
			"entity_type", fmt.Sprintf("%T", entity),
			"error", err,
		)
	} else {
		o.logger.Debug("Сущность успешно создана",
			"entity_type", fmt.Sprintf("%T", entity),
			"entity_id", id,
		)
	}

	return id, err
}

// performUpdate обновляет сущность в БД.
//
// Маршрутизирует вызов к соответствующему репозиторию по типу сущности.
//
// Параметры:
//   - ctx — контекст запроса.
//   - tx — транзакция (может быть nil для autocommit).
//   - entityType — тип сущности (Company, Server, Workstation, FiscalRegister).
//   - internalID — внутренний ID сущности.
//   - updates — карта полей для обновления.
func (o *Orchestrator) performUpdate(ctx context.Context, tx *gorm.DB, entityType, internalID string, updates map[string]interface{}) error {
	var err error

	o.logger.Debug("Выполнение обновления сущности",
		"entity_type", entityType,
		"entity_id", internalID,
		"fields", updates,
	)

	switch entityType {
	case "Company":
		_, err = o.companyRepo.Update(ctx, internalID, updates)
	case "Server":
		_, err = o.serverRepo.Update(ctx, tx, internalID, updates)
	case "Workstation":
		_, err = o.workstationRepo.Update(ctx, tx, internalID, updates)
	case "FiscalRegister":
		_, err = o.frRepo.Update(ctx, tx, internalID, updates)
	default:
		return fmt.Errorf("неподдерживаемый тип для обновления: %s", entityType)
	}

	if err != nil {
		o.logger.Debug("Ошибка обновления сущности",
			"entity_type", entityType,
			"entity_id", internalID,
			"error", err,
		)
	}

	return err
}

// performDelete выполняет мягкое удаление сущности.
//
// Маршрутизирует вызов к соответствующему репозиторию по типу сущности.
//
// Параметры:
//   - ctx — контекст запроса.
//   - tx — транзакция.
//   - entityType — тип сущности.
//   - internalID — внутренний ID сущности.
func (o *Orchestrator) performDelete(ctx context.Context, tx *gorm.DB, entityType, internalID string) error {
	var err error

	o.logger.Debug("Выполнение мягкого удаления сущности",
		"entity_type", entityType,
		"entity_id", internalID,
	)

	switch entityType {
	case "Company":
		_, err = o.companyRepo.Delete(ctx, internalID)
	case "Server":
		_, err = o.serverRepo.Delete(ctx, tx, internalID)
	case "Workstation":
		_, err = o.workstationRepo.Delete(ctx, tx, internalID)
	case "FiscalRegister":
		_, err = o.frRepo.Delete(ctx, tx, internalID)
	default:
		return fmt.Errorf("неподдерживаемый тип для удаления: %s", entityType)
	}

	if err != nil {
		o.logger.Debug("Ошибка удаления сущности",
			"entity_type", entityType,
			"entity_id", internalID,
			"error", err,
		)
	}

	return err
}

// lockEquipment блокирует оборудование компаний с неактивными контрактами.
//
// Устанавливает статус "locked" и сохраняет предыдущий статус для восстановления.
// Обрабатывает серверы, рабочие станции и фискальные регистраторы.
//
// Параметры:
//   - ctx — контекст запроса.
//   - tx — транзакция.
//   - inactiveIDs — список ID компаний с неактивными контрактами.
//   - log — логгер с контекстом.
func (o *Orchestrator) lockEquipment(ctx context.Context, tx *gorm.DB, inactiveIDs []string, log logger.LoggerInterface) error {
	log.Debug("Блокировка оборудования компаний",
		"company_ids_count", len(inactiveIDs),
	)

	for _, model := range []interface{}{&server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status != ?", inactiveIDs, "locked").
			Updates(map[string]interface{}{"status_before_lock": gorm.Expr("status"), "status": "locked"})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Заморожено единиц оборудования",
				"model_type", fmt.Sprintf("%T", model),
				"count", res.RowsAffected,
			)
		}
	}
	return nil
}

// unlockEquipment разблокирует оборудование компаний с активными контрактами.
//
// Восстанавливает предыдущий статус из поля status_before_lock.
// Обрабатывает серверы, рабочие станции и фискальные регистраторы.
//
// Параметры:
//   - ctx — контекст запроса.
//   - tx — транзакция.
//   - activeIDs — список ID компаний с активными контрактами.
//   - log — логгер с контекстом.
func (o *Orchestrator) unlockEquipment(ctx context.Context, tx *gorm.DB, activeIDs []string, log logger.LoggerInterface) error {
	log.Debug("Разблокировка оборудования компаний",
		"company_ids_count", len(activeIDs),
	)

	for _, model := range []interface{}{&server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status = ? AND status_before_lock IS NOT NULL", activeIDs, "locked").
			Updates(map[string]interface{}{"status": gorm.Expr("status_before_lock"), "status_before_lock": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Разморожено единиц оборудования",
				"model_type", fmt.Sprintf("%T", model),
				"count", res.RowsAffected,
			)
		}
	}
	return nil
}

// getLMDFromModel извлекает LastModifiedDate из модели сущности.
//
// Используется для определения времени последнего изменения сущности
// при синхронизации с внешними системами.
//
// Параметры:
//   - entity — указатель на модель сущности.
//
// Возвращает указатель на time.Time или nil, если тип не поддерживается.
func getLMDFromModel(entity interface{}) *time.Time {
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
