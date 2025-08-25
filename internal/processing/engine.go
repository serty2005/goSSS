package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/internal/utils"
	"etalon-server/internal/validators"
	"fmt"
	"strings"

	"go.uber.org/zap"
	"gorm.io/datatypes"
)

// ActionType определяет тип действия, которое должен выполнить Оркестратор.
type ActionType string

const (
	ActionCreateTask  ActionType = "create_task"
	ActionUpdate      ActionType = "update"
	ActionCommentTask ActionType = "comment_task" // Новое действие
)

// Action представляет одно действие в плане.
type Action struct {
	Type       ActionType
	EntityType string
	EntityUUID string
	Updates    map[string]interface{}
	Task       *models.ReconciliationTask
	TaskID     uint   // для ActionCommentTask
	Comment    string // для ActionCommentTask
}

// ProcessingResult - это "план действий", который Процессор возвращает Оркестратору.
type ProcessingResult struct {
	Actions []Action
}

// ProcessingEngine содержит всю сложную бизнес-логику сверки.
type ProcessingEngine interface {
	ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) *ProcessingResult
}

type processingEngineImpl struct {
	logger          *zap.Logger
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	companyRepo     repositories.CompanyRepo
	taskRepo        repositories.TaskRepo
	matcherSvc      services.EntityMatcherService
}

// NewProcessingEngine создает новый экземпляр движка.
func NewProcessingEngine(
	logger *zap.Logger,
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
func (p *processingEngineImpl) ProcessAgentData(ctx context.Context, data *api.AgentDataDTO) *ProcessingResult {
	result := &ProcessingResult{Actions: []Action{}}
	logIdentifier := data.SerialNumber
	if logIdentifier == "" {
		logIdentifier = data.TeamviewerID
	}
	log := p.logger.With(zap.String("log_identifier", logIdentifier))

	unidentifiedTaskUUID := fmt.Sprintf("%s@%s", data.Hostname, data.URLRms)

	mainMatch := p.matcherSvc.FindEntityByAgentData(ctx, data)
	if mainMatch == nil {
		// Для new_client используем SN как уникальный идентификатор, чтобы избежать дублей
		p.createTaskIfNotExists(ctx, result, "new_client", "", unidentifiedTaskUUID, data, "Не удалось идентифицировать оборудование. Требуется создать нового клиента и привязать оборудование.")
		return result
	}

	foundServer, _ := p.serverRepo.FindByCRMidOrIP(ctx, data.CRMID, utils.SafeStringDereference(validators.ValidateIPAddress(data.URLRms)))
	foundWS, _ := p.workstationRepo.FindByRemoteIDs(ctx, data.TeamviewerID, "", data.LitemanagerID)
	foundFR, _ := p.frRepo.FindBySerialNumber(ctx, data.SerialNumber)

	etalonOwnerUUID, err := p.determineEtalonOwner(ctx, foundServer, foundWS, foundFR)
	if err != nil {
		p.createTaskIfNotExists(ctx, result, "data_conflict", "", unidentifiedTaskUUID, data, err.Error())
		return result
	}

	log.Info("Эталонный владелец определен", zap.String("ownerUUID", etalonOwnerUUID))

	p.processServerActions(ctx, result, etalonOwnerUUID, foundServer, data)
	p.processWorkstationActions(ctx, result, etalonOwnerUUID, foundWS, data)
	p.processFiscalRegisterActions(ctx, result, etalonOwnerUUID, foundFR, data)

	return result
}

// determineEtalonOwner реализует сложную логику определения владельца с учетом иерархии.
func (p *processingEngineImpl) determineEtalonOwner(ctx context.Context, server *models.Server, ws *models.Workstation, fr *models.FiscalRegister) (string, error) {
	wsOwner := ""
	if ws != nil {
		wsOwner = utils.SafeStringDereference(ws.OwnerServiceDeskUUID)
	}
	frOwner := ""
	if fr != nil {
		frOwner = utils.SafeStringDereference(fr.OwnerServiceDeskUUID)
	}
	serverOwner := ""
	if server != nil {
		serverOwner = utils.SafeStringDereference(server.OwnerServiceDeskUUID)
	}

	if wsOwner != "" && frOwner != "" && wsOwner != frOwner {
		frOwner = wsOwner
	}

	if serverOwner == "" {
		if wsOwner != "" && frOwner != "" && wsOwner != frOwner {
			return "", fmt.Errorf("неразрешимый конфликт: РС и ФР принадлежат разным владельцам (%s и %s) и нет сервера для определения иерархии", wsOwner, frOwner)
		}
		if wsOwner != "" {
			return wsOwner, nil
		}
		if frOwner != "" {
			return frOwner, nil
		}
		return "", fmt.Errorf("критическая ошибка: не найдено ни одной сущности с владельцем")
	}

	ownerOfEquipment := wsOwner
	if ownerOfEquipment == "" {
		ownerOfEquipment = frOwner
	}

	if ownerOfEquipment == "" || ownerOfEquipment == serverOwner {
		return serverOwner, nil
	}

	parentUUIDs, err := p.companyRepo.GetAllParentUUIDs(ctx, ownerOfEquipment)
	if err != nil {
		p.logger.Error("Не удалось получить иерархию компании", zap.String("childUUID", ownerOfEquipment), zap.Error(err))
		return "", fmt.Errorf("ошибка проверки иерархии компаний")
	}

	isChild := false
	for _, parentUUID := range parentUUIDs {
		if parentUUID == serverOwner {
			isChild = true
			break
		}
	}

	if isChild {
		return ownerOfEquipment, nil
	}

	return "", fmt.Errorf("конфликт владельцев: Сервер принадлежит '%s', а оборудование - '%s', иерархическая связь не найдена", serverOwner, ownerOfEquipment)
}

// processServerActions формирует план действий для Сервера.
func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, owner string, server *models.Server, data *api.AgentDataDTO) {
	if server != nil {
		if utils.SafeStringDereference(server.OwnerServiceDeskUUID) != owner {
			p.createOwnerMismatchTask(ctx, res, "Server", *server.ServiceDeskUUID, utils.SafeStringDereference(server.DeviceName), owner, data)
		}
		updates := make(map[string]interface{})
		if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
			updates["crm_id"] = data.CRMID
		}
		if len(updates) > 0 {
			res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Server", EntityUUID: *server.ServiceDeskUUID, Updates: updates})
		}
	} else if data.CRMID != "" || validators.ValidateIPAddress(data.URLRms) != nil {
		comment := fmt.Sprintf("Агент '%s' сообщил о сервере (IP: %s, CRMID: %s), который отсутствует в базе. Проверьте, нужно ли создать новую сущность сервера и привязать к владельцу '%s'.",
			data.Hostname, data.URLRms, data.CRMID, owner)
		// Для этой задачи используем CRMID или IP как уникальный идентификатор
		entityID := data.CRMID
		if entityID == "" {
			entityID = data.URLRms
		}
		p.createTaskIfNotExists(ctx, res, "owner_check_required", "Server", entityID, data, comment)
	}
}

// processWorkstationActions формирует план действий для Рабочей станции.
func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, owner string, ws *models.Workstation, data *api.AgentDataDTO) {
	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))

	if ws != nil {
		if utils.SafeStringDereference(ws.OwnerServiceDeskUUID) != owner {
			p.createOwnerMismatchTask(ctx, res, "Workstation", *ws.ServiceDeskUUID, utils.SafeStringDereference(ws.DeviceName), owner, data)
		}

		updates := make(map[string]interface{})
		if (ws.Teamviewer == nil || *ws.Teamviewer == "") && agentTV != "" {
			updates["teamviewer"] = agentTV
		} else if agentTV != "" && *ws.Teamviewer != agentTV {
			comment := fmt.Sprintf("Конфликт Teamviewer ID для РС '%s' (%s). В базе: %s, от агента: %s.",
				utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID, *ws.Teamviewer, agentTV)
			p.createTaskIfNotExists(ctx, res, "data_conflict", "Workstation", *ws.ServiceDeskUUID, data, comment)
		}
		if (ws.Litemanager == nil || *ws.Litemanager == "") && agentLM != "" {
			updates["litemanager"] = agentLM
		} else if agentLM != "" && *ws.Litemanager != agentLM {
			comment := fmt.Sprintf("Конфликт Litemanager ID для РС '%s' (%s). В базе: %s, от агента: %s.",
				utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID, *ws.Litemanager, agentLM)
			p.createTaskIfNotExists(ctx, res, "data_conflict", "Workstation", *ws.ServiceDeskUUID, data, comment)
		}

		if len(updates) > 0 {
			res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Workstation", EntityUUID: *ws.ServiceDeskUUID, Updates: updates})
		}

	} else if agentTV != "" || agentLM != "" {
		comment := fmt.Sprintf("Добавить новую рабочую станцию для владельца '%s'. TV: %s, LM: %s.", owner, agentTV, agentLM)
		// Для этой задачи используем TV или LM ID как уникальный идентификатор
		entityID := agentTV
		if entityID == "" {
			entityID = agentLM
		}
		p.createTaskIfNotExists(ctx, res, "add_equipment", "Workstation", entityID, data, comment)
	}
}

// processFiscalRegisterActions формирует план действий для ФР.
func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, owner string, fr *models.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		if utils.SafeStringDereference(fr.OwnerServiceDeskUUID) != owner {
			p.createOwnerMismatchTask(ctx, res, "FiscalRegister", *fr.ServiceDeskUUID, utils.SafeStringDereference(fr.FRSerialNumber), owner, data)
		}
		updates := map[string]interface{}{
			"model_kkt": data.ModelName, "rn_kkt": utils.NormalizeRNKKT(data.RNM),
			"fn_number": data.FNSerial, "inn": strings.TrimSpace(data.INN),
			"ffd":            utils.FormatFFDVersion(data.FFDVersion),
			"fn_expire_date": utils.ParseAgentTime(data.DateTimeEnd),
		}
		if data.InstalledDriver != "" {
			updates["driver_version"] = data.InstalledDriver
		}
		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "FiscalRegister", EntityUUID: *fr.ServiceDeskUUID, Updates: updates})

	} else if data.SerialNumber != "" {
		comment := fmt.Sprintf("Добавить новый ФР (СН: %s) для владельца '%s'.", data.SerialNumber, owner)
		// Для этой задачи используем серийный номер как уникальный идентификатор
		p.createTaskIfNotExists(ctx, res, "add_equipment", "FiscalRegister", data.SerialNumber, data, comment)
	}
}

// createOwnerMismatchTask проверяет наличие задачи о дублях перед созданием задачи о смене владельца.
func (p *processingEngineImpl) createOwnerMismatchTask(ctx context.Context, res *ProcessingResult, entityType, entityUUID, entityName, owner string, data *api.AgentDataDTO) {
	duplicateTask, err := p.taskRepo.FindActiveDuplicateTaskByMemberUUIDs(ctx, []string{entityUUID})
	if err != nil {
		p.logger.Error("Ошибка проверки на дубликаты перед созданием задачи owner_mismatch", zap.Error(err))
		// Все равно создаем задачу, чтобы не потерять информацию
	}

	if duplicateTask != nil {
		// Задача о дублях уже есть! Добавляем комментарий к ней.
		comment := fmt.Sprintf("\n[АВТОМАТИЧЕСКИ] Обнаружено также несоответствие владельца для участника этой группы дубликатов (%s: %s). Ожидаемый владелец: %s.",
			entityType, entityUUID, owner)
		res.Actions = append(res.Actions, Action{Type: ActionCommentTask, TaskID: duplicateTask.ID, Comment: comment})
		return
	}

	// Задачи о дублях нет, создаем новую задачу о смене владельца.
	comment := fmt.Sprintf("Несоответствие владельца для %s '%s' (%s). Ожидаемый владелец: %s.",
		entityType, entityName, entityUUID, owner)
	task := p.buildTask("owner_mismatch", entityType, entityUUID, data, comment)
	res.Actions = append(res.Actions, Action{Type: ActionCreateTask, Task: task})
}

// createTaskIfNotExists - централизованный хелпер для создания задач с проверкой на дублирование.
func (p *processingEngineImpl) createTaskIfNotExists(ctx context.Context, res *ProcessingResult, taskType, entityType, entityUUID string, data *api.AgentDataDTO, comment string) {
	// Для задач, не привязанных к существующей сущности, entityUUID может быть не-UUID строкой. Это нормально.
	existingTask, err := p.taskRepo.FindActiveTask(ctx, taskType, entityUUID)
	if err != nil {
		p.logger.Error("Ошибка при поиске существующей активной задачи",
			zap.String("taskType", taskType), zap.String("entityUUID", entityUUID), zap.Error(err))
		// В случае ошибки все равно пытаемся создать задачу, чтобы не потерять информацию
	}

	if existingTask != nil {
		p.logger.Info("Активная задача уже существует, новая не создается",
			zap.String("taskType", taskType), zap.String("entityUUID", entityUUID), zap.Uint("existingTaskID", existingTask.ID))
		return
	}

	task := p.buildTask(taskType, entityType, entityUUID, data, comment)
	res.Actions = append(res.Actions, Action{Type: ActionCreateTask, Task: task})
}

// buildTask - универсальный конструктор задач.
func (p *processingEngineImpl) buildTask(taskType, entityType, entityUUID string, agentData *api.AgentDataDTO, comment string) *models.ReconciliationTask {
	details, _ := json.Marshal(agentData)
	return &models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: entityType,
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
}
