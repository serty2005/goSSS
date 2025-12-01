// Файл: internal/core/processing/engine.go
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
type ActionType string

const (
	ActionCreateTask         ActionType = "create_task"
	ActionUpdate             ActionType = "update"
	ActionAddAdditionalOwner ActionType = "add_additional_owner"
	ActionCreate             ActionType = "create" // Новое действие для создания сущностей
)

// Action представляет одно действие в плане.
type Action struct {
	Type                ActionType
	EntityType          string
	EntityUUID          string // Внутренний ID сущности
	Updates             map[string]interface{}
	Task                *models.ReconciliationTask
	AdditionalOwnerUUID string // Внутренний ID доп. владельца
	EntityToCreate      interface{}
}

// ProcessingResult - это "план действий", который Процессор возвращает Оркестратору.
type ProcessingResult struct {
	Actions []Action
}

// ProcessingEngine содержит всю сложную бизнес-логику сверки.
type ProcessingEngine interface {
	ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult
	ProcessDuplicates(ctx context.Context, payload events.DuplicatesFoundPayload) *ProcessingResult
	ProcessServiceDeskUpdate(ctx context.Context, isNew bool, entityType string, currentEntity, newEntityModel interface{}) (*ProcessingResult, error)
	CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error)
}

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

// NewProcessingEngine создает новый экземпляр движка.
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
	return &processingEngineImpl{
		logger, taskRepo, companyRepo, serverRepo, workstationRepo, frRepo, linkRepo, reconciliationEngine, matcherSvc,
	}
}

// ProcessServiceDeskUpdate обрабатывает событие обновления из SD и возвращает план.
func (p *processingEngineImpl) ProcessServiceDeskUpdate(ctx context.Context, isNew bool, entityType string, currentEntity, newEntityModel interface{}) (*ProcessingResult, error) {
	result := &ProcessingResult{Actions: []Action{}}

	if isNew {
		// Если сущность новая, план прост: создать ее.
		action := Action{
			Type:           ActionCreate,
			EntityType:     entityType,
			EntityToCreate: newEntityModel,
		}
		result.Actions = append(result.Actions, action)
		return result, nil
	}

	// Если сущность существует, сравниваем ее с новой версией.
	updates, err := p.reconciliationEngine.CompareModelsForUpdate(entityType, currentEntity, newEntityModel)
	if err != nil {
		return nil, err
	}

	// Добавляем дату модификации в список обновлений.
	if newLMD := getLMDFromModel(newEntityModel); newLMD != nil {
		if updates == nil {
			updates = make(map[string]interface{})
		}
		updates["last_modified_date"] = newLMD
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
	}

	return result, nil
}

// CompareModelsForUpdate просто делегирует вызов нижележащему ReconciliationEngine.
func (p *processingEngineImpl) CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error) {
	return p.reconciliationEngine.CompareModelsForUpdate(entityType, current, new)
}

// ProcessDuplicates обрабатывает событие нахождения дубликатов и возвращает план.
func (p *processingEngineImpl) ProcessDuplicates(ctx context.Context, payload events.DuplicatesFoundPayload) *ProcessingResult {
	result := &ProcessingResult{Actions: []Action{}}
	log := p.logger.With("entityType", payload.EntityType, "field", payload.Field, "value", payload.Value)

	enrichedDuplicates := make([]map[string]interface{}, 0, len(payload.InternalIDs))
	for _, internalID := range payload.InternalIDs {
		data, err := p.reconciliationEngine.GetEnrichmentDataForEntity(ctx, payload.EntityType, internalID)
		if err != nil {
			log.Warn("Не удалось обогатить данные для одного из дубликатов", "internalID", internalID, "error", err)
			continue
		}
		enrichedDuplicates = append(enrichedDuplicates, data)
	}

	if len(enrichedDuplicates) < 2 {
		log.Info("После обогащения осталось меньше двух сущностей, задача на дубликаты не создается")
		return result
	}

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

	updates := map[string]interface{}{
		"health_status":  "attention_required",
		"status_details": datatypes.JSON(detailsJSON),
	}

	for _, internalID := range payload.InternalIDs {
		action := Action{
			Type:       ActionUpdate,
			EntityType: payload.EntityType,
			EntityUUID: internalID,
			Updates:    updates,
		}
		result.Actions = append(result.Actions, action)
	}

	return result
}

// ProcessAgentData - главный метод, реализующий согласованную логику сверки.
func (p *processingEngineImpl) ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult {
	result := &ProcessingResult{Actions: []Action{}}
	log := p.logger.With("source", source)

	// 1. Валидация времени агента
	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime == nil {
		log.Warn("Не удалось распознать 'current_time' из данных агента. Обработка прервана.")
		return result
	}
	if currentTime.Before(time.Now().AddDate(0, 0, -60)) {
		log.Info("Данные от агента пропущены, так как они старше 60 дней.", "current_time", *currentTime)
		return result
	}

	// 2. Получение отчета о поиске сущностей (Водопадный алгоритм)
	report, err := p.matcherSvc.GetMatchReport(ctx, data)
	if err != nil {
		log.Error("Ошибка при выполнении поиска сущностей (Matcher)", "error", err)
		return result
	}

	// 3. Обработка Дубликатов
	if len(report.Duplicates) > 0 {
		log.Warn("Обнаружены дубликаты сущностей. Создание задачи resolve_duplicate.", "count", len(report.Duplicates))

		// Извлекаем UUID дубликатов для создания задачи
		var duplicateUUIDs []string
		for _, dup := range report.Duplicates {
			if ws, ok := dup.(workstation.Workstation); ok {
				duplicateUUIDs = append(duplicateUUIDs, ws.ID)
			}
			// TODO:
			// Добавить другие типы, если matcher начнет возвращать дубли по ним
		}

		// Проверяем, есть ли уже задача на эти дубликаты
		existingTask, _ := p.taskRepo.FindActiveDuplicateTaskByMemberUUIDs(ctx, duplicateUUIDs)
		if existingTask == nil {
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
		} else {
			log.Debug("Активная задача на дубликаты уже существует.", "task_id", existingTask.ID)
		}

		return result // Прерываем обработку, нельзя обновлять при дублях
	}

	// 4. Обработка Конфликта Владельцев (Owner Mismatch)
	if report.Conflict {
		srvOwner := utils.SafeStringDereference(report.FoundServer.OwnerID)
		wsOwner := utils.SafeStringDereference(report.FoundWorkstation.OwnerID)

		log.Info("Проверка родства компаний при конфликте владельцев", "server_owner", srvOwner, "ws_owner", wsOwner)

		areRelated := p.reconciliationEngine.AreCompaniesRelated(srvOwner, wsOwner)
		if !areRelated {
			// Создаем задачу owner_mismatch
			log.Warn("Владельцы не связаны. Создание задачи owner_mismatch.")

			// В качестве эталонного владельца берем владельца Сервера (Приоритет 1)
			action := p.reconciliationEngine.CreateConflictTask(ctx, "owner_mismatch", srvOwner, data, report.FoundServer, report.FoundWorkstation)
			if action != nil {
				result.Actions = append(result.Actions, *action)
			}
			return result // Прерываем обработку, требуется вмешательство человека
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
		}
		// Продолжаем попытку обновления найденных сущностей, даже если владелец не ясен?
		// Нет, лучше остановиться, это риск.
		return result
	}

	// Проверка контракта владельца
	ownerCompany, err := p.companyRepo.GetByID(ctx, ownerID)
	if err == nil && ownerCompany != nil {
		if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
			log.Debug("Обработка данных от агента пропущена: неактивный контракт у владельца", "ownerID", ownerID)
			return result
		}
	}

	// A. Обработка Сервера (Привязка владельца, если отсутствует)
	if report.FoundServer != nil && report.FoundServer.HealthStatus != "locked" {
		// Если у сервера нет владельца, привязываем найденного
		if report.FoundServer.OwnerID == nil || *report.FoundServer.OwnerID == "" {
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
		}
	}

	// B. Обработка Рабочей станции
	agentWSData := map[string]interface{}{
		"teamviewer":  utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID)),
		"litemanager": utils.SafeStringDereference(validators.ExtractLiteManagerID(data.AdditionalProperties, data.LitemanagerID)),
		"hostname":    data.Hostname,
	}

	if report.FoundWorkstation != nil {
		if report.FoundWorkstation.HealthStatus != "locked" {
			_, action := p.reconciliationEngine.CompareEntityData(ctx, EntityTypeWorkstation, agentWSData, report.FoundWorkstation)
			if action != nil {
				result.Actions = append(result.Actions, *action)
			}
		}
	} else {
		// РС не найдена, но есть данные для неё -> add_equipment
		// Проверяем, есть ли валидные данные для создания
		if agentWSData["teamviewer"] != "" || agentWSData["litemanager"] != "" {
			log.Info("Рабочая станция не найдена, создание задачи add_equipment")
			action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
			if action != nil {
				result.Actions = append(result.Actions, *action)
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

	if report.FoundFR != nil {
		if report.FoundFR.HealthStatus != "locked" {
			_, action := p.reconciliationEngine.CompareEntityData(ctx, EntityTypeFiscalRegister, agentFRData, report.FoundFR)
			if action != nil {
				result.Actions = append(result.Actions, *action)
			}
		}
	} else if data.SerialNumber != "" {
		log.Info("ФР не найден, создание задачи add_equipment")
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			result.Actions = append(result.Actions, *action)
		}
	}

	return result
}

func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, equipmentOwnerID string, server *server.Server, data *api.AgentDataDTO) {
	serverID := "nil"
	if server != nil {
		serverID = server.ID
	}
	p.logger.Debug("processServerActions вызвана", "equipmentOwnerID", equipmentOwnerID, "server_id", serverID, "crm_id", data.CRMID)

	if server == nil {
		p.logger.Debug("server nil, выход из processServerActions")
		return
	}

	if server.HealthStatus == "locked" {
		p.logger.Debug("Обработка сервера пропущена: статус 'locked'", "id", server.ID)
		return
	}

	serverPrimaryOwnerID := *server.OwnerID
	p.logger.Debug("Анализ конфликта владельцев сервера", "server_id", server.ID, "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID)
	areRelated := p.reconciliationEngine.AreCompaniesRelated(equipmentOwnerID, serverPrimaryOwnerID)
	p.logger.Debug("Результат проверки родства компаний", "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID, "are_related", areRelated)

	if !areRelated {
		p.logger.Debug("Компании не связаны, создание задачи owner_mismatch", "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID)
		action := p.reconciliationEngine.CreateConflictTask(ctx, "owner_mismatch", equipmentOwnerID, data, server)
		if action != nil {
			p.logger.Debug("Задача owner_mismatch создана", "task_type", action.Task.TaskType)
			res.Actions = append(res.Actions, *action)
		} else {
			p.logger.Debug("Задача owner_mismatch уже существует, пропускаем")
		}
	}
}

func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, ownerID string, ws *workstation.Workstation, data *api.AgentDataDTO) {
	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))
	agentAD := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.AnydeskID))

	if ws != nil {
		if ws.HealthStatus == "locked" {
			return
		}
		agentData := map[string]interface{}{
			"teamviewer":  agentTV,
			"litemanager": agentLM,
			"anydesk":     agentAD,
		}
		if hasChanges, updateAction := p.reconciliationEngine.CompareEntityData(ctx, "Workstation", agentData, ws); hasChanges {
			res.Actions = append(res.Actions, *updateAction)
		}
	} else if agentTV != "" || agentLM != "" || agentAD != "" {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			res.Actions = append(res.Actions, *action)
			p.logger.Debug("Задача add_equipment создана", "task_type", action.Task.TaskType)
		} else {
			p.logger.Debug("Задача add_equipment уже существует, пропускаем")
		}
	}
}

func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, ownerID string, fr *fiscal.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		if fr.HealthStatus == "locked" {
			return
		}
		agentData := map[string]interface{}{
			// Существующие поля
			"dateTime_end":     data.DateTimeEnd,
			"RNM":              data.RNM,
			"organizationName": data.OrganizationName,
			"INN":              data.INN,
			"modelName":        data.ModelName,
			// Новые поля из данных агента
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
		}
	} else if data.SerialNumber != "" {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		if action != nil {
			res.Actions = append(res.Actions, *action)
		}
	}
}
