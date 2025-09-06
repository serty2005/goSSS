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
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
)

// ActionType определяет тип действия, которое должен выполнить Оркестратор.
type ActionType string

const (
	ActionCreateTask         ActionType = "create_task"
	ActionUpdate             ActionType = "update"
	ActionCommentTask        ActionType = "comment_task" // Новое действие
	ActionAddAdditionalOwner ActionType = "add_additional_owner"
)

// Action представляет одно действие в плане.
type Action struct {
	Type                ActionType
	EntityType          string
	EntityUUID          string
	Updates             map[string]interface{}
	Task                *models.ReconciliationTask
	TaskID              uint   // для ActionCommentTask
	Comment             string // для ActionCommentTask
	AdditionalOwnerUUID string // для ActionAddAdditionalOwner
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

	// --- НОВЫЙ ФИЛЬТР: Проверка на возраст данных ---
	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime == nil {
		log.Warn("Не удалось распознать 'current_time' из данных агента. Обработка прервана.")
		return result
	}
	if currentTime.Before(time.Now().AddDate(0, 0, -60)) {
		log.Info("Данные от агента пропущены, так как они старше 60 дней.", zap.Time("current_time", *currentTime))
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

	etalonOwnerUUID, err := p.getEquipmentOwnerUUID(foundWS, foundFR)
	if err != nil {
		if foundServer != nil && foundServer.OwnerServiceDeskUUID != nil {
			etalonOwnerUUID = *foundServer.OwnerServiceDeskUUID
		} else {
			p.createTaskIfNotExists(ctx, result, "data_conflict", "", unidentifiedTaskUUID, "", data, err.Error())
			return result
		}
	}

	log.Info("Эталонный владелец оборудования определен", zap.String("ownerUUID", etalonOwnerUUID))

	ownerCompany, err := p.companyRepo.GetByUUID(ctx, etalonOwnerUUID)
	if err != nil || ownerCompany == nil {
		log.Error("Не удалось получить данные о компании-владельце, обработка прервана", zap.String("ownerUUID", etalonOwnerUUID), zap.Error(err))
		return result
	}

	if ownerCompany.ActiveContract == nil || !*ownerCompany.ActiveContract {
		log.Debug("Обработка данных от агента пропущена: неактивный контракт у владельца", zap.String("ownerUUID", etalonOwnerUUID))
		return result
	}

	p.processServerActions(ctx, result, etalonOwnerUUID, foundServer, data)
	p.processWorkstationActions(ctx, result, etalonOwnerUUID, foundWS, data)
	p.processFiscalRegisterActions(ctx, result, etalonOwnerUUID, foundFR, data)

	return result
}

// getEquipmentOwnerUUID определяет владельца по оборудованию (РС или ФР).
func (p *processingEngineImpl) getEquipmentOwnerUUID(ws *models.Workstation, fr *models.FiscalRegister) (string, error) {
	wsOwner := ""
	if ws != nil && ws.OwnerServiceDeskUUID != nil {
		wsOwner = *ws.OwnerServiceDeskUUID
	}
	frOwner := ""
	if fr != nil && fr.OwnerServiceDeskUUID != nil {
		frOwner = *fr.OwnerServiceDeskUUID
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

// processServerActions формирует план действий для Сервера с учетом иерархии холдинга.
func (p *processingEngineImpl) processServerActions(ctx context.Context, res *ProcessingResult, equipmentOwnerUUID string, server *models.Server, data *api.AgentDataDTO) {
	if server == nil {
		// Логика для случая, когда сервер не найден, но должен быть
		// ... (остается без изменений)
		return
	}

	// --- НОВЫЙ ФИЛЬТР: Проверка на актуальность при обновлении ---
	currentTime := utils.ParseAgentTime(data.CurrentTime)
	if currentTime != nil && server.LastModifiedDate != nil && currentTime.Before(*server.LastModifiedDate) {
		p.logger.Info("Обновление сервера пропущено: данные от агента старше, чем запись в БД",
			zap.String("server_uuid", *server.ServiceDeskUUID),
			zap.Time("agent_time", *currentTime),
			zap.Time("db_time", *server.LastModifiedDate))
		return
	}

	if server.Status == "locked" {
		p.logger.Debug("Обработка сервера пропущена: статус 'locked'", zap.String("uuid", *server.ServiceDeskUUID))
		return
	}

	serverPrimaryOwnerUUID := *server.OwnerServiceDeskUUID
	equipmentOwnerParents, err := p.companyRepo.GetAllParentUUIDs(ctx, equipmentOwnerUUID)
	if err != nil {
		p.logger.Error("Не удалось получить иерархию для владельца оборудования", zap.String("ownerUUID", equipmentOwnerUUID), zap.Error(err))
	}
	serverOwnerParents, err := p.companyRepo.GetAllParentUUIDs(ctx, serverPrimaryOwnerUUID)
	if err != nil {
		p.logger.Error("Не удалось получить иерархию для владельца сервера", zap.String("ownerUUID", serverPrimaryOwnerUUID), zap.Error(err))
	}

	areRelated := p.areCompaniesRelated(equipmentOwnerUUID, serverPrimaryOwnerUUID, equipmentOwnerParents, serverOwnerParents)

	if areRelated {
		isAlreadyAdditionalOwner := false
		for _, owner := range server.AdditionalOwners {
			if owner.ServiceDeskUUID != nil && *owner.ServiceDeskUUID == equipmentOwnerUUID {
				isAlreadyAdditionalOwner = true
				break
			}
		}
		if equipmentOwnerUUID != serverPrimaryOwnerUUID && !isAlreadyAdditionalOwner {
			res.Actions = append(res.Actions, Action{
				Type:                ActionAddAdditionalOwner,
				EntityUUID:          *server.ServiceDeskUUID,
				AdditionalOwnerUUID: equipmentOwnerUUID,
			})
			p.logger.Info("Сформировано действие на добавление дополнительного владельца",
				zap.String("server", *server.ServiceDeskUUID),
				zap.String("new_owner", equipmentOwnerUUID))
		}
	} else {
		comment := fmt.Sprintf(
			"Конфликт владения сервером! Оборудование (РС/ФР) принадлежит '%s', но оно подключено к серверу '%s', который принадлежит '%s'. Эти компании не связаны иерархически.",
			equipmentOwnerUUID, *server.ServiceDeskUUID, serverPrimaryOwnerUUID,
		)
		p.createTaskIfNotExists(ctx, res, "data_conflict", "Server", *server.ServiceDeskUUID, equipmentOwnerUUID, data, comment)
	}

	updates := make(map[string]interface{})
	if (server.CRMid == nil || *server.CRMid == "") && data.CRMID != "" {
		updates["crm_id"] = data.CRMID
	}
	if len(updates) > 0 {
		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Server", EntityUUID: *server.ServiceDeskUUID, Updates: updates})
	}
}

// areCompaniesRelated проверяет, связаны ли две компании (одна структура холдинга).
func (p *processingEngineImpl) areCompaniesRelated(owner1, owner2 string, parents1, parents2 []string) bool {
	if owner1 == owner2 {
		return true // Это одна и та же компания
	}

	// Проверяем, является ли одна родителем для другой
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

	// Проверяем, есть ли у них общий родитель (являются "сестринскими")
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
func (p *processingEngineImpl) processWorkstationActions(ctx context.Context, res *ProcessingResult, owner string, ws *models.Workstation, data *api.AgentDataDTO) {
	agentTV := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.TeamviewerID))
	agentLM := utils.SafeStringDereference(validators.ValidateRemoteAccessID(data.LitemanagerID))

	if ws != nil {
		// --- НОВЫЙ ФИЛЬТР: Проверка на актуальность при обновлении ---
		currentTime := utils.ParseAgentTime(data.CurrentTime)
		if currentTime != nil && ws.LastModifiedDate != nil && currentTime.Before(*ws.LastModifiedDate) {
			p.logger.Info("Обновление рабочей станции пропущено: данные от агента старше, чем запись в БД",
				zap.String("ws_uuid", *ws.ServiceDeskUUID),
				zap.Time("agent_time", *currentTime),
				zap.Time("db_time", *ws.LastModifiedDate))
			return
		}

		if ws.Status != nil && *ws.Status == "locked" {
			p.logger.Debug("Создание задач для сущности пропущено: статус 'locked'", zap.String("uuid", *ws.ServiceDeskUUID))
			return
		}

		updates := make(map[string]interface{})
		if (ws.Teamviewer == nil || *ws.Teamviewer == "") && agentTV != "" {
			updates["teamviewer"] = agentTV
		} else if agentTV != "" && *ws.Teamviewer != agentTV {
			comment := fmt.Sprintf("Конфликт Teamviewer ID для РС '%s' (%s). В базе: %s, от агента: %s.",
				utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID, *ws.Teamviewer, agentTV)
			p.createTaskIfNotExists(ctx, res, "data_conflict", "Workstation", *ws.ServiceDeskUUID, owner, data, comment)
		}
		if (ws.Litemanager == nil || *ws.Litemanager == "") && agentLM != "" {
			updates["litemanager"] = agentLM
		} else if agentLM != "" && *ws.Litemanager != agentLM {
			comment := fmt.Sprintf("Конфликт Litemanager ID для РС '%s' (%s). В базе: %s, от агента: %s.",
				utils.SafeStringDereference(ws.DeviceName), *ws.ServiceDeskUUID, *ws.Litemanager, agentLM)
			p.createTaskIfNotExists(ctx, res, "data_conflict", "Workstation", *ws.ServiceDeskUUID, owner, data, comment)
		}

		if len(updates) > 0 {
			res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "Workstation", EntityUUID: *ws.ServiceDeskUUID, Updates: updates})
		}

	} else if agentTV != "" || agentLM != "" {
		comment := fmt.Sprintf("Добавить новую рабочую станцию для владельца '%s'. TV: %s, LM: %s.", owner, agentTV, agentLM)
		entityID := agentTV
		if entityID == "" {
			entityID = agentLM
		}
		p.createTaskIfNotExists(ctx, res, "add_equipment", "Workstation", entityID, owner, data, comment)
	}
}

// processFiscalRegisterActions формирует план действий для ФР.
// processFiscalRegisterActions формирует план действий для ФР.
func (p *processingEngineImpl) processFiscalRegisterActions(ctx context.Context, res *ProcessingResult, owner string, fr *models.FiscalRegister, data *api.AgentDataDTO) {
	if fr != nil {
		// --- НОВЫЙ ФИЛЬТР: Проверка на актуальность при обновлении ---
		currentTime := utils.ParseAgentTime(data.CurrentTime)
		if currentTime != nil && fr.LastModifiedDate != nil && currentTime.Before(*fr.LastModifiedDate) {
			p.logger.Info("Обновление фискального регистратора пропущено: данные от агента старше, чем запись в БД",
				zap.String("fr_uuid", *fr.ServiceDeskUUID),
				zap.Time("agent_time", *currentTime),
				zap.Time("db_time", *fr.LastModifiedDate))
			return
		}

		if fr.Status != nil && *fr.Status == "locked" {
			p.logger.Debug("Создание задач для ФР пропущено: статус 'locked'", zap.String("uuid", *fr.ServiceDeskUUID))
			return
		}

		updates := map[string]interface{}{
			"model_kkt":      data.ModelName,
			"rn_kkt":         utils.NormalizeRNKKT(data.RNM),
			"fn_number":      data.FNSerial,
			"inn":            strings.TrimSpace(data.INN),
			"ffd":            utils.FormatFFDVersion(data.FFDVersion),
			"fn_expire_date": utils.ParseAgentTime(data.DateTimeEnd),
			"kkt_reg_date":   utils.ParseAgentTime(data.DateTimeReg),
		}

		updates["fr_downloader"] = data.BootVersion
		calculatedFRFirmware := utils.CalculateFRFirmware(data.Licenses)
		updates["fr_firmware"] = calculatedFRFirmware

		if data.InstalledDriver != "" {
			updates["driver_version"] = data.InstalledDriver
		}
		licensesJSON, err := json.Marshal(data.Licenses)
		if err == nil {
			updates["licenses"] = datatypes.JSON(licensesJSON)
		}

		res.Actions = append(res.Actions, Action{Type: ActionUpdate, EntityType: "FiscalRegister", EntityUUID: *fr.ServiceDeskUUID, Updates: updates})

	} else if data.SerialNumber != "" {
		comment := fmt.Sprintf("Добавить новый ФР (СН: %s) для владельца '%s'.", data.SerialNumber, owner)
		p.createTaskIfNotExists(ctx, res, "add_equipment", "FiscalRegister", data.SerialNumber, owner, data, comment)
	}
}

// createTaskIfNotExists - централизованный хелпер для создания задач с проверкой на дублирование.
func (p *processingEngineImpl) createTaskIfNotExists(ctx context.Context, res *ProcessingResult, taskType, entityType, entityUUID, etalonOwnerUUID string, data *api.AgentDataDTO, comment string) {
	existingTask, err := p.taskRepo.FindActiveTask(ctx, taskType, entityUUID)
	if err != nil {
		p.logger.Error("Ошибка при поиске существующей активной задачи",
			zap.String("taskType", taskType), zap.String("entityUUID", entityUUID), zap.Error(err))
	}

	if existingTask != nil {
		p.logger.Info("Активная задача уже существует, новая не создается",
			zap.String("taskType", taskType), zap.String("entityUUID", entityUUID), zap.Uint("existingTaskID", existingTask.ID))
		return
	}

	task := p.buildTask(taskType, entityType, entityUUID, etalonOwnerUUID, data, comment)
	res.Actions = append(res.Actions, Action{Type: ActionCreateTask, Task: task})
}

// buildTask - универсальный конструктор задач.
func (p *processingEngineImpl) buildTask(taskType, entityType, entityUUID, etalonOwnerUUID string, agentData *api.AgentDataDTO, comment string) *models.ReconciliationTask {
	detailsMap := make(map[string]interface{})
	detailsMap["agent_data"] = agentData
	if etalonOwnerUUID != "" {
		detailsMap["etalon_owner_uuid"] = etalonOwnerUUID
	}

	details, _ := json.Marshal(detailsMap)
	return &models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: entityType,
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    comment,
	}
}
