// Файл: internal/core/processing/reconciliation.go
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
	"fmt"
	"reflect"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Константы для типов сущностей для улучшения читаемости и поддерживаемости
const (
	EntityTypeServer         = "Server"
	EntityTypeWorkstation    = "Workstation"
	EntityTypeFiscalRegister = "FiscalRegister"
	EntityTypeAgent          = "Agent"
)

type ReconciliationEngine interface {
	DetermineOwner(ctx context.Context, data *events.AgentDataPayload) (string, error)
	AreCompaniesRelated(owner1, owner2 string) bool
	CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action
	CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action)
	GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error)
	CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error)
}

type reconciliationEngineImpl struct {
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	matcherSvc      services.EntityMatcherService
	logger          logger.LoggerInterface
}

func NewReconciliationEngine(
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
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

// CreateConflictTask создает задачу на конфликт (owner_mismatch, need_update и т.д.).
// Возвращает nil, если активная задача уже существует.
func (r *reconciliationEngineImpl) CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action {
	r.logger.Debug("CreateConflictTask вызвана", "conflictType", conflictType, "etalonOwnerID", etalonOwnerID)

	// 1. Определяем EntityUUID для поиска дубликатов
	var entityUUID string
	var entityType string

	if len(entities) > 0 {
		switch e := entities[0].(type) {
		case *server.Server:
			entityType = EntityTypeServer
			entityUUID = e.ID
		case *workstation.Workstation:
			entityType = EntityTypeWorkstation
			entityUUID = e.ID
		case *fiscal.FiscalRegister:
			entityType = EntityTypeFiscalRegister
			entityUUID = e.ID
		}
	} else if conflictType == "add_equipment" {
		// Для нового оборудования используем уникальные идентификаторы как временный UUID
		if data != nil && data.SerialNumber != "" {
			entityType = EntityTypeFiscalRegister
			entityUUID = data.SerialNumber
		} else if data != nil && (data.TeamviewerID != "" || data.LitemanagerID != "" || data.AnydeskID != "") {
			entityType = EntityTypeWorkstation
			// Склеиваем найденные ID удалённого доступа в порядке Teamviewer -> Litemanager -> Anydesk, пропуская пустые
			var ids []string
			if data.TeamviewerID != "" {
				ids = append(ids, data.TeamviewerID)
			}
			if data.LitemanagerID != "" {
				ids = append(ids, data.LitemanagerID)
			}
			if data.AnydeskID != "" {
				ids = append(ids, data.AnydeskID)
			}
			entityUUID = strings.Join(ids, "_")
		}
	} else if conflictType == "new_client" {
		entityType = EntityTypeAgent
		// Для new_client entityUUID формируем из склеенных ID данных нового клиента в порядке ServerURL -> SerialNumber -> TeamviewerID -> LitemanagerID -> AnydeskID, пропуская пустые
		if data != nil {
			var ids []string
			if data.URLRms != "" {
				ids = append(ids, data.URLRms)
			}
			if data.SerialNumber != "" {
				ids = append(ids, data.SerialNumber)
			}
			if data.TeamviewerID != "" {
				ids = append(ids, data.TeamviewerID)
			}
			if data.LitemanagerID != "" {
				ids = append(ids, data.LitemanagerID)
			}
			if data.AnydeskID != "" {
				ids = append(ids, data.AnydeskID)
			}
			entityUUID = strings.Join(ids, "_")
		}
		// Если ids пустой, entityUUID останется пустым, что приемлемо для new_client
	}

	// 2. ПРОВЕРКА: Есть ли уже активная задача?
	// Проверяем только если смогли определить идентификатор
	if entityUUID != "" {
		existingTask, err := r.taskRepo.FindActiveTask(ctx, conflictType, entityUUID)
		if err == nil && existingTask != nil {
			r.logger.Debug("Активная задача такого типа уже существует, пропускаем создание",
				"type", conflictType, "uuid", entityUUID)
			return nil // <--- Возвращаем nil, действие не требуется
		}
	}

	// 3. Формируем задачу, если дубликата нет
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
			// Для обратной совместимости с фронтом добавляем плоское поле
			if link != nil {
				detailsMap["etalon_owner_id"] = link.ServiceDeskUUID // Внешний ID
			} else {
				detailsMap["etalon_owner_id"] = etalonOwnerID
			}
		} else {
			detailsMap["etalon_owner_id"] = etalonOwnerID
		}
	}

	for i, entity := range entities {
		var eID, eType string
		switch e := entity.(type) {
		case *server.Server:
			eID, eType = e.ID, EntityTypeServer
		case *workstation.Workstation:
			eID, eType = e.ID, EntityTypeWorkstation
		case *fiscal.FiscalRegister:
			eID, eType = e.ID, EntityTypeFiscalRegister
		default:
			continue
		}
		enriched, err := r.GetEnrichmentDataForEntity(ctx, eType, eID)
		if err == nil {
			detailsMap[fmt.Sprintf("entity_%d", i)] = enriched
		}
	}

	details, _ := json.Marshal(detailsMap)
	task := &models.ReconciliationTask{
		TaskType:   conflictType,
		Status:     "new",
		Details:    datatypes.JSON(details),
		Comment:    fmt.Sprintf("Конфликт типа: %s", conflictType),
		EntityType: entityType,
		EntityUUID: entityUUID,
	}

	r.logger.Debug("CreateConflictTask сформировала новую задачу", "task_type", task.TaskType)
	return &Action{Type: ActionCreateTask, Task: task}
}

func (r *reconciliationEngineImpl) GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error) {
	var ownerID string
	var lmd *time.Time

	switch entityType {
	case EntityTypeServer:
		entity, err := r.serverRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти сервер с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case EntityTypeWorkstation:
		entity, err := r.workstationRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			return nil, fmt.Errorf("не удалось найти РС с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case EntityTypeFiscalRegister:
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

// CompareEntityData сравнивает данные агента с сущностью и определяет необходимость обновления,
// применяя правила "Доверенного обновления" (Trusted Update Rules).
func (r *reconciliationEngineImpl) CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action) {
	r.logger.Debug("Сравнение данных агента с сущностью (Trusted Rules)", "entity_type", entityType)
	updates := make(map[string]interface{})
	hasChanges := false
	var entityUUID string

	switch entityType {
	case EntityTypeServer:
		server, ok := entity.(*server.Server)
		if !ok {
			return false, nil
		}
		entityUUID = server.ID

		// ПРАВИЛО 1: Сервер (Server) - Read-only данные, кроме Owner и CRM ID.
		// Мы НЕ обновляем ServerName, Version, IP из агента, так как агент может запускаться в разных средах.

		// Обновляем CRM ID, только если он отсутствует
		if agentData["crm_id"] != nil && (server.CRMid == nil || *server.CRMid == "") {
			val := agentData["crm_id"].(string)
			if val != "" {
				updates["crm_id"] = val
				hasChanges = true
			}
		}

		// Владелец (Owner) обновляется через механизм owner_mismatch задач, здесь мы его не трогаем напрямую,
		// если только он не пустой. Но логика ProcessingEngine это обработает отдельно.

	case EntityTypeWorkstation:
		ws, ok := entity.(*workstation.Workstation)
		if !ok {
			return false, nil
		}
		entityUUID = ws.ID

		// ПРАВИЛО 2: Рабочая станция (WS) - Доверенное обновление специфичных полей.

		// Teamviewer
		if val, ok := agentData["teamviewer"].(string); ok && val != "" {
			if ws.Teamviewer == nil || *ws.Teamviewer != val {
				updates["teamviewer"] = val
				hasChanges = true
			}
		}

		// Litemanager
		if val, ok := agentData["litemanager"].(string); ok && val != "" {
			if ws.Litemanager == nil || *ws.Litemanager != val {
				updates["litemanager"] = val
				hasChanges = true
			}
		}

		// Hostname (DeviceName)
		if val, ok := agentData["hostname"].(string); ok && val != "" {
			if ws.DeviceName == nil || *ws.DeviceName != val {
				updates["device_name"] = val
				hasChanges = true
			}
		}

		// ВАЖНО: Anydesk НЕ обновляем (пока не починим агента), как указано в ТЗ.

	case EntityTypeFiscalRegister:
		fr, ok := entity.(*fiscal.FiscalRegister)
		if !ok {
			return false, nil
		}
		entityUUID = fr.ID

		// ПРАВИЛО 3: Фискальный регистратор (FR) - Полное доверие (Full Trust).
		// Обновляем все поля, пришедшие от агента.

		// Используем хелпер для сравнения и заполнения updates
		compareAndSet(updates, "rn_kkt", fr.RNKKT, agentData["RNM"])
		compareAndSet(updates, "inn", fr.INN, agentData["INN"])
		compareAndSet(updates, "legal_name", fr.LegalName, agentData["organizationName"])
		compareAndSet(updates, "model_kkt", fr.ModelKKT, agentData["modelName"])
		compareAndSet(updates, "fr_downloader", fr.FRDownloader, agentData["fr_downloader"])
		compareAndSet(updates, "driver_version", fr.DriverVersion, agentData["driver_version"])
		compareAndSet(updates, "fn_number", fr.FNNumber, agentData["fn_number"])
		compareAndSet(updates, "address", fr.Address, agentData["address"])
		compareAndSet(updates, "ofd_name", fr.OFDName, agentData["ofd_name"])

		// Сложные типы (Даты, JSON, Bool)
		if checkDateChanged(fr.FNExpireDate, agentData["dateTime_end"]) {
			updates["fn_expire_date"] = utils.ParseAgentTime(agentData["dateTime_end"].(string))
			hasChanges = true
		}
		if checkDateChanged(fr.KKTRegDate, agentData["kkt_reg_date"]) {
			updates["kkt_reg_date"] = utils.ParseAgentTime(agentData["kkt_reg_date"].(string))
			hasChanges = true
		}

		// Licenses (JSON)
		if agentData["licenses"] != nil {
			jsonBytes, _ := json.Marshal(agentData["licenses"])
			if string(fr.Licenses) != string(jsonBytes) {
				updates["licenses"] = datatypes.JSON(jsonBytes)
				hasChanges = true
			}
		}

		// Attribute Excise (Bool/String conversion handled inside agentData parsing mostly, but checking here)
		if agentData["attribute_excise"] != nil {
			var newVal *bool
			if strVal, ok := agentData["attribute_excise"].(*string); ok && strVal != nil {
				b := (*strVal == "true" || *strVal == "1")
				newVal = &b
			}

			if newVal != nil && (fr.AttributeExcise == nil || *fr.AttributeExcise != *newVal) {
				updates["attribute_excise"] = newVal
				hasChanges = true
			}
		}

		// Attribute Marked
		if agentData["attribute_marked"] != nil {
			var newVal *bool
			if strVal, ok := agentData["attribute_marked"].(*string); ok && strVal != nil {
				b := (*strVal == "true" || *strVal == "1")
				newVal = &b
			}

			if newVal != nil && (fr.AttributeMarked == nil || *fr.AttributeMarked != *newVal) {
				updates["attribute_marked"] = newVal
				hasChanges = true
			}
		}

		// Проверка hasChanges для строковых полей, если они были добавлены через compareAndSet
		if len(updates) > 0 {
			hasChanges = true
		}
	}

	if hasChanges {
		updates["last_modified_date"] = time.Now()
		updates["last_updated_by"] = "agent"

		return true, &Action{
			Type:       ActionUpdate,
			EntityType: entityType,
			EntityUUID: entityUUID,
			Updates:    updates,
		}
	}

	return false, nil
}

// CompareModelsForUpdate сравнивает две модели одного типа и возвращает map для обновления.
func (r *reconciliationEngineImpl) CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error) {
	switch entityType {
	case "Company":
		c, okC := current.(*company.Company)
		n, okN := new.(*company.Company)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Company")
		}
		return getCompanyDiff(c, n), nil
	case EntityTypeServer:
		c, okC := current.(*server.Server)
		n, okN := new.(*server.Server)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Server")
		}
		return getServerDiff(c, n), nil
	case EntityTypeWorkstation:
		c, okC := current.(*workstation.Workstation)
		n, okN := new.(*workstation.Workstation)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения Workstation")
		}
		return getWorkstationDiff(c, n), nil
	case EntityTypeFiscalRegister:
		c, okC := current.(*fiscal.FiscalRegister)
		n, okN := new.(*fiscal.FiscalRegister)
		if !okC || !okN {
			return nil, fmt.Errorf("неверные типы для сравнения FiscalRegister")
		}
		return getFiscalRegisterDiff(c, n), nil
	}
	return nil, fmt.Errorf("неподдерживаемый тип для сравнения: %s", entityType)
}

// Вспомогательные функции для сравнения полей

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

func getCompanyDiff(current *company.Company, new *company.Company) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "title", current.Title, new.Title)
	compareAndLog(updates, "address", current.Address, new.Address)
	compareAndLog(updates, "additional_name", current.AdditionalName, new.AdditionalName)
	compareAndLog(updates, "parent_id", current.ParentID, new.ParentID)

	// Удалено сравнение MetaClass

	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getServerDiff(current *server.Server, new *server.Server) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	compareAndLog(updates, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, "server_version", current.ServerVersion, new.ServerVersion)

	// Удалено сравнение MetaClass

	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getWorkstationDiff(current *workstation.Workstation, new *workstation.Workstation) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	// Удалено сравнение MetaClass
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func getFiscalRegisterDiff(current *fiscal.FiscalRegister, new *fiscal.FiscalRegister) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	// Удалено сравнение MetaClass
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

func compareAndSet(updates map[string]interface{}, key string, currentPtr *string, newVal interface{}) {
	if newVal == nil {
		return
	}
	strVal, ok := newVal.(string)
	if !ok || strVal == "" {
		return
	}
	if currentPtr == nil || *currentPtr != strVal {
		updates[key] = strVal
	}
}

func checkDateChanged(current *time.Time, newDateInterface interface{}) bool {
	if newDateInterface == nil {
		return false
	}
	dateStr, ok := newDateInterface.(string)
	if !ok || dateStr == "" {
		return false
	}
	parsed := utils.ParseAgentTime(dateStr)
	if parsed == nil {
		return false
	}
	if current == nil || !current.Equal(*parsed) {
		return true
	}
	return false
}
