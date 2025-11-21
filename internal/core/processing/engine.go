// Файл: internal/core/processing/engine.go
package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
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
	serverRepo           repositories.ServerRepo
	workstationRepo      repositories.WorkstationRepo
	frRepo               repositories.FiscalRegisterRepo
	companyRepo          repositories.CompanyRepo
	taskRepo             repositories.TaskRepo
	matcherSvc           services.EntityMatcherService
	linkRepo             repositories.LinkRepo
	reconciliationEngine ReconciliationEngine
}

// NewProcessingEngine создает новый экземпляр движка.
func NewProcessingEngine(
	logger logger.LoggerInterface,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	companyRepo repositories.CompanyRepo,
	taskRepo repositories.TaskRepo,
	matcherSvc services.EntityMatcherService,
	linkRepo repositories.LinkRepo,
	reconciliationEngine ReconciliationEngine,
) ProcessingEngine {
	return &processingEngineImpl{
		logger, serverRepo, workstationRepo, frRepo, companyRepo, taskRepo, matcherSvc, linkRepo, reconciliationEngine,
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

// ProcessAgentData - главный метод, реализующий согласованную логику.
func (p *processingEngineImpl) ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult {
	result := &ProcessingResult{Actions: []Action{}}
	log := p.logger.With("source", source)

	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime == nil {
		log.Warn("Не удалось распознать 'current_time' из данных агента. Обработка прервана.")
		return result
	}
	if currentTime.Before(time.Now().AddDate(0, 0, -60)) {
		log.Info("Данные от агента пропущены, так как они старше 60 дней.", "current_time", *currentTime)
		return result
	}

	// Используем водопадную логику поиска для надежного сопоставления
	match := p.matcherSvc.FindEntityByAgentData(ctx, data)

	// Если нет совпадений, создаем задачу на нового клиента
	if match == nil {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "new_client", "", data)
		result.Actions = append(result.Actions, *action)
		return result
	}

	log.Info("Найдено совпадение для обработки",
		"entity_type", match.EntityType,
		"owner_id", match.OwnerUUID)

	// Получаем ID владельца из найденного совпадения
	etalonOwnerID := match.OwnerUUID
	if etalonOwnerID == "" {
		log.Error("Владелец не найден в совпадении")
		action := p.reconciliationEngine.CreateConflictTask(ctx, "data_conflict", "", data)
		result.Actions = append(result.Actions, *action)
		return result
	}

	log.Info("Эталонный владелец оборудования определен", "ownerID", etalonOwnerID)

	foundServer, _ := p.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(validators.ValidateIPAddress(data.URLRms)))
	foundWS, _ := p.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, "", data.LitemanagerID)
	if foundWS == nil && data.AnydeskID != "" && data.AnydeskID != "None" {
		foundWS, _ = p.workstationRepo.FindByRemoteIDs(ctx, "", data.AnydeskID, "")
	}
	foundFR, _ := p.frRepo.FindBySerialNumber(ctx, data.SerialNumber)

	ownerCompany, err := p.companyRepo.GetByID(ctx, etalonOwnerID)
	if err != nil || ownerCompany == nil {
		log.Error("Не удалось получить данные о компании-владельце, обработка прервана", "ownerID", etalonOwnerID, "error", err)
		return result
	}

	if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
		log.Debug("Обработка данных от агента пропущена: неактивный контракт у владельца", "ownerID", etalonOwnerID)
		return result
	}

	p.processServerActions(ctx, result, etalonOwnerID, foundServer, data)
	p.processWorkstationActions(ctx, result, etalonOwnerID, foundWS, data)
	p.processFiscalRegisterActions(ctx, result, etalonOwnerID, foundFR, data)

	return result
}

func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, equipmentOwnerID string, server *models.Server, data *api.AgentDataDTO) {
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
		p.logger.Debug("Задача owner_mismatch создана", "task_type", action.Task.TaskType, "entity_type", action.Task.EntityType)
		res.Actions = append(res.Actions, *action)
	}
}

func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, ownerID string, ws *models.Workstation, data *api.AgentDataDTO) {
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
		res.Actions = append(res.Actions, *action)
	}
}

func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, ownerID string, fr *models.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		if fr.HealthStatus == "locked" {
			return
		}
		agentData := map[string]interface{}{
			// Существующие поля
			"dateTime_end":     data.DateTimeEnd,
			"licenses":         data.Licenses,
			"RNM":              data.RNM,
			"organizationName": data.OrganizationName,
			"INN":              data.INN,
			"modelName":        data.ModelName,
			// Новые поля из данных агента
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
		res.Actions = append(res.Actions, *action)
	}
}
