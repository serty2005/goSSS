package processing

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
	"time"
)

// ActionType определяет тип действия, которое должен выполнить Оркестратор.
type ActionType string

const (
	ActionCreateTask         ActionType = "create_task"
	ActionUpdate             ActionType = "update"
	ActionAddAdditionalOwner ActionType = "add_additional_owner"
)

// Action представляет одно действие в плане.
type Action struct {
	Type                ActionType
	EntityType          string
	EntityUUID          string // Внутренний ID сущности
	Updates             map[string]interface{}
	Task                *models.ReconciliationTask
	AdditionalOwnerUUID string // Внутренний ID доп. владельца
}

// ProcessingResult - это "план действий", который Процессор возвращает Оркестратору.
type ProcessingResult struct {
	Actions []Action
}

// ProcessingEngine содержит всю сложную бизнес-логику сверки.
type ProcessingEngine interface {
	ProcessAgentData(ctx context.Context, source string, data *api.AgentDataDTO) *ProcessingResult
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

	mainMatch := p.matcherSvc.FindEntityByAgentData(ctx, data)
	if mainMatch == nil {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "new_client", "", data)
		result.Actions = append(result.Actions, *action)
		return result
	}

	payload := &events.AgentDataPayload{Source: source, Data: *data}
	etalonOwnerID, err := p.reconciliationEngine.DetermineOwner(ctx, payload)
	if err != nil {
		p.logger.Error("Ошибка определения владельца", "error", err)
		action := p.reconciliationEngine.CreateConflictTask(ctx, "data_conflict", "", data)
		result.Actions = append(result.Actions, *action)
		return result
	}
	if etalonOwnerID == "" {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "new_client", "", data)
		result.Actions = append(result.Actions, *action)
		return result
	}

	log.Info("Эталонный владелец оборудования определен", "ownerID", etalonOwnerID)

	foundServer, _ := p.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(validators.ValidateIPAddress(data.URLRms)))
	// Используем правильный порядок поиска: TV/LM сначала, Anydesk как fallback
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

// processServerActions формирует план действий для Сервера.
// Сервер используется только для определения владельца, данные сервера не обновляются.
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

	// Проверяем статус сервера - если заблокирован, пропускаем обработку
	if server.HealthStatus == "locked" {
		p.logger.Debug("Обработка сервера пропущена: статус 'locked'", "id", server.ID)
		return
	}

	serverPrimaryOwnerID := *server.OwnerID
	p.logger.Debug("Анализ конфликта владельцев сервера", "server_id", server.ID, "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID)
	areRelated := p.reconciliationEngine.AreCompaniesRelated(equipmentOwnerID, serverPrimaryOwnerID)
	p.logger.Debug("Результат проверки родства компаний", "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID, "are_related", areRelated)

	if areRelated {
		p.logger.Debug("Компании связаны, задача owner_mismatch не создаётся", "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID)
	} else {
		p.logger.Debug("Компании не связаны, создание задачи owner_mismatch", "server_owner", serverPrimaryOwnerID, "equipment_owner", equipmentOwnerID)
		// Создать задачу на конфликт владельца
		action := p.reconciliationEngine.CreateConflictTask(ctx, "owner_mismatch", equipmentOwnerID, data, server)
		p.logger.Debug("Задача owner_mismatch создана", "task_type", action.Task.TaskType, "entity_type", action.Task.EntityType)
		res.Actions = append(res.Actions, *action)
	}
}

// processWorkstationActions формирует план действий для Рабочей станции.
func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, ownerID string, ws *models.Workstation, data *api.AgentDataDTO) {
	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))
	agentAD := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.AnydeskID))

	if ws != nil {
		if ws.HealthStatus == "locked" {
			return
		}
		// Использовать CompareEntityData для обновления
		agentData := map[string]interface{}{
			"teamviewer":  agentTV,
			"litemanager": agentLM,
			"anydesk":     agentAD,
		}
		hasChanges, updateAction := p.reconciliationEngine.CompareEntityData(ctx, "Workstation", agentData, ws)
		if hasChanges {
			res.Actions = append(res.Actions, *updateAction)
		}
	} else if agentTV != "" || agentLM != "" || agentAD != "" {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		res.Actions = append(res.Actions, *action)
	}
}

// processFiscalRegisterActions формирует план действий для ФР.
func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, ownerID string, fr *models.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		if fr.HealthStatus == "locked" {
			return
		}
		// Использовать CompareEntityData для обновления
		agentData := map[string]interface{}{
			"dateTime_end":     data.DateTimeEnd,
			"licenses":         data.Licenses,
			"RNM":              data.RNM,
			"organizationName": data.OrganizationName,
			"INN":              data.INN,
			"modelName":        data.ModelName,
		}
		hasChanges, updateAction := p.reconciliationEngine.CompareEntityData(ctx, "FiscalRegister", agentData, fr)
		if hasChanges {
			res.Actions = append(res.Actions, *updateAction)
		}
	} else if data.SerialNumber != "" {
		action := p.reconciliationEngine.CreateConflictTask(ctx, "add_equipment", ownerID, data)
		res.Actions = append(res.Actions, *action)
	}
}
