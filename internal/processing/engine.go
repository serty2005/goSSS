// internal/processing/engine.go
package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/logger"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"time"

	"gorm.io/datatypes"
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
	logger          logger.LoggerInterface
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	companyRepo     repositories.CompanyRepo
	taskRepo        repositories.TaskRepo
	matcherSvc      services.EntityMatcherService
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
) ProcessingEngine {
	return &processingEngineImpl{
		logger, serverRepo, workstationRepo, frRepo, companyRepo, taskRepo, matcherSvc,
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

	unidentifiedTaskUUID := fmt.Sprintf("%s@%s", data.Hostname, data.URLRms)

	mainMatch := p.matcherSvc.FindEntityByAgentData(ctx, data)
	if mainMatch == nil {
		p.createTaskIfNotExists(ctx, result, "new_client", "", unidentifiedTaskUUID, "", data, "Не удалось идентифицировать оборудование. Требуется создать нового клиента и привязать оборудование.")
		return result
	}

	foundServer, _ := p.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(validators.ValidateIPAddress(data.URLRms)))
	foundWS, _ := p.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, "", data.LitemanagerID)
	foundFR, _ := p.frRepo.FindBySerialNumber(ctx, data.SerialNumber)

	etalonOwnerID, err := p.getEquipmentOwnerID(foundWS, foundFR)
	if err != nil {
		if foundServer != nil && foundServer.OwnerID != nil {
			etalonOwnerID = *foundServer.OwnerID
		} else {
			p.createTaskIfNotExists(ctx, result, "data_conflict", "", unidentifiedTaskUUID, "", data, err.Error())
			return result
		}
	}

	log.Info("Эталонный владелец оборудования определен", "ownerID", etalonOwnerID)

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

// getEquipmentOwnerID определяет владельца по оборудованию (РС или ФР).
func (p *processingEngineImpl) getEquipmentOwnerID(ws *models.Workstation, fr *models.FiscalRegister) (string, error) {
	wsOwner := ""
	if ws != nil && ws.OwnerID != nil {
		wsOwner = *ws.OwnerID
	}
	frOwner := ""
	if fr != nil && fr.OwnerID != nil {
		frOwner = *fr.OwnerID
	}

	if wsOwner != "" && frOwner != "" && wsOwner != frOwner {
		return "", fmt.Errorf("конфликт владельцев: РС принадлежит '%s', а ФР - '%s'", wsOwner, frOwner)
	}
	if wsOwner != "" {
		return wsOwner, nil
	}
	if frOwner != "" {
		return frOwner, nil
	}
	return "", fmt.Errorf("не удалось определить владельца оборудования: не найдено ни РС, ни ФР с владельцем")
}

// processServerActions формирует план действий для Сервера.
func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, equipmentOwnerID string, server *models.Server, data *api.AgentDataDTO) {
	if server == nil {
		return
	}
	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime != nil && server.LastModifiedDate != nil && currentTime.Before(*server.LastModifiedDate) {
		p.logger.Info("Обновление сервера пропущено: данные от агента старше, чем запись в БД", "server_id", server.ID, "agent_time", *currentTime, "db_time", *server.LastModifiedDate)
		return
	}
	if server.Status == "locked" {
		p.logger.Debug("Обработка сервера пропущена: статус 'locked'", "id", server.ID)
		return
	}

	serverPrimaryOwnerID := *server.OwnerID
	equipmentOwnerParents, _ := p.companyRepo.GetAllParentIDs(ctx, equipmentOwnerID)
	serverOwnerParents, _ := p.companyRepo.GetAllParentIDs(ctx, serverPrimaryOwnerID)
	areRelated := p.areCompaniesRelated(equipmentOwnerID, serverPrimaryOwnerID, equipmentOwnerParents, serverOwnerParents)

	if areRelated {
		// Логика добавления доп. владельца (уже работает с внутренними ID, но нужно проверить)
	} else {
		comment := fmt.Sprintf("Конфликт владения сервером! Оборудование принадлежит '%s', но оно подключено к серверу '%s', который принадлежит '%s'.", equipmentOwnerID, server.ID, serverPrimaryOwnerID)
		p.createTaskIfNotExists(ctx, res, "data_conflict", "Server", server.ID, equipmentOwnerID, data, comment)
	}

	updates := make(map[string]interface{})
	if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
		updates["crm_id"] = data.CRMID
	}
	if len(updates) > 0 {
		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Server", EntityUUID: server.ID, Updates: updates})
	}
}

// areCompaniesRelated проверяет, связаны ли две компании.
func (p *processingEngineImpl) areCompaniesRelated(owner1, owner2 string, parents1, parents2 []string) bool {
	if owner1 == owner2 {
		return true
	}
	for _, p1 := range parents1 {
		if p1 == owner2 {
			return true
		}
	}
	for _, p2 := range parents2 {
		if p2 == owner1 {
			return true
		}
	}
	parents1Set := make(map[string]struct{})
	for _, p1 := range parents1 {
		parents1Set[p1] = struct{}{}
	}
	for _, p2 := range parents2 {
		if _, ok := parents1Set[p2]; ok {
			return true
		}
	}
	return false
}

// processWorkstationActions формирует план действий для Рабочей станции.
func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, ownerID string, ws *models.Workstation, data *api.AgentDataDTO) {
	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))

	if ws != nil {
		if ws.Status != nil && *ws.Status == "locked" {
			return
		}
		updates := make(map[string]interface{})
		if (ws.Teamviewer == nil || *ws.Teamviewer == "") && agentTV != "" {
			updates["teamviewer"] = agentTV
		}
		if (ws.Litemanager == nil || *ws.Litemanager == "") && agentLM != "" {
			updates["litemanager"] = agentLM
		}
		if len(updates) > 0 {
			res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Workstation", EntityUUID: ws.ID, Updates: updates})
		}
	} else if agentTV != "" || agentLM != "" {
		entityID := agentTV
		if entityID == "" {
			entityID = agentLM
		}
		comment := fmt.Sprintf("Добавить новую рабочую станцию для владельца '%s'. TV: %s, LM: %s.", ownerID, agentTV, agentLM)
		p.createTaskIfNotExists(ctx, res, "add_equipment", "Workstation", entityID, ownerID, data, comment)
	}
}

// processFiscalRegisterActions формирует план действий для ФР.
func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, ownerID string, fr *models.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		if fr.Status != nil && *fr.Status == "locked" {
			return
		}
		updates := make(map[string]interface{})
		// ... (логика обновления полей ФР)
		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "FiscalRegister", EntityUUID: fr.ID, Updates: updates})
	} else if data.SerialNumber != "" {
		comment := fmt.Sprintf("Добавить новый ФР (СН: %s) для владельца '%s'.", data.SerialNumber, ownerID)
		p.createTaskIfNotExists(ctx, res, "add_equipment", "FiscalRegister", data.SerialNumber, ownerID, data, comment)
	}
}

// createTaskIfNotExists создает задачу, если активной задачи такого типа еще нет.
func (p *processingEngineImpl) createTaskIfNotExists(ctx context.Context, res *ProcessingResult, taskType, entityType, entityUUID, etalonOwnerID string, data *api.AgentDataDTO, comment string) {
	existingTask, _ := p.taskRepo.FindActiveTask(ctx, taskType, entityUUID)
	if existingTask != nil {
		return
	}
	detailsMap := make(map[string]interface{})
	detailsMap["agent_data"] = data
	if etalonOwnerID != "" {
		detailsMap["etalon_owner_id"] = etalonOwnerID
	}
	details, _ := json.Marshal(detailsMap)
	task := &models.ReconciliationTask{
		TaskType: taskType, EntityType: entityType, EntityUUID: entityUUID,
		Details: datatypes.JSON(details), Status: "new", Comment: comment,
	}
	res.Actions = append(res.Actions, Action{Type: ActionCreateTask, Task: task})
}
