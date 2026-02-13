// Файл: internal/core/processing/engine.go
//
// Package processing содержит движок обработки агентских данных.
// Движок отвечает за сверку данных от агентов мониторинга с существующими
// сущностями в системе (серверы, рабочие станции, фискальные регистраторы).
//
// Основные responsibilities:
//   - Валидация входящих данных от агентов
//   - Поиск и сопоставление сущностей через EntityMatcherService
//   - Обработка дубликатов и конфликтов владельцев
//   - Генерация плана действий (ProcessingResult) для Оркестратора
//
// Связь с другими компонентами:
//   - Orchestrator — вызывает ProcessAgentData для обработки данных
//   - ReconciliationEngine — делегирует сравнение моделей
//   - EntityMatcherService — выполняет поиск сущностей
//   - EventBus — публикует события о результатах обработки
package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"reflect"
	"time"

	"gorm.io/datatypes"
)

// ActionType определяет тип действия, которое должен выполнить Оркестратор.
// Используется для классификации действий в плане обработки.
type ActionType string

// Константы типов действий.
const (
	// ActionCreateTask — создание задачи на сверку (например, new_client, add_equipment).
	ActionCreateTask ActionType = "create_task"
	// ActionUpdate — обновление существующей сущности.
	ActionUpdate ActionType = "update"
	// ActionAddAdditionalOwner — добавление дополнительного владельца сущности.
	ActionAddAdditionalOwner ActionType = "add_additional_owner"
	// ActionCreate — создание новой сущности в системе.
	ActionCreate ActionType = "create"
)

// Action представляет одно действие в плане обработки.
// Оркестратор последовательно выполняет действия из плана.
//
// Поля:
//   - Type: тип действия (ActionType)
//   - EntityType: тип сущности (Server, Workstation, FiscalRegister)
//   - EntityUUID: внутренний UUID сущности для обновления
//   - Updates: карта полей для обновления
//   - Task: задача для создания (если Type == ActionCreateTask)
//   - AdditionalOwnerUUID: UUID дополнительного владельца
//   - EntityToCreate: модель сущности для создания (если Type == ActionCreate)
type Action struct {
	Type                ActionType
	EntityType          string
	EntityUUID          string                 // Внутренний ID сущности
	Updates             map[string]interface{} // Поля для обновления
	Task                *models.ReconciliationTask
	AdditionalOwnerUUID string // Внутренний ID доп. владельца
	EntityToCreate      interface{}
}

// ProcessingResult — это "план действий", который Процессор возвращает Оркестратору.
// Содержит список действий, которые нужно выполнить для обработки данных агента.
// Пустой список действий означает, что обработка не требуется.
type ProcessingResult struct {
	Actions []Action
}

// ProcessingEngine — интерфейс движка обработки данных.
// Определяет методы для обработки различных типов входящих данных:
//   - ProcessAgentData: обработка данных от агентов мониторинга
//   - ProcessDuplicates: обработка обнаруженных дубликатов
//   - ProcessServiceDeskUpdate: обработка обновлений из ServiceDesk
//   - CompareModelsForUpdate: сравнение моделей для определения изменений
type ProcessingEngine interface {
	// ProcessAgentData обрабатывает данные от агента и возвращает план действий.
	// Параметры:
	//   - ctx: контекст для отмены операции
	//   - source: источник данных (имя файла или UUID агента)
	//   - data: DTO с данными от агента
	// Возвращает ProcessingResult с планом действий для Оркестратора.
	ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult

	// ProcessDuplicates обрабатывает событие обнаружения дубликатов.
	// Создаёт задачи на разрешение дубликатов или обновляет статусы сущностей.
	ProcessDuplicates(ctx context.Context, payload events.DuplicatesFoundPayload) *ProcessingResult

	// ProcessServiceDeskUpdate обрабатывает обновление сущности из ServiceDesk.
	// Параметры:
	//   - isNew: true если сущность новая (создание), false если обновление
	//   - entityType: тип сущности (Server, Workstation и т.д.)
	//   - currentEntity: текущая модель сущности (nil если isNew)
	//   - newEntityModel: новая модель сущности
	ProcessServiceDeskUpdate(ctx context.Context, isNew bool, entityType string, currentEntity, newEntityModel interface{}) (*ProcessingResult, error)

	// CompareModelsForUpdate сравнивает две модели и возвращает карту изменений.
	CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error)
}

// processingEngineImpl — реализация ProcessingEngine.
// Содержит репозитории для доступа к данным и сервисы для бизнес-логики.
type processingEngineImpl struct {
	logger               logger.LoggerInterface
	taskRepo             repositories.TaskRepo
	companyRepo          company.Repository
	serverRepo           server.Repository
	workstationRepo      workstation.Repository
	frRepo               fiscal.Repository
	linkRepo             repositories.LinkRepo
	reconciliationEngine ReconciliationEngine
	matcherSvc           services.EntityMatcherService
}

// NewProcessingEngine создаёт новый экземпляр движка обработки.
// Все параметры обязательны и не должны быть nil.
//
// Параметры:
//   - logger: логгер для записи отладочной информации
//   - taskRepo: репозиторий задач сверки
//   - companyRepo: репозиторий компаний
//   - serverRepo: репозиторий серверов
//   - workstationRepo: репозиторий рабочих станций
//   - frRepo: репозиторий фискальных регистраторов
//   - linkRepo: репозиторий связей
//   - reconciliationEngine: движок сверки для сравнения моделей
//   - matcherSvc: сервис поиска и сопоставления сущностей
func NewProcessingEngine(
	logger logger.LoggerInterface,
	taskRepo repositories.TaskRepo,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
	linkRepo repositories.LinkRepo,
	reconciliationEngine ReconciliationEngine,
	matcherSvc services.EntityMatcherService,
) ProcessingEngine {
	logger.Debug("Создание ProcessingEngine", "операция", "NewProcessingEngine")
	return &processingEngineImpl{
		logger, taskRepo, companyRepo, serverRepo, workstationRepo, frRepo, linkRepo, reconciliationEngine, matcherSvc,
	}
}

// ProcessServiceDeskUpdate обрабатывает событие обновления из ServiceDesk и возвращает план действий.
// Метод вызывается при получении обновлений сущностей из внешней системы ServiceDesk.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - isNew: true если сущность новая (создание), false если обновление существующей
//   - entityType: тип сущности (Server, Workstation, FiscalRegister)
//   - currentEntity: текущая модель сущности (nil если isNew == true)
//   - newEntityModel: новая модель сущности из ServiceDesk
//
// Возвращает:
//   - *ProcessingResult: план действий для Оркестратора
//   - error: ошибка при сравнении моделей
//
// Логика:
//   - Для новых сущностей: создаёт действие ActionCreate
//   - Для существующих: сравнивает модели через ReconciliationEngine
//   - Добавляет last_modified_date и last_updated_by в обновления
func (p *processingEngineImpl) ProcessServiceDeskUpdate(ctx context.Context, isNew bool, entityType string, currentEntity, newEntityModel interface{}) (*ProcessingResult, error) {
	log := p.logger.With("операция", "ProcessServiceDeskUpdate", "entityType", entityType, "isNew", isNew)
	log.Debug("Начало обработки обновления из ServiceDesk")

	result := &ProcessingResult{Actions: []Action{}}

	if isNew {
		log.Debug("Сущность новая, создание действия ActionCreate")
		// Если сущность новая, план прост: создать ее.
		action := Action{
			Type:           ActionCreate,
			EntityType:     entityType,
			EntityToCreate: newEntityModel,
		}
		result.Actions = append(result.Actions, action)
		log.Info("Создано действие на создание новой сущности", "entityType", entityType)
		return result, nil
	}

	log.Debug("Сущность существует, сравнение моделей")
	// Если сущность существует, сравниваем ее с новой версией.
	updates, err := p.reconciliationEngine.CompareModelsForUpdate(entityType, currentEntity, newEntityModel)
	if err != nil {
		log.Error("Ошибка при сравнении моделей", "error", err)
		return nil, err
	}

	// Добавляем дату модификации в список обновлений.
	if newLMD := getLMDFromModel(newEntityModel); newLMD != nil {
		if updates == nil {
			updates = make(map[string]interface{})
		}
		updates["last_modified_date"] = newLMD
		log.Debug("Добавлена last_modified_date в обновления", "last_modified_date", newLMD)
	}

	if len(updates) > 0 {
		updates["last_updated_by"] = "naumen_gateway"
		var internalID string
		// Получаем ID из существующей сущности
		if val := reflect.ValueOf(currentEntity).Elem().FieldByName("ID"); val.IsValid() {
			internalID = val.String()
		}

		action := Action{
			Type:       ActionUpdate,
			EntityType: entityType,
			EntityUUID: internalID,
			Updates:    updates,
		}
		result.Actions = append(result.Actions, action)
		log.Info("Создано действие на обновление сущности",
			"entityUUID", internalID,
			"fields_count", len(updates),
		)
	} else {
		log.Debug("Изменений не обнаружено, действия не требуются")
	}

	return result, nil
}

// CompareModelsForUpdate делегирует вызов нижележащему ReconciliationEngine.
// Метод является обёрткой для сохранения единого интерфейса ProcessingEngine.
//
// Параметры:
//   - entityType: тип сущности (Server, Workstation, FiscalRegister)
//   - current: текущая модель сущности
//   - new: новая модель сущности
//
// Возвращает:
//   - map[string]interface{}: карта полей для обновления (nil если изменений нет)
//   - error: ошибка при сравнении
func (p *processingEngineImpl) CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error) {
	p.logger.Debug("Сравнение моделей для обновления",
		"операция", "CompareModelsForUpdate",
		"entityType", entityType,
	)
	return p.reconciliationEngine.CompareModelsForUpdate(entityType, current, new)
}

// ProcessDuplicates обрабатывает событие обнаружения дубликатов и возвращает план действий.
// Метод вызывается при обнаружении сущностей с одинаковыми значениями уникальных полей.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - payload: данные о найденных дубликатах (тип сущности, поле, значение, список ID)
//
// Возвращает:
//   - *ProcessingResult: план действий для обновления статусов дубликатов
//
// Логика:
//   - Обогащает данные каждого дубликата через ReconciliationEngine
//   - Если после обогащения осталось < 2 сущностей — задача не создаётся
//   - Устанавливает health_status = "attention_required" для всех дубликатов
//   - Записывает детали в status_details (JSON с информацией о дубликатах)
func (p *processingEngineImpl) ProcessDuplicates(ctx context.Context, payload events.DuplicatesFoundPayload) *ProcessingResult {
	log := p.logger.With(
		"операция", "ProcessDuplicates",
		"entityType", payload.EntityType,
		"field", payload.Field,
		"value", payload.Value,
		"duplicates_count", len(payload.InternalIDs),
	)
	log.Debug("Начало обработки дубликатов")

	result := &ProcessingResult{Actions: []Action{}}

	// Обогащаем данные каждого дубликата для включения в status_details
	log.Debug("Обогащение данных дубликатов")
	enrichedDuplicates := make([]map[string]interface{}, 0, len(payload.InternalIDs))
	for i, internalID := range payload.InternalIDs {
		data, err := p.reconciliationEngine.GetEnrichmentDataForEntity(ctx, payload.EntityType, internalID)
		if err != nil {
			log.Warn("Не удалось обогатить данные для одного из дубликатов",
				"internalID", internalID,
				"index", i,
				"error", err,
			)
			continue
		}
		enrichedDuplicates = append(enrichedDuplicates, data)
		log.Debug("Дубликат обогащён", "internalID", internalID, "index", i)
	}

	if len(enrichedDuplicates) < 2 {
		log.Info("После обогащения осталось меньше двух сущностей, задача на дубликаты не создаётся",
			"enriched_count", len(enrichedDuplicates),
		)
		return result
	}

	// Формируем status_details с информацией о дубликатах
	statusDetails := map[string]interface{}{
		"type":       "duplicate_found",
		"field":      payload.Field,
		"value":      payload.Value,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"source":     "duplicates_gateway",
		"duplicates": enrichedDuplicates,
	}

	detailsJSON, err := json.Marshal(statusDetails)
	if err != nil {
		log.Error("Ошибка сериализации status_details для дубликатов", "error", err)
		return result
	}

	// Создаём действия на обновление каждой сущности-дубликата
	updates := map[string]interface{}{
		"health_status":  "attention_required",
		"status_details": datatypes.JSON(detailsJSON),
	}

	log.Debug("Создание действий на обновление статусов дубликатов")
	for i, internalID := range payload.InternalIDs {
		action := Action{
			Type:       ActionUpdate,
			EntityType: payload.EntityType,
			EntityUUID: internalID,
			Updates:    updates,
		}
		result.Actions = append(result.Actions, action)
		log.Debug("Действие на обновление создано", "internalID", internalID, "index", i)
	}

	log.Info("Обработка дубликатов завершена",
		"actions_count", len(result.Actions),
		"health_status", "attention_required",
	)

	return result
}

// ProcessAgentData — главный метод обработки данных от агента мониторинга.
// Реализует согласованную логику сверки данных с существующими сущностями.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - source: источник данных (имя файла или UUID агента)
//   - data: DTO с данными от агента (AgentDataDTO)
//
// Возвращает:
//   - *ProcessingResult: план действий для Оркестратора
//
// Алгоритм обработки (см. docs/AGENT_DATA_FLOW.md):
//  1. Валидация времени агента (пропуск данных старше 60 дней)
//  2. Поиск сущностей через EntityMatcherService (водопадный алгоритм)
//  3. Обработка дубликатов (создание задачи resolve_duplicate)
//  4. Обработка конфликта владельцев (проверка родства компаний)
//  5. Обработка новых/существующих сущностей (Trusted Updates)
//
// Преобразование AgentDataDTO → AgentObservation:
//   - source → Source
//   - data.CurrentTime → ObservedAt
//   - data.URLRms → ServerKey (через UUID)
//   - data.CRMID → ServerCRMID
//   - весь payload → PayloadJSON
func (p *processingEngineImpl) ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult {
	log := p.logger.With("операция", "ProcessAgentData", "source", source)
	log.Debug("Начало обработки данных агента",
		"hostname", data.Hostname,
		"crm_id", data.CRMID,
		"serial_number", data.SerialNumber,
	)

	result := &ProcessingResult{Actions: []Action{}}

	// 1. Валидация времени агента
	log.Debug("Валидация времени агента", "current_time", data.CurrentTime)
	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime == nil {
		log.Warn("Не удалось распознать 'current_time' из данных агента. Обработка прервана.")
		return result
	}
	if currentTime.Before(time.Now().AddDate(0, 0, -60)) {
		log.Info("Данные от агента пропущены, так как они старше 60 дней.",
			"current_time", *currentTime,
			"threshold_days", 60,
		)
		return result
	}
	log.Debug("Время агента валидно", "current_time", *currentTime)

	// 2. Получение отчета о поиске сущностей (Водопадный алгоритм)
	log.Debug("Поиск сущностей через MatcherService")
	report, err := p.matcherSvc.GetMatchReport(ctx, data)
	if err != nil {
		log.Error("Ошибка при выполнении поиска сущностей (Matcher)", "error", err)
		return result
	}

	// Логируем результаты поиска
	log.Debug("Результаты поиска сущностей",
		"found_server", report.FoundServer != nil,
		"found_workstation", report.FoundWorkstation != nil,
		"found_fr", report.FoundFR != nil,
		"duplicates_count", len(report.Duplicates),
		"conflict", report.Conflict,
		"primary_owner_id", report.PrimaryOwnerID,
	)

	// 3. Обработка Дубликатов
	if len(report.Duplicates) > 0 {
		log.Warn("Обнаружены дубликаты сущностей. Создание задачи resolve_duplicate.",
			"count", len(report.Duplicates),
		)

		// Извлекаем UUID дубликатов для создания задачи
		var duplicateUUIDs []string
		for _, dup := range report.Duplicates {
			if ws, ok := dup.(workstation.Workstation); ok {
				duplicateUUIDs = append(duplicateUUIDs, ws.ID)
				log.Debug("Дубликат рабочей станции", "ws_id", ws.ID)
			}
			// TODO:
			// Добавить другие типы, если matcher начнет возвращать дубли по ним
		}

		// Проверяем, есть ли уже задача на эти дубликаты
		existingTask, _ := p.taskRepo.FindActiveDuplicateTaskByMemberUUIDs(ctx, duplicateUUIDs)
		if existingTask == nil {
			log.Debug("Активной задачи нет, создание новой задачи resolve_duplicate")
			// Создаем задачу вручную, так как CreateConflictTask заточен под одну сущность
			detailsMap := map[string]interface{}{
				"duplicates": report.Duplicates,
				"agent_data": data,
				"reason":     "multiple_entities_match_agent_id",
			}
			detailsJSON, _ := json.Marshal(detailsMap)

			task := &models.ReconciliationTask{
				TaskType:   "resolve_duplicate",
				Status:     "new",
				Details:    datatypes.JSON(detailsJSON),
				Comment:    "Обнаружено несколько сущностей с одинаковыми ID удаленного доступа.",
				EntityType: "Workstation", // Пока только для WS актуально
				// EntityUUID можно оставить пустым или записать ID первой сущности
			}
			result.Actions = append(result.Actions, Action{Type: ActionCreateTask, Task: task})
			log.Info("Задача resolve_duplicate создана", "task_type", "resolve_duplicate", "duplicates_count", len(duplicateUUIDs))
		} else {
			log.Debug("Активная задача на дубликаты уже существует.", "task_id", existingTask.ID)
		}

		return result // Прерываем обработку, нельзя обновлять при дублях
	}

	// 4. Обработка Конфликта Владельцев (Owner Mismatch)
	if report.Conflict {
		srvOwner := utils.SafeStringDereference(report.FoundServer.OwnerID)
		wsOwner := utils.SafeStringDereference(report.FoundWorkstation.OwnerID)

		log.Info("Проверка родства компаний при конфликте владельцев",
			"server_owner", srvOwner,
			"ws_owner", wsOwner,
		)

		areRelated := p.reconciliationEngine.AreCompaniesRelated(srvOwner, wsOwner)
		log.Debug("Результат проверки родства компаний", "are_related", areRelated)

		if !areRelated {
			log.Warn("Владельцы не связаны. Контур owner_mismatch отключен, обработка продолжается в автоматическом режиме.")
		} else {
			log.Info("Компании связаны (холдинг/филиал). Конфликт игнорируется.")
		}
	}

	// 5. Обработка "Нового" или "Существующего" (Trusted Updates)

	// Если ничего не найдено -> New Client / Add Equipment
	if report.FoundServer == nil && report.FoundWorkstation == nil && report.FoundFR == nil {
		log.Info("Сущности не найдены. Создание задачи new_client.")
		action := p.reconciliationEngine.CreateConflictTask(ctx, "new_client", "", data)
		if action != nil {
			result.Actions = append(result.Actions, *action)
			log.Debug("Задача new_client создана")
		}
		return result
	}

	// Если что-то найдено, применяем логику обновлений
	ownerID := report.PrimaryOwnerID
	if ownerID == "" {
		log.Warn("Сущности найдены, но владелец не определен. Создание задачи data_conflict.")
		action := p.reconciliationEngine.CreateConflictTask(ctx, "data_conflict", "", data)
		if action != nil {
			result.Actions = append(result.Actions, *action)
			log.Debug("Задача data_conflict создана")
		}
		// Продолжаем попытку обновления найденных сущностей, даже если владелец не ясен?
		// Нет, лучше остановиться, это риск.
		return result
	}

	// Проверка контракта владельца
	log.Debug("Проверка контракта владельца", "owner_id", ownerID)
	ownerCompany, err := p.companyRepo.GetByID(ctx, ownerID)
	if err == nil && ownerCompany != nil {
		if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
			log.Debug("Обработка данных от агента пропущена: неактивный контракт у владельца", "ownerID", ownerID)
			return result
		}
		log.Debug("Контракт владельца активен")
	}

	// A. Обработка Сервера (Привязка владельца, если отсутствует)
	if report.FoundServer != nil && report.FoundServer.HealthStatus != "locked" {
		log.Debug("Обработка сервера", "server_id", report.FoundServer.ID)
		// Если у сервера нет владельца, привязываем найденного
		if report.FoundServer.OwnerID == nil || *report.FoundServer.OwnerID == "" {
			log.Debug("У сервера нет владельца, привязка найденного владельца",
				"server_id", report.FoundServer.ID,
				"owner_id", ownerID,
			)
			updates := map[string]interface{}{
				"owner_id":        ownerID,
				"last_updated_by": "agent_matcher",
			}
			result.Actions = append(result.Actions, Action{
				Type:       ActionUpdate,
				EntityType: EntityTypeServer,
				EntityUUID: report.FoundServer.ID,
				Updates:    updates,
			})
		}
		// Остальные поля сервера обновляются через CompareEntityData (но там read-only правила)
		_, action := p.reconciliationEngine.CompareEntityData(ctx, EntityTypeServer, map[string]interface{}{"crm_id": data.CRMID}, report.FoundServer)
		if action != nil {
			result.Actions = append(result.Actions, *action)
			log.Debug("Создано действие на обновление сервера", "server_id", report.FoundServer.ID)
		}
	} else if report.FoundServer != nil {
		log.Debug("Сервер заблокирован, обновление пропущено", "server_id", report.FoundServer.ID, "health_status", report.FoundServer.HealthStatus)
	}

	// B. Обработка Рабочей станции
	agentWSData := map[string]interface{}{
		"teamviewer":  utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID)),
		"litemanager": utils.SafeStringDereference(validators.ExtractLiteManagerID(data.AdditionalProperties, data.LitemanagerID)),
		"hostname":    data.Hostname,
	}
	log.Debug("Данные РС от агента",
		"teamviewer", agentWSData["teamviewer"],
		"litemanager", agentWSData["litemanager"],
		"hostname", agentWSData["hostname"],
	)

	if report.FoundWorkstation != nil {
		log.Debug("Обработка рабочей станции", "ws_id", report.FoundWorkstation.ID)
		if report.FoundWorkstation.HealthStatus != "locked" {
			_, action := p.reconciliationEngine.CompareEntityData(ctx, EntityTypeWorkstation, agentWSData, report.FoundWorkstation)
			if action != nil {
				result.Actions = append(result.Actions, *action)
				log.Debug("Создано действие на обновление РС", "ws_id", report.FoundWorkstation.ID)
			}
		} else {
			log.Debug("РС заблокирована, обновление пропущено", "ws_id", report.FoundWorkstation.ID, "health_status", report.FoundWorkstation.HealthStatus)
		}
	} else {
		// РС не найдена, но есть данные для неё -> add_equipment
		// Проверяем, есть ли валидные данные для создания
		if agentWSData["teamviewer"] != "" || agentWSData["litemanager"] != "" {
			log.Info("Рабочая станция не найдена, создание задачи add_equipment",
				"owner_id", ownerID,
				"teamviewer", agentWSData["teamviewer"],
				"litemanager", agentWSData["litemanager"],
			)
			action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
			if action != nil {
				result.Actions = append(result.Actions, *action)
				log.Debug("Задача add_equipment создана для РС")
			}
		}
	}

	// C. Обработка Фискального регистратора
	agentFRData := map[string]interface{}{
		"RNM":              data.RNM,
		"organizationName": data.OrganizationName,
		"INN":              data.INN,
		"modelName":        data.ModelName,
		"licenses":         data.Licenses,
		"fr_downloader":    data.BootVersion,
		"kkt_reg_date":     data.DateTimeReg,
		"dateTime_end":     data.DateTimeEnd,
		"driver_version":   data.InstalledDriver,
		"fn_number":        data.FNSerial,
		"address":          data.Address,
		"attribute_excise": data.AttributeExcise,
		"attribute_marked": data.AttributeMarked,
		"ofd_name":         data.OFDName,
	}
	log.Debug("Данные ФР от агента",
		"serial_number", data.SerialNumber,
		"RNM", data.RNM,
		"INN", data.INN,
	)

	if report.FoundFR != nil {
		log.Debug("Обработка фискального регистратора", "fr_id", report.FoundFR.ID)
		if report.FoundFR.HealthStatus != "locked" {
			_, action := p.reconciliationEngine.CompareEntityData(ctx, EntityTypeFiscalRegister, agentFRData, report.FoundFR)
			if action != nil {
				result.Actions = append(result.Actions, *action)
				log.Debug("Создано действие на обновление ФР", "fr_id", report.FoundFR.ID)
			}
		} else {
			log.Debug("ФР заблокирован, обновление пропущено", "fr_id", report.FoundFR.ID, "health_status", report.FoundFR.HealthStatus)
		}
	} else if data.SerialNumber != "" {
		log.Info("ФР не найден, создание задачи add_equipment",
			"owner_id", ownerID,
			"serial_number", data.SerialNumber,
		)
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			result.Actions = append(result.Actions, *action)
			log.Debug("Задача add_equipment создана для ФР")
		}
	}

	log.Info("Обработка данных агента завершена",
		"actions_count", len(result.Actions),
		"source", source,
	)

	return result
}

// processServerActions обрабатывает действия для сервера.
// В текущей реализации метод используется для анализа конфликта владельцев.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - res: результат обработки для добавления действий
//   - equipmentOwnerID: ID владельца оборудования
//   - server: найденный сервер (может быть nil)
//   - data: данные от агента
//
// Логика:
//   - Пропускает заблокированные серверы (health_status = "locked")
//   - Проверяет родство компаний владельца сервера и оборудования
//   - Логирует результат проверки для диагностики
func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, equipmentOwnerID string, server *server.Server, data *api.AgentDataDTO) {
	log := p.logger.With("операция", "processServerActions")

	serverID := "nil"
	if server != nil {
		serverID = server.ID
	}
	log.Debug("Начало обработки сервера", "equipmentOwnerID", equipmentOwnerID, "server_id", serverID, "crm_id", data.CRMID)

	if server == nil {
		log.Debug("Сервер не найден, выход из processServerActions")
		return
	}

	if server.HealthStatus == "locked" {
		log.Debug("Обработка сервера пропущена: статус 'locked'", "id", server.ID)
		return
	}

	serverPrimaryOwnerID := *server.OwnerID
	log.Debug("Анализ конфликта владельцев сервера",
		"server_id", server.ID,
		"server_owner", serverPrimaryOwnerID,
		"equipment_owner", equipmentOwnerID,
	)

	areRelated := p.reconciliationEngine.AreCompaniesRelated(equipmentOwnerID, serverPrimaryOwnerID)
	log.Debug("Результат проверки родства компаний",
		"server_owner", serverPrimaryOwnerID,
		"equipment_owner", equipmentOwnerID,
		"are_related", areRelated,
	)

	if !areRelated {
		log.Debug("Компании не связаны, owner_mismatch отключен",
			"server_owner", serverPrimaryOwnerID,
			"equipment_owner", equipmentOwnerID,
		)
	}
}

// processWorkstationActions обрабатывает действия для рабочей станции.
// Метод сравнивает данные агента с существующей РС и создаёт действия на обновление.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - res: результат обработки для добавления действий
//   - ownerID: ID владельца для создания задачи add_equipment
//   - ws: найденная рабочая станция (может быть nil)
//   - data: данные от агента
//
// Логика:
//   - Если РС найдена и не заблокирована: сравнивает данные через ReconciliationEngine
//   - Если РС не найдена, но есть remote IDs: создаёт задачу add_equipment
//
// Преобразование полей:
//   - data.TeamviewerID → teamviewer (с валидацией)
//   - data.LitemanagerID → litemanager (с извлечением из additional_properties)
//   - data.AnydeskID → anydesk (с валидацией)
func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, ownerID string, ws *workstation.Workstation, data *api.AgentDataDTO) {
	log := p.logger.With("операция", "processWorkstationActions")

	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))
	agentAD := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.AnydeskID))

	log.Debug("Данные РС от агента",
		"teamviewer", agentTV,
		"litemanager", agentLM,
		"anydesk", agentAD,
	)

	if ws != nil {
		log.Debug("Обработка существующей РС", "ws_id", ws.ID, "health_status", ws.HealthStatus)
		if ws.HealthStatus == "locked" {
			log.Debug("РС заблокирована, обновление пропущено", "ws_id", ws.ID)
			return
		}
		agentData := map[string]interface{}{
			"teamviewer":  agentTV,
			"litemanager": agentLM,
			"anydesk":     agentAD,
		}
		if hasChanges, updateAction := p.reconciliationEngine.CompareEntityData(ctx, "Workstation", agentData, ws); hasChanges {
			res.Actions = append(res.Actions, *updateAction)
			log.Debug("Создано действие на обновление РС", "ws_id", ws.ID)
		} else {
			log.Debug("Изменений в РС не обнаружено", "ws_id", ws.ID)
		}
	} else if agentTV != "" || agentLM != "" || agentAD != "" {
		log.Info("РС не найдена, создание задачи add_equipment",
			"owner_id", ownerID,
			"teamviewer", agentTV,
			"litemanager", agentLM,
			"anydesk", agentAD,
		)
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			res.Actions = append(res.Actions, *action)
			log.Debug("Задача add_equipment создана", "task_type", action.Task.TaskType)
		} else {
			log.Debug("Задача add_equipment уже существует, пропускаем")
		}
	}
}

// processFiscalRegisterActions обрабатывает действия для фискального регистратора.
// Метод сравнивает данные агента с существующим ФР и создаёт действия на обновление.
//
// Параметры:
//   - ctx: контекст для отмены операции
//   - res: результат обработки для добавления действий
//   - ownerID: ID владельца для создания задачи add_equipment
//   - fr: найденный фискальный регистратор (может быть nil)
//   - data: данные от агента
//
// Логика:
//   - Если ФР найден и не заблокирован: сравнивает данные через ReconciliationEngine
//   - Если ФР не найден, но есть serial_number: создаёт задачу add_equipment
//
// Преобразование полей (Full Trust для ФР):
//   - data.DateTimeEnd → dateTime_end
//   - data.RNM → RNM
//   - data.OrganizationName → organizationName
//   - data.INN → INN
//   - data.ModelName → modelName
//   - data.Licenses → licenses
//   - data.BootVersion → fr_downloader
//   - data.DateTimeReg → kkt_reg_date
//   - data.InstalledDriver → driver_version
//   - data.FNSerial → fn_number
//   - data.Address → address
//   - data.AttributeExcise → attribute_excise
//   - data.AttributeMarked → attribute_marked
//   - data.OFDName → ofd_name
func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, ownerID string, fr *fiscal.FiscalRegister, data *api.AgentDataDTO) {
	log := p.logger.With("операция", "processFiscalRegisterActions")

	log.Debug("Данные ФР от агента",
		"serial_number", data.SerialNumber,
		"RNM", data.RNM,
		"INN", data.INN,
	)

	if fr != nil {
		log.Debug("Обработка существующего ФР", "fr_id", fr.ID, "health_status", fr.HealthStatus)
		if fr.HealthStatus == "locked" {
			log.Debug("ФР заблокирован, обновление пропущено", "fr_id", fr.ID)
			return
		}
		agentData := map[string]interface{}{
			"dateTime_end":     data.DateTimeEnd,
			"RNM":              data.RNM,
			"organizationName": data.OrganizationName,
			"INN":              data.INN,
			"modelName":        data.ModelName,
			"licenses":         data.Licenses,
			"fr_downloader":    data.BootVersion,
			"kkt_reg_date":     data.DateTimeReg,
			"driver_version":   data.InstalledDriver,
			"fn_number":        data.FNSerial,
			"address":          data.Address,
			"attribute_excise": data.AttributeExcise,
			"attribute_marked": data.AttributeMarked,
			"ofd_name":         data.OFDName,
		}
		if hasChanges, updateAction := p.reconciliationEngine.CompareEntityData(ctx, "FiscalRegister", agentData, fr); hasChanges {
			res.Actions = append(res.Actions, *updateAction)
			log.Debug("Создано действие на обновление ФР", "fr_id", fr.ID)
		} else {
			log.Debug("Изменений в ФР не обнаружено", "fr_id", fr.ID)
		}
	} else if data.SerialNumber != "" {
		log.Info("ФР не найден, создание задачи add_equipment",
			"owner_id", ownerID,
			"serial_number", data.SerialNumber,
		)
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			res.Actions = append(res.Actions, *action)
			log.Debug("Задача add_equipment создана для ФР")
		}
	}
}
