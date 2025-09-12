package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/internal/transport/http/validators"
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
	linkRepo        repositories.LinkRepo
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
) ProcessingEngine {
	return &processingEngineImpl{
		logger, serverRepo, workstationRepo, frRepo, companyRepo, taskRepo, matcherSvc, linkRepo,
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

	ownerCompany, err := p.companyRepo.GetByID(ctx, *server.OwnerID)
	if err != nil || ownerCompany == nil {
		p.logger.Error("Не удалось получить компанию-владельца сервера для проверки контракта", "server_id", server.ID, "owner_id", *server.OwnerID, "error", err)
		return
	}
	if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
		p.logger.Info("Обработка сервера пропущена: неактивный контракт у владельца", "server_id", server.ID, "owner_id", ownerCompany.ID)
		return
	}

	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime != nil && server.LastModifiedDate != nil && currentTime.Before(*server.LastModifiedDate) {
		p.logger.Info("Обновление сервера пропущено: данные от агента старше, чем запись в БД", "server_id", server.ID, "agent_time", *currentTime, "db_time", *server.LastModifiedDate)
		return
	}
	if server.HealthStatus == "locked" {
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
		// Обогащаем данные для обеих конфликтующих сторон
		serverInfo, errSrv := p.getEnrichmentDataForEntity(ctx, "Server", server.ID)
		if errSrv != nil {
			p.logger.Error("Не удалось обогатить данные для сервера в конфликте", "server_id", server.ID, "error", errSrv)
			return
		}

		mainMatch := p.matcherSvc.FindEntityByAgentData(ctx, data)
		var equipmentInfo map[string]interface{}
		var errEq error
		if mainMatch != nil {
			var entityID string
			switch e := mainMatch.Entity.(type) {
			case *models.Server:
				entityID = e.ID
			case *models.Workstation:
				entityID = e.ID
			case *models.FiscalRegister:
				entityID = e.ID
			}
			equipmentInfo, errEq = p.getEnrichmentDataForEntity(ctx, mainMatch.EntityType, entityID)
			if errEq != nil {
				p.logger.Error("Не удалось обогатить данные для оборудования в конфликте", "equipment_id", entityID, "error", errEq)
				return
			}
		}

		statusDetails := map[string]interface{}{
			"type":           "owner_mismatch",
			"reason":         "Оборудование (ФР/РС) принадлежит одному клиенту, но подключено к серверу другого, не связанного клиента.",
			"timestamp":      time.Now().UTC().Format(time.RFC3339),
			"source":         "agent_processing",
			"server_info":    serverInfo,
			"equipment_info": equipmentInfo,
		}

		detailsJSON, err := json.Marshal(statusDetails)
		if err != nil {
			p.logger.Error("Не удалось сериализовать status_details для owner_mismatch", "server_id", server.ID, "error", err)
			return
		}
		updates := map[string]interface{}{
			"health_status":  "attention_required",
			"status_details": datatypes.JSON(detailsJSON),
		}
		res.Actions = append(res.Actions, Action{
			Type:       ActionUpdate,
			EntityType: "Server",
			EntityUUID: server.ID,
			Updates:    updates,
		})
	}

	updates := make(map[string]interface{})
	if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
		updates["crm_id"] = data.CRMID
	}
	if len(updates) > 0 {
		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Server", EntityUUID: server.ID, Updates: updates})
	}
}

// getEnrichmentDataForEntity собирает полную информацию о сущности для записи в StatusDetails.
func (p *processingEngineImpl) getEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error) {
	var ownerID string
	var lmd *time.Time

	switch entityType {
	case "Server":
		entity, err := p.serverRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти сервер с ID %s: %w", entityID, err)
		}
		ownerID = *entity.OwnerID
		lmd = entity.LastModifiedDate
	case "Workstation":
		entity, err := p.workstationRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти РС с ID %s: %w", entityID, err)
		}
		ownerID = *entity.OwnerID
		lmd = entity.LastModifiedDate
	case "FiscalRegister":
		entity, err := p.frRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти ФР с ID %s: %w", entityID, err)
		}
		ownerID = *entity.OwnerID
		lmd = entity.LastModifiedDate
	default:
		return nil, fmt.Errorf("неподдерживаемый тип сущности: %s", entityType)
	}

	link, _ := p.linkRepo.GetByInternalID(ctx, nil, "naumen", entityID)
	owner, _ := p.companyRepo.GetByID(ctx, ownerID)

	var externalID string
	if link != nil {
		externalID = link.ServiceDeskUUID
	}

	var ownerTitle string
	var ownerActiveContract bool
	if owner != nil {
		ownerTitle = *owner.Title
		if owner.ActiveContract != nil {
			ownerActiveContract = *owner.ActiveContract
		}
	}

	return map[string]interface{}{
		"internal_id":        entityID,
		"external_id":        externalID,
		"last_modified_date": lmd,
		"owner_info": map[string]interface{}{
			"id":              ownerID,
			"title":           ownerTitle,
			"active_contract": ownerActiveContract,
		},
	}, nil
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
		if ws.HealthStatus == "locked" {
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
		if fr.HealthStatus == "locked" {
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
