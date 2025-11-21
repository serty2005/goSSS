// Файл: internal/core/processing/reconciliation.go
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
	"fmt"
	"reflect"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ReconciliationEngine отвечает за логику сверки данных агента с существующими сущностями,
// определение владельцев, проверку конфликтов и создание задач.
type ReconciliationEngine interface {
	// DetermineOwner определяет владельца на основе данных агента (сервер, РС, ФР).
	DetermineOwner(ctx context.Context, data *events.AgentDataPayload) (string, error)

	// AreCompaniesRelated проверяет, связаны ли компании (родительские связи).
	AreCompaniesRelated(owner1, owner2 string) bool

	// CreateConflictTask создает задачу на конфликт (owner_mismatch, need_update и т.д.).
	CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action

	// CompareEntityData сравнивает данные агента с сущностью и определяет необходимость обновления.
	CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action)

	// GetEnrichmentDataForEntity собирает полную информацию о сущности для записи в детали.
	GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error)

	// CompareModelsForUpdate сравнивает две модели одного типа и возвращает map для обновления.
	CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error)
}

// reconciliationEngineImpl реализация ReconciliationEngine.
type reconciliationEngineImpl struct {
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	matcherSvc      services.EntityMatcherService
	logger          logger.LoggerInterface
}

// NewReconciliationEngine создает новый экземпляр ReconciliationEngine.
func NewReconciliationEngine(
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
	taskRepo repositories.TaskRepo,
	linkRepo repositories.LinkRepo,
	matcherSvc services.EntityMatcherService,
	logger logger.LoggerInterface,
) ReconciliationEngine {
	return &reconciliationEngineImpl{
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
		taskRepo:        taskRepo,
		linkRepo:        linkRepo,
		matcherSvc:      matcherSvc,
		logger:          logger,
	}
}

func (r *reconciliationEngineImpl) DetermineOwner(ctx context.Context, data *events.AgentDataPayload) (string, error) {
	r.logger.Info("Определение владельца для данных из агента", "source", data.Source, "url_rms", data.Data.URLRms, "teamviewer", data.Data.TeamviewerID, "litemanager", data.Data.LitemanagerID, "anydesk", data.Data.AnydeskID, "serial", data.Data.SerialNumber)

	normalizedIP := validators.ValidateIPAddress(data.Data.URLRms)
	isLocal := normalizedIP == nil
	r.logger.Debug("Проверка локальности URL", "url", data.Data.URLRms, "normalized_ip", utils.SafeStringDereference(normalizedIP), "is_local", isLocal)

	if !isLocal {
		server, err := r.serverRepo.FindByCRMidOrIP(ctx, data.Data.CRMID, utils.SafeStringDereference(normalizedIP))
		r.logger.Debug("Поиск сервера по URL/IP для определения владельца", "crm_id", data.Data.CRMID, "ip", utils.SafeStringDereference(normalizedIP), "found", server != nil, "error", err)
		if err == nil && server != nil {
			if server.OwnerID != nil {
				r.logger.Debug("Владелец определен по серверу", "server_id", server.ID, "owner_id", *server.OwnerID)
				return *server.OwnerID, nil
			} else {
				r.logger.Warn("Сервер найден, но OwnerID равен nil", "server_id", server.ID)
			}
		}
	} else {
		r.logger.Debug("URL локальный, пропуск поиска сервера для определения владельца")
	}

	ws, err := r.workstationRepo.FindByRemoteIDs(ctx, data.Data.TeamviewerID, "", data.Data.LitemanagerID)
	r.logger.Debug("Поиск РС по TV/LM", "teamviewer", data.Data.TeamviewerID, "litemanager", data.Data.LitemanagerID, "found", ws != nil, "error", err)
	if err == nil && ws != nil {
		if ws.OwnerID != nil {
			r.logger.Debug("Владелец определен по РС (TV/LM)", "ws_id", ws.ID, "owner_id", *ws.OwnerID)
			return *ws.OwnerID, nil
		} else {
			r.logger.Warn("РС найдена по TV/LM, но OwnerID равен nil", "ws_id", ws.ID)
		}
	}

	if data.Data.AnydeskID != "" && data.Data.AnydeskID != "None" {
		ws, err = r.workstationRepo.FindByRemoteIDs(ctx, "", data.Data.AnydeskID, "")
		r.logger.Debug("Поиск РС по Anydesk", "anydesk", data.Data.AnydeskID, "found", ws != nil, "error", err)
		if err == nil && ws != nil {
			if ws.OwnerID != nil {
				r.logger.Debug("Владелец определен по РС (Anydesk)", "ws_id", ws.ID, "owner_id", *ws.OwnerID)
				return *ws.OwnerID, nil
			} else {
				r.logger.Warn("РС найдена по Anydesk, но OwnerID равен nil", "ws_id", ws.ID)
			}
		}
	}

	fr, err := r.frRepo.FindBySerialNumber(ctx, data.Data.SerialNumber)
	r.logger.Debug("Поиск ФР по serial", "serial", data.Data.SerialNumber, "found", fr != nil, "error", err)
	if err == nil && fr != nil {
		if fr.OwnerID != nil {
			r.logger.Debug("Владелец определен по ФР", "fr_id", fr.ID, "owner_id", *fr.OwnerID)
			return *fr.OwnerID, nil
		} else {
			r.logger.Warn("ФР найдена по serial, но OwnerID равен nil", "fr_id", fr.ID)
		}
	}

	r.logger.Info("Владелец не найден")
	return "", nil
}

func (r *reconciliationEngineImpl) AreCompaniesRelated(owner1, owner2 string) bool {
	r.logger.Debug("Проверка родства компаний", "owner1", owner1, "owner2", owner2)
	if owner1 == owner2 {
		r.logger.Debug("Компании идентичны, считаем связанными", "owner1", owner1, "owner2", owner2)
		return true
	}
	parents1, err1 := r.companyRepo.GetAllParentIDs(context.Background(), owner1)
	if err1 != nil {
		r.logger.Error("Ошибка получения родителей для owner1", "owner1", owner1, "error", err1)
		return false
	}
	r.logger.Debug("Родители owner1", "owner1", owner1, "parents1", parents1)
	parents2, err2 := r.companyRepo.GetAllParentIDs(context.Background(), owner2)
	if err2 != nil {
		r.logger.Error("Ошибка получения родителей для owner2", "owner2", owner2, "error", err2)
		return false
	}
	r.logger.Debug("Родители owner2", "owner2", owner2, "parents2", parents2)

	for _, p := range parents2 {
		if p == owner1 {
			r.logger.Debug("owner1 является родителем owner2", "parent", owner1, "child", owner2)
			return true
		}
	}

	for _, p := range parents1 {
		if p == owner2 {
			r.logger.Debug("owner2 является родителем owner1", "parent", owner2, "child", owner1)
			return true
		}
	}

	parents1Set := make(map[string]struct{})
	for _, p := range parents1 {
		parents1Set[p] = struct{}{}
	}
	for _, p := range parents2 {
		if _, ok := parents1Set[p]; ok {
			r.logger.Debug("Найдены общие родители", "common_parent", p, "owner1", owner1, "owner2", owner2)
			return true
		}
	}
	r.logger.Debug("Родственные связи не найдены", "owner1", owner1, "owner2", owner2, "parents1", parents1, "parents2", parents2)
	return false
}

func (r *reconciliationEngineImpl) CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action {
	r.logger.Debug("CreateConflictTask вызвана", "conflictType", conflictType, "etalonOwnerID", etalonOwnerID, "entities_count", len(entities))
	detailsMap := make(map[string]interface{})
	detailsMap["conflict_type"] = conflictType
	detailsMap["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	detailsMap["agent_data"] = data

	if etalonOwnerID != "" {
		etalonOwner, err := r.companyRepo.GetByID(ctx, etalonOwnerID)
		if err == nil && etalonOwner != nil {
			link, _ := r.linkRepo.GetByInternalID(ctx, nil, "naumen", etalonOwnerID)
			ownerInfo := map[string]interface{}{
				"internal_id": etalonOwnerID,
				"external_id": "",
				"title":       utils.SafeStringDereference(etalonOwner.Title),
			}
			if link != nil {
				ownerInfo["external_id"] = link.ServiceDeskUUID
			}
			detailsMap["etalon_owner"] = ownerInfo
		} else {
			detailsMap["etalon_owner_id"] = etalonOwnerID
		}
	}

	for i, entity := range entities {
		var entityID string
		var entityType string
		switch e := entity.(type) {
		case *models.Server:
			entityID = e.ID
			entityType = "Server"
		case *models.Workstation:
			entityID = e.ID
			entityType = "Workstation"
		case *models.FiscalRegister:
			entityID = e.ID
			entityType = "FiscalRegister"
		default:
			continue
		}
		enriched, err := r.GetEnrichmentDataForEntity(ctx, entityType, entityID)
		if err == nil {
			detailsMap[fmt.Sprintf("entity_%d", i)] = enriched
		}
	}

	details, _ := json.Marshal(detailsMap)
	task := &models.ReconciliationTask{
		TaskType: conflictType,
		Status:   "new",
		Details:  datatypes.JSON(details),
		Comment:  fmt.Sprintf("Конфликт типа: %s", conflictType),
	}

	if len(entities) > 0 {
		switch e := entities[0].(type) {
		case *models.Server:
			task.EntityType = "Server"
			task.EntityUUID = e.ID
		case *models.Workstation:
			task.EntityType = "Workstation"
			task.EntityUUID = e.ID
		case *models.FiscalRegister:
			task.EntityType = "FiscalRegister"
			task.EntityUUID = e.ID
		}
	} else if conflictType == "add_equipment" {
		// Логика определения типа сущности для новой задачи
		if data.SerialNumber != "" {
			task.EntityType = "FiscalRegister"
			task.EntityUUID = data.SerialNumber // Используем серийный номер как временный идентификатор
		} else if data.TeamviewerID != "" || data.LitemanagerID != "" || data.AnydeskID != "" {
			task.EntityType = "Workstation"
			// Для РС нет одного уникального идентификатора, поэтому оставляем UUID пустым
		}
	}

	r.logger.Debug("CreateConflictTask завершена", "task_type", task.TaskType, "entity_type", task.EntityType, "entity_uuid", task.EntityUUID)
	return &Action{Type: ActionCreateTask, Task: task}
}

func (r *reconciliationEngineImpl) GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error) {
	var ownerID string
	var lmd *time.Time

	switch entityType {
	case "Server":
		entity, err := r.serverRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти сервер с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case "Workstation":
		entity, err := r.workstationRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти РС с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case "FiscalRegister":
		entity, err := r.frRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти ФР с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	default:
		return nil, fmt.Errorf("неподдерживаемый тип сущности: %s", entityType)
	}

	link, _ := r.linkRepo.GetByInternalID(ctx, nil, "naumen", entityID)
	owner, _ := r.companyRepo.GetByID(ctx, ownerID)

	var externalID string
	if link != nil {
		externalID = link.ServiceDeskUUID
	}

	var ownerTitle string
	var ownerActiveContract bool
	if owner != nil {
		ownerTitle = utils.SafeStringDereference(owner.Title)
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

func (r *reconciliationEngineImpl) CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action) {
	r.logger.Debug("Сравнение данных агента с сущностью", "entity_type", entityType, "agent_data_keys", getMapKeys(agentData))
	updates := make(map[string]interface{})
	hasChanges := false

	switch entityType {
	case "Server":
		server, ok := entity.(*models.Server)
		if !ok {
			r.logger.Error("Некорректный тип сущности для Server")
			return false, nil
		}
		r.logger.Debug("Сравнение для сервера", "server_id", server.ID, "current_crm_id", utils.SafeStringDereference(server.CRMid))
		if agentData["crm_id"] != nil && (server.CRMid == nil || *server.CRMid == "") {
			updates["crm_id"] = agentData["crm_id"].(string)
			hasChanges = true
			r.logger.Info("Обновление crm_id для сервера", "server_id", server.ID, "new_crm_id", agentData["crm_id"])
		}
	case "FiscalRegister":
		fr, ok := entity.(*models.FiscalRegister)
		if !ok {
			r.logger.Error("Некорректный тип сущности для FiscalRegister")
			return false, nil
		}
		r.logger.Debug("Сравнение для ФР", "fr_id", fr.ID, "serial", utils.SafeStringDereference(fr.FRSerialNumber))

		changesDetected := false

		if agentData["dateTime_end"] != nil && agentData["dateTime_end"].(string) != "" {
			agentDateStr := agentData["dateTime_end"].(string)
			var parsed time.Time
			var err error
			formats := []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "02.01.2006", "02/01/2006"}
			for _, format := range formats {
				parsed, err = time.Parse(format, agentDateStr)
				if err == nil {
					break
				}
			}

			if err != nil {
				r.logger.Warn("Не удалось распарсить dateTime_end ни в одном формате", "value", agentDateStr, "error", err)
			} else if fr.FNExpireDate == nil || parsed != *fr.FNExpireDate {
				updates["fn_expire_date"] = parsed
				changesDetected = true
				r.logger.Debug("Обновление fn_expire_date для ФР", "fr_id", fr.ID, "new_date", agentDateStr, "parsed", parsed.Format("2006-01-02"))
			}
		}

		if agentData["licenses"] != nil {
			jsonBytes, err := json.Marshal(agentData["licenses"])
			if err != nil {
				r.logger.Error("Ошибка сериализации licenses", "error", err)
			} else if string(fr.Licenses) != string(jsonBytes) {
				updates["licenses"] = datatypes.JSON(jsonBytes)
				changesDetected = true
				r.logger.Debug("Обновление licenses для ФР", "fr_id", fr.ID)
			}
		}

		if agentData["RNM"] != nil && agentData["RNM"].(string) != "" && (fr.RNKKT == nil || *fr.RNKKT != agentData["RNM"].(string)) {
			updates["rn_kkt"] = agentData["RNM"].(string)
			changesDetected = true
			r.logger.Debug("Обновление rn_kkt для ФР", "fr_id", fr.ID, "new_value", agentData["RNM"])
		}

		if agentData["organizationName"] != nil && agentData["organizationName"].(string) != "" && (fr.LegalName == nil || *fr.LegalName != agentData["organizationName"].(string)) {
			updates["legal_name"] = agentData["organizationName"].(string)
			changesDetected = true
			r.logger.Debug("Обновление legal_name (organizationName) для ФР", "fr_id", fr.ID, "new_value", agentData["organizationName"])
		}

		if agentData["INN"] != nil && agentData["INN"].(string) != "" && (fr.INN == nil || *fr.INN != agentData["INN"].(string)) {
			updates["inn"] = agentData["INN"].(string)
			changesDetected = true
			r.logger.Debug("Обновление inn для ФР", "fr_id", fr.ID, "new_value", agentData["INN"])
		}

		if agentData["modelName"] != nil && agentData["modelName"].(string) != "" && (fr.ModelKKT == nil || *fr.ModelKKT != agentData["modelName"].(string)) {
			updates["model_kkt"] = agentData["modelName"].(string)
			changesDetected = true
			r.logger.Debug("Обновление model_kkt для ФР", "fr_id", fr.ID, "new_value", agentData["modelName"])
		}

		// Новые поля из данных агента

		// fr_downloader из bootVersion
		if agentData["fr_downloader"] != nil && agentData["fr_downloader"].(string) != "" && (fr.FRDownloader == nil || *fr.FRDownloader != agentData["fr_downloader"].(string)) {
			updates["fr_downloader"] = agentData["fr_downloader"].(string)
			changesDetected = true
			r.logger.Debug("Обновление fr_downloader для ФР", "fr_id", fr.ID, "new_value", agentData["fr_downloader"])
		}

		// kkt_reg_date из datetime_reg
		if agentData["kkt_reg_date"] != nil && agentData["kkt_reg_date"].(string) != "" {
			agentDateStr := agentData["kkt_reg_date"].(string)
			var parsed time.Time
			var err error
			formats := []string{"2006-01-02", "2006-01-02 15:04:05", "2006-01-02T15:04:05", "02.01.2006", "02/01/2006"}
			for _, format := range formats {
				parsed, err = time.Parse(format, agentDateStr)
				if err == nil {
					break
				}
			}

			if err != nil {
				r.logger.Warn("Не удалось распарсить kkt_reg_date ни в одном формате", "value", agentDateStr, "error", err)
			} else if fr.KKTRegDate == nil || parsed != *fr.KKTRegDate {
				updates["kkt_reg_date"] = parsed
				changesDetected = true
				r.logger.Debug("Обновление kkt_reg_date для ФР", "fr_id", fr.ID, "new_date", agentDateStr, "parsed", parsed.Format("2006-01-02"))
			}
		}

		// driver_version из installed_driver
		if agentData["driver_version"] != nil && agentData["driver_version"].(string) != "" && (fr.DriverVersion == nil || *fr.DriverVersion != agentData["driver_version"].(string)) {
			updates["driver_version"] = agentData["driver_version"].(string)
			changesDetected = true
			r.logger.Debug("Обновление driver_version для ФР", "fr_id", fr.ID, "new_value", agentData["driver_version"])
		}

		// fn_number из fn_serial
		if agentData["fn_number"] != nil && agentData["fn_number"].(string) != "" && (fr.FNNumber == nil || *fr.FNNumber != agentData["fn_number"].(string)) {
			updates["fn_number"] = agentData["fn_number"].(string)
			changesDetected = true
			r.logger.Debug("Обновление fn_number для ФР", "fr_id", fr.ID, "new_value", agentData["fn_number"])
		}

		// address - адрес фискального регистратора
		if agentData["address"] != nil && agentData["address"].(string) != "" && (fr.Address == nil || *fr.Address != agentData["address"].(string)) {
			updates["address"] = agentData["address"].(string)
			changesDetected = true
			r.logger.Debug("Обновление address для ФР", "fr_id", fr.ID, "new_value", agentData["address"])
		}

		// attribute_excise - признак работы с акцизными товарами
		if agentData["attribute_excise"] != nil {
			var newValue *bool
			if val, ok := agentData["attribute_excise"].(*string); ok && val != nil {
				// Пришло как строка, парсим в boolean
				switch *val {
				case "True", "true", "1":
					excise := true
					newValue = &excise
				case "False", "false", "0":
					excise := false
					newValue = &excise
				}
			} else if val, ok := agentData["attribute_excise"].(*bool); ok && val != nil {
				// Пришло как *bool (legacy)
				newValue = val
			} else if val, ok := agentData["attribute_excise"].(bool); ok {
				// Пришло как bool (legacy)
				newValue = &val
			}

			if newValue != nil && (fr.AttributeExcise == nil || *fr.AttributeExcise != *newValue) {
				updates["attribute_excise"] = newValue
				changesDetected = true
				r.logger.Debug("Обновление attribute_excise для ФР", "fr_id", fr.ID, "new_value", newValue)
			}
		}

		// attribute_marked - признак работы с маркированными товарами
		if agentData["attribute_marked"] != nil {
			var newValue *bool
			if val, ok := agentData["attribute_marked"].(*string); ok && val != nil {
				// Пришло как строка, парсим в boolean
				switch *val {
				case "True", "true", "1":
					marked := true
					newValue = &marked
				case "False", "false", "0":
					marked := false
					newValue = &marked
				}
			} else if val, ok := agentData["attribute_marked"].(*bool); ok && val != nil {
				// Пришло как *bool (legacy)
				newValue = val
			} else if val, ok := agentData["attribute_marked"].(bool); ok {
				// Пришло как bool (legacy)
				newValue = &val
			}

			if newValue != nil && (fr.AttributeMarked == nil || *fr.AttributeMarked != *newValue) {
				updates["attribute_marked"] = newValue
				changesDetected = true
				r.logger.Debug("Обновление attribute_marked для ФР", "fr_id", fr.ID, "new_value", newValue)
			}
		}

		// ofd_name - название оператора фискальных данных
		if agentData["ofd_name"] != nil && agentData["ofd_name"].(string) != "" && (fr.OFDName == nil || *fr.OFDName != agentData["ofd_name"].(string)) {
			updates["ofd_name"] = agentData["ofd_name"].(string)
			changesDetected = true
			r.logger.Debug("Обновление ofd_name для ФР", "fr_id", fr.ID, "new_value", agentData["ofd_name"])
		}

		if changesDetected {
			hasChanges = true
		}
	case "Workstation":
		ws, ok := entity.(*models.Workstation)
		if !ok {
			r.logger.Error("Некорректный тип сущности для Workstation")
			return false, nil
		}
		r.logger.Debug("Сравнение для РС", "ws_id", ws.ID, "current_teamviewer", utils.SafeStringDereference(ws.Teamviewer), "current_litemanager", utils.SafeStringDereference(ws.Litemanager))
		if agentData["teamviewer"] != nil && agentData["teamviewer"].(string) != "" && (ws.Teamviewer == nil || *ws.Teamviewer != agentData["teamviewer"].(string)) {
			updates["teamviewer"] = agentData["teamviewer"].(string)
			hasChanges = true
			r.logger.Info("Обновление teamviewer для РС", "ws_id", ws.ID, "new_value", agentData["teamviewer"])
		}
		if agentData["litemanager"] != nil && agentData["litemanager"].(string) != "" && (ws.Litemanager == nil || *ws.Litemanager != agentData["litemanager"].(string)) {
			updates["litemanager"] = agentData["litemanager"].(string)
			hasChanges = true
			r.logger.Info("Обновление litemanager для РС", "ws_id", ws.ID, "new_value", agentData["litemanager"])
		}
	}

	if hasChanges {
		updates["last_modified_date"] = time.Now()
		r.logger.Info("Найдены изменения для сущности", "entity_type", entityType, "updates", updates)
		var entityUUID string
		switch entityType {
		case "Server":
			if server, ok := entity.(*models.Server); ok {
				entityUUID = server.ID
			}
		case "Workstation":
			if ws, ok := entity.(*models.Workstation); ok {
				entityUUID = ws.ID
			}
		case "FiscalRegister":
			if fr, ok := entity.(*models.FiscalRegister); ok {
				entityUUID = fr.ID
			}
		}
		r.logger.Info("Создание действия обновления", "entity_type", entityType, "entity_uuid", entityUUID)
		return true, &Action{
			Type:       ActionUpdate,
			EntityType: entityType,
			EntityUUID: entityUUID,
			Updates:    updates,
		}
	}
	r.logger.Debug("Изменений не найдено", "entity_type", entityType)
	return false, nil
}

func (r *reconciliationEngineImpl) CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error) {
	switch entityType {
	case "Company":
		c, okC := current.(*models.Company)
		n, okN := new.(*models.Company)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Company")
		}
		return getCompanyDiff(c, n), nil
	case "Server":
		c, okC := current.(*models.Server)
		n, okN := new.(*models.Server)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Server")
		}
		return getServerDiff(c, n), nil
	case "Workstation":
		c, okC := current.(*models.Workstation)
		n, okN := new.(*models.Workstation)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Workstation")
		}
		return getWorkstationDiff(c, n), nil
	case "FiscalRegister":
		c, okC := current.(*models.FiscalRegister)
		n, okN := new.(*models.FiscalRegister)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения FiscalRegister")
		}
		return getFiscalRegisterDiff(c, n), nil
	}
	return nil, fmt.Errorf("неподдерживаемый тип для сравнения: %s", entityType)
}

func compareAndLog[T comparable](updates map[string]interface{}, key string, current, new *T) {
	isCurrentNil := current == nil || reflect.ValueOf(current).IsNil()
	isNewNil := new == nil || reflect.ValueOf(new).IsNil()
	if isCurrentNil && isNewNil {
		return
	}
	if isCurrentNil != isNewNil || *current != *new {
		updates[key] = new
	}
}

func getCompanyDiff(current *models.Company, new *models.Company) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "title", current.Title, new.Title)
	compareAndLog(updates, "address", current.Address, new.Address)
	compareAndLog(updates, "additional_name", current.AdditionalName, new.AdditionalName)
	compareAndLog(updates, "parent_id", current.ParentID, new.ParentID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getServerDiff(current *models.Server, new *models.Server) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	compareAndLog(updates, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, "server_version", current.ServerVersion, new.ServerVersion)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getWorkstationDiff(current *models.Workstation, new *models.Workstation) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
