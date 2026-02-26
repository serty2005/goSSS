// Файл: internal/core/processing/reconciliation.go
//
// Модуль сверки/согласования данных (Reconciliation Engine)
//
// Назначение:
//   - Сравнение данных из разных источников (агенты, внешние системы)
//   - Выявление расхождений между текущим состоянием БД и входящими данными
//   - Определение владельцев сущностей по цепочке идентификаторов
//   - Создание задач на разрешение конфликтов (owner_mismatch, need_update и др.)
//   - Применение правил "Доверенного обновления" (Trusted Update Rules)
//
// Алгоритм работы:
//  1. Определение владельца (DetermineOwner) — поиск по серверу, РС, ФР
//  2. Проверка родства компаний (AreCompaniesRelated) — для разрешения конфликтов владения
//  3. Сравнение данных (CompareEntityData) — применение правил обновления
//  4. Создание задачи конфликта (CreateConflictTask) — при обнаружении расхождений
//
// Trusted Update Rules:
//   - Server: Read-only (только owner_id и crm_id при отсутствии)
//   - Workstation: Доверие для teamviewer, litemanager, hostname (при пустом)
//   - FiscalRegister: Полное доверие (Full Trust) — все поля обновляются
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

// ReconciliationEngine — интерфейс движка сверки данных.
//
// Методы:
//   - DetermineOwner: Определяет владельца по цепочке идентификаторов (сервер → РС → ФР)
//   - AreCompaniesRelated: Проверяет родственные связи между компаниями
//   - CreateConflictTask: Создаёт задачу на разрешение конфликта
//   - CompareEntityData: Сравнивает данные агента с сущностью по правилам Trusted Update
//   - GetEnrichmentDataForEntity: Возвращает обогащённые данные о сущности для задачи
//   - CompareModelsForUpdate: Сравнивает две модели и возвращает diff для обновления
type ReconciliationEngine interface {
	DetermineOwner(ctx context.Context, data *events.AgentDataPayload) (string, error)
	AreCompaniesRelated(owner1, owner2 string) bool
	CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action
	CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action)
	GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error)
	CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error)
}

// reconciliationEngineImpl — реализация движка сверки данных.
//
// Зависимости:
//   - companyRepo: Репозиторий компаний (для проверки родства и получения владельцев)
//   - serverRepo: Репозиторий серверов (для поиска по CRM ID и IP)
//   - workstationRepo: Репозиторий РС (для поиска по remote IDs)
//   - frRepo: Репозиторий ФР (для поиска по серийному номеру)
//   - taskRepo: Репозиторий задач (для проверки дубликатов конфликтов)
//   - linkRepo: Репозиторий связей (для получения external ID сущностей)
//   - matcherSvc: Сервис сопоставления сущностей
//   - logger: Логгер для дебаг-логирования
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

// NewReconciliationEngine — конструктор движка сверки.
//
// Параметры:
//   - companyRepo: Репозиторий компаний
//   - serverRepo: Репозиторий серверов
//   - workstationRepo: Репозиторий рабочих станций
//   - frRepo: Репозиторий фискальных регистраторов
//   - taskRepo: Репозиторий задач
//   - linkRepo: Репозиторий связей с внешними системами
//   - matcherSvc: Сервис сопоставления сущностей
//   - logger: Логгер
//
// Возвращает: экземпляр ReconciliationEngine
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
	logger.Debug("ReconciliationEngine инициализирован", "операция", "init")
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

// DetermineOwner — определяет владельца по цепочке идентификаторов.
//
// Алгоритм поиска владельца (по приоритету):
//  1. По серверу: поиск по CRM ID или IP-адресу (если URL не локальный)
//  2. По рабочей станции: поиск по TeamViewer и LiteManager ID
//  3. По рабочей станции: поиск по AnyDesk ID
//  4. По фискальному регистратору: поиск по серийному номеру
//
// Параметры:
//   - ctx: Контекст для отмены операций
//   - data: Данные от агента (AgentDataPayload)
//
// Возвращает:
//   - string: ID компании-владельца (пустая строка, если не найден)
//   - error: Ошибка при проблемах с БД (nil, если владелец просто не найден)
//
// Критерии принятия решений:
//   - Локальные IP-адреса (127.x, 10.x, 192.168.x, 172.16-31.x) пропускаются
//   - Владелец определяется по первой найденной сущности с непустым OwnerID
func (r *reconciliationEngineImpl) DetermineOwner(ctx context.Context, data *events.AgentDataPayload) (string, error) {
	r.logger.Info("Определение владельца для данных из агента", "source", data.Source, "url_rms", data.Data.URLRms, "teamviewer", data.Data.TeamviewerID, "litemanager", data.Data.LitemanagerID, "rustdesk", data.Data.RustdeskID, "anydesk", data.Data.AnydeskID, "serial", data.Data.SerialNumber)

	normalizedIP := validators.ValidateIPAddress(data.Data.URLRms)
	isLocal := normalizedIP == nil
	r.logger.Debug("Проверка локальности URL", "url", data.Data.URLRms, "normalized_ip", utils.SafeStringDereference(normalizedIP), "is_local", isLocal)

	// Шаг 1: Поиск по серверу (если URL не локальный)
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

	// Шаг 2: Поиск по РС (TeamViewer + LiteManager)
	ws, err := r.workstationRepo.FindByRemoteIDs(ctx, data.Data.TeamviewerID, "", data.Data.LitemanagerID, data.Data.RustdeskID)
	r.logger.Debug("Поиск РС по TV/LM/RD", "teamviewer", data.Data.TeamviewerID, "litemanager", data.Data.LitemanagerID, "rustdesk", data.Data.RustdeskID, "found", ws != nil, "error", err)
	if err == nil && ws != nil {
		if ws.OwnerID != nil {
			r.logger.Debug("Владелец определен по РС (TV/LM)", "ws_id", ws.ID, "owner_id", *ws.OwnerID)
			return *ws.OwnerID, nil
		} else {
			r.logger.Warn("РС найдена по TV/LM, но OwnerID равен nil", "ws_id", ws.ID)
		}
	}

	// Шаг 3: Поиск по РС (AnyDesk)
	if data.Data.AnydeskID != "" && data.Data.AnydeskID != "None" {
		ws, err = r.workstationRepo.FindByRemoteIDs(ctx, "", data.Data.AnydeskID, "", "")
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

	// Шаг 4: Поиск по ФР (серийный номер)
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

// AreCompaniesRelated — проверяет родственные связи между двумя компаниями.
//
// Алгоритм проверки:
//  1. Прямое совпадение: owner1 == owner2
//  2. owner1 является родителем owner2
//  3. owner2 является родителем owner1
//  4. Наличие общего родителя у обеих компаний
//
// Параметры:
//   - owner1: ID первой компании
//   - owner2: ID второй компании
//
// Возвращает:
//   - true: Компании связаны (родитель-потомок или общий родитель)
//   - false: Компании не связаны или произошла ошибка получения родителей
//
// Критерии принятия решений:
//   - При ошибке получения родителей возвращается false (безопасное поведение)
//   - Пустые цепочки родителей считаются корректными (компания без родителя)
func (r *reconciliationEngineImpl) AreCompaniesRelated(owner1, owner2 string) bool {
	r.logger.Debug("Проверка родства компаний", "owner1", owner1, "owner2", owner2)

	// Шаг 1: Прямое совпадение
	if owner1 == owner2 {
		r.logger.Debug("Компании идентичны, считаем связанными", "owner1", owner1, "owner2", owner2)
		return true
	}

	// Шаг 2: Получаем цепочки родителей для обеих компаний
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

	// Шаг 3: Проверяем, является ли owner1 родителем owner2
	for _, p := range parents2 {
		if p == owner1 {
			r.logger.Debug("owner1 является родителем owner2", "parent", owner1, "child", owner2)
			return true
		}
	}

	// Шаг 4: Проверяем, является ли owner2 родителем owner1
	for _, p := range parents1 {
		if p == owner2 {
			r.logger.Debug("owner2 является родителем owner1", "parent", owner2, "child", owner1)
			return true
		}
	}

	// Шаг 5: Проверяем наличие общего родителя
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

// CreateConflictTask — создаёт задачу на разрешение конфликта.
//
// Типы конфликтов:
//   - owner_mismatch: Несовпадение владельца в Etalon и данных от агента
//   - need_update: Обнаружены расхождения в данных сущности
//   - add_equipment: Обнаружено новое оборудование (нет в БД)
//   - new_client: Обнаружен новый клиент (сервер не найден)
//
// Алгоритм:
//  1. Определение EntityUUID и EntityType для идентификации конфликта
//  2. Проверка на дубликат: поиск активной задачи с таким же типом и UUID
//  3. Формирование details с информацией о владельце и сущностях
//  4. Создание ReconciliationTask со статусом "new"
//
// Параметры:
//   - ctx: Контекст для отмены операций
//   - conflictType: Тип конфликта (owner_mismatch, need_update, add_equipment, new_client)
//   - etalonOwnerID: ID владельца в Etalon (может быть пустым)
//   - data: Данные от агента (AgentDataDTO)
//   - entities: Сущности, связанные с конфликтом (Server, Workstation, FiscalRegister)
//
// Возвращает:
//   - *Action: Действие для создания задачи (ActionCreateTask)
//   - nil: Если активная задача уже существует (дубликат)
//
// Критерии принятия решений:
//   - Для существующих сущностей UUID = ID сущности
//   - Для нового оборудования UUID формируется из SerialNumber или remote IDs
//   - Для new_client UUID формируется из всех доступных идентификаторов
func (r *reconciliationEngineImpl) CreateConflictTask(ctx context.Context, conflictType string, etalonOwnerID string, data *api.AgentDataDTO, entities ...interface{}) *Action {
	r.logger.Debug("CreateConflictTask: начало формирования задачи",
		"conflict_type", conflictType,
		"etalon_owner_id", etalonOwnerID,
		"entities_count", len(entities))

	// Шаг 1: Определяем EntityUUID для поиска дубликатов
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
		r.logger.Debug("CreateConflictTask: определена сущность",
			"entity_type", entityType,
			"entity_uuid", entityUUID)
	} else if conflictType == "add_equipment" {
		// Для нового оборудования используем уникальные идентификаторы как временный UUID
		if data != nil && data.SerialNumber != "" {
			entityType = EntityTypeFiscalRegister
			entityUUID = data.SerialNumber
			r.logger.Debug("CreateConflictTask: новое оборудование (ФР)",
				"serial_number", data.SerialNumber)
		} else if data != nil && (data.TeamviewerID != "" || data.LitemanagerID != "" || data.RustdeskID != "" || data.AnydeskID != "") {
			entityType = EntityTypeWorkstation
			// Склеиваем найденные ID удалённого доступа в порядке Teamviewer -> Litemanager -> Anydesk, пропуская пустые
			var ids []string
			if data.TeamviewerID != "" {
				ids = append(ids, data.TeamviewerID)
			}
			if data.LitemanagerID != "" {
				ids = append(ids, data.LitemanagerID)
			}
			if data.RustdeskID != "" {
				ids = append(ids, data.RustdeskID)
			}
			if data.AnydeskID != "" {
				ids = append(ids, data.AnydeskID)
			}
			entityUUID = strings.Join(ids, "_")
			r.logger.Debug("CreateConflictTask: новое оборудование (РС)",
				"remote_ids", entityUUID)
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
			if data.RustdeskID != "" {
				ids = append(ids, data.RustdeskID)
			}
			if data.AnydeskID != "" {
				ids = append(ids, data.AnydeskID)
			}
			entityUUID = strings.Join(ids, "_")
			r.logger.Debug("CreateConflictTask: новый клиент",
				"composite_uuid", entityUUID)
		}
		// Если ids пустой, entityUUID останется пустым, что приемлемо для new_client
	}

	// Шаг 2: Проверка на дубликат активной задачи
	if entityUUID != "" {
		existingTask, err := r.taskRepo.FindActiveTask(ctx, conflictType, entityUUID)
		if err == nil && existingTask != nil {
			r.logger.Debug("CreateConflictTask: активная задача уже существует, пропуск создания",
				"conflict_type", conflictType,
				"entity_uuid", entityUUID,
				"existing_task_id", existingTask.ID)
			return nil
		}
		r.logger.Debug("CreateConflictTask: дубликатов не найдено",
			"conflict_type", conflictType,
			"entity_uuid", entityUUID)
	}

	// Шаг 3: Формируем details задачи
	detailsMap := make(map[string]interface{})
	detailsMap["conflict_type"] = conflictType
	detailsMap["timestamp"] = time.Now().UTC().Format(time.RFC3339)
	detailsMap["agent_data"] = data

	// Добавляем информацию о владельце
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
			r.logger.Debug("CreateConflictTask: добавлена информация о владельце",
				"owner_id", etalonOwnerID,
				"owner_title", ownerInfo["title"])
		} else {
			detailsMap["etalon_owner_id"] = etalonOwnerID
			r.logger.Debug("CreateConflictTask: владелец не найден в БД",
				"owner_id", etalonOwnerID)
		}
	}

	// Добавляем обогащённые данные о сущностях
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
			r.logger.Debug("CreateConflictTask: добавлены обогащённые данные сущности",
				"entity_index", i,
				"entity_type", eType,
				"entity_id", eID)
		}
	}

	// Шаг 4: Создаём задачу
	details, _ := json.Marshal(detailsMap)
	task := &models.ReconciliationTask{
		TaskType:   conflictType,
		Status:     "new",
		Details:    datatypes.JSON(details),
		Comment:    fmt.Sprintf("Конфликт типа: %s", conflictType),
		EntityType: entityType,
		EntityUUID: entityUUID,
	}

	r.logger.Info("CreateConflictTask: задача сформирована",
		"task_type", task.TaskType,
		"entity_type", entityType,
		"entity_uuid", entityUUID)

	return &Action{Type: ActionCreateTask, Task: task}
}

// GetEnrichmentDataForEntity — возвращает обогащённые данные о сущности для задачи конфликта.
//
// Алгоритм:
//  1. Получение сущности из соответствующего репозитория по типу
//  2. Извлечение owner_id и last_modified_date
//  3. Получение external_id через linkRepo (связь с Naumen)
//  4. Получение информации о владельце (title, active_contract)
//
// Параметры:
//   - ctx: Контекст для отмены операций
//   - entityType: Тип сущности (Server, Workstation, FiscalRegister)
//   - entityID: ID сущности
//
// Возвращает:
//   - map[string]interface{}: Обогащённые данные:
//   - internal_id: ID в Etalon
//   - external_id: ID в Naumen (если есть связь)
//   - last_modified_date: Дата последнего изменения
//   - owner_info: Информация о владельце (id, title, active_contract)
//   - error: Ошибка при отсутствии сущности или неподдерживаемом типе
func (r *reconciliationEngineImpl) GetEnrichmentDataForEntity(ctx context.Context, entityType string, entityID string) (map[string]interface{}, error) {
	r.logger.Debug("GetEnrichmentDataForEntity: получение обогащённых данных",
		"entity_type", entityType,
		"entity_id", entityID)

	var ownerID string
	var lmd *time.Time

	// Шаг 1: Получаем сущность по типу
	switch entityType {
	case EntityTypeServer:
		entity, err := r.serverRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			r.logger.Error("GetEnrichmentDataForEntity: сервер не найден",
				"entity_id", entityID,
				"error", err)
			return nil, fmt.Errorf("не удалось найти сервер с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case EntityTypeWorkstation:
		entity, err := r.workstationRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			r.logger.Error("GetEnrichmentDataForEntity: РС не найдена",
				"entity_id", entityID,
				"error", err)
			return nil, fmt.Errorf("не удалось найти РС с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	case EntityTypeFiscalRegister:
		entity, err := r.frRepo.GetByID(ctx, entityID)
		if err != nil || entity == nil {
			r.logger.Error("GetEnrichmentDataForEntity: ФР не найден",
				"entity_id", entityID,
				"error", err)
			return nil, fmt.Errorf("не удалось найти ФР с ID %s: %w", entityID, err)
		}
		ownerID = utils.SafeStringDereference(entity.OwnerID)
		lmd = entity.LastModifiedDate
	default:
		r.logger.Error("GetEnrichmentDataForEntity: неподдерживаемый тип сущности",
			"entity_type", entityType)
		return nil, fmt.Errorf("неподдерживаемый тип сущности: %s", entityType)
	}

	r.logger.Debug("GetEnrichmentDataForEntity: сущность найдена",
		"entity_type", entityType,
		"entity_id", entityID,
		"owner_id", ownerID)

	// Шаг 2: Получаем связь с внешней системой и информацию о владельце
	link, _ := r.linkRepo.GetByInternalID(ctx, nil, "naumen", entityID)
	owner, _ := r.companyRepo.GetByID(ctx, ownerID)

	var externalID string
	if link != nil {
		externalID = link.ServiceDeskUUID
		r.logger.Debug("GetEnrichmentDataForEntity: найдена связь с Naumen",
			"entity_id", entityID,
			"external_id", externalID)
	}

	var ownerTitle string
	var ownerActiveContract bool
	if owner != nil {
		ownerTitle = utils.SafeStringDereference(owner.Title)
		if owner.ActiveContract != nil {
			ownerActiveContract = *owner.ActiveContract
		}
		r.logger.Debug("GetEnrichmentDataForEntity: информация о владельце",
			"owner_id", ownerID,
			"owner_title", ownerTitle,
			"active_contract", ownerActiveContract)
	}

	result := map[string]interface{}{
		"internal_id":        entityID,
		"external_id":        externalID,
		"last_modified_date": lmd,
		"owner_info": map[string]interface{}{
			"id":              ownerID,
			"title":           ownerTitle,
			"active_contract": ownerActiveContract,
		},
	}

	r.logger.Debug("GetEnrichmentDataForEntity: обогащённые данные сформированы",
		"entity_type", entityType,
		"entity_id", entityID)

	return result, nil
}

// CompareEntityData — сравнивает данные агента с сущностью и определяет необходимость обновления.
//
// Применяет правила "Доверенного обновления" (Trusted Update Rules):
//   - Server: Read-only (только crm_id при отсутствии)
//   - Workstation: Доверие для teamviewer, litemanager, hostname (при пустом)
//   - FiscalRegister: Полное доверие (Full Trust) — все поля обновляются
//
// Алгоритм:
//  1. Определение типа сущности и извлечение текущих значений
//  2. Применение правил сравнения в зависимости от типа
//  3. Формирование map с полями для обновления
//  4. Добавление метаданных (last_modified_date, last_updated_by)
//
// Параметры:
//   - ctx: Контекст для отмены операций
//   - entityType: Тип сущности (Server, Workstation, FiscalRegister)
//   - agentData: Данные от агента (map[string]interface{})
//   - entity: Текущая сущность для сравнения
//
// Возвращает:
//   - bool: true, если есть изменения, false — если изменений нет
//   - *Action: Действие для обновления (ActionUpdate) или nil
//
// Критерии принятия решений:
//   - Server: crm_id обновляется только если он пустой в БД
//   - Workstation: teamviewer/litemanager обновляются при любом отличии, hostname — только если пустой
//   - FiscalRegister: Все поля обновляются при любом отличии (Full Trust)
func (r *reconciliationEngineImpl) CompareEntityData(ctx context.Context, entityType string, agentData map[string]interface{}, entity interface{}) (bool, *Action) {
	r.logger.Debug("CompareEntityData: начало сравнения данных",
		"entity_type", entityType)

	updates := make(map[string]interface{})
	hasChanges := false
	var entityUUID string

	switch entityType {
	case EntityTypeServer:
		server, ok := entity.(*server.Server)
		if !ok {
			r.logger.Warn("CompareEntityData: неверный тип сущности для Server")
			return false, nil
		}
		entityUUID = server.ID

		r.logger.Debug("CompareEntityData: сравнение Server (Read-only правила)",
			"server_id", server.ID,
			"current_crm_id", utils.SafeStringDereference(server.CRMid))

		// ПРАВИЛО 1: Сервер (Server) - Read-only данные, кроме Owner и CRM ID.
		// Мы НЕ обновляем ServerName, Version, IP из агента, так как агент может запускаться в разных средах.

		// Обновляем CRM ID, только если он отсутствует
		if agentData["crm_id"] != nil && (server.CRMid == nil || *server.CRMid == "") {
			val := agentData["crm_id"].(string)
			if val != "" {
				updates["crm_id"] = val
				hasChanges = true
				r.logger.Debug("CompareEntityData: Server — crm_id будет обновлён",
					"server_id", server.ID,
					"new_crm_id", val)
			}
		}

		// Владелец (Owner) обновляется через механизм owner_mismatch задач, здесь мы его не трогаем напрямую,
		// если только он не пустой. Но логика ProcessingEngine это обработает отдельно.

	case EntityTypeWorkstation:
		ws, ok := entity.(*workstation.Workstation)
		if !ok {
			r.logger.Warn("CompareEntityData: неверный тип сущности для Workstation")
			return false, nil
		}
		entityUUID = ws.ID

		r.logger.Debug("CompareEntityData: сравнение Workstation (частичное доверие)",
			"ws_id", ws.ID,
			"current_teamviewer", utils.SafeStringDereference(ws.Teamviewer),
			"current_litemanager", utils.SafeStringDereference(ws.Litemanager),
			"current_rustdesk", utils.SafeStringDereference(ws.Rustdesk),
			"current_device_name", utils.SafeStringDereference(ws.DeviceName))

		// ПРАВИЛО 2: Рабочая станция (WS) - Доверенное обновление специфичных полей.

		// Teamviewer
		if val, ok := agentData["teamviewer"].(string); ok && val != "" {
			if ws.Teamviewer == nil || *ws.Teamviewer != val {
				updates["teamviewer"] = val
				hasChanges = true
				r.logger.Debug("CompareEntityData: Workstation — teamviewer будет обновлён",
					"ws_id", ws.ID,
					"old_value", utils.SafeStringDereference(ws.Teamviewer),
					"new_value", val)
			}
		}

		// Litemanager
		if val, ok := agentData["litemanager"].(string); ok && val != "" {
			if ws.Litemanager == nil || *ws.Litemanager != val {
				updates["litemanager"] = val
				hasChanges = true
				r.logger.Debug("CompareEntityData: Workstation — litemanager будет обновлён",
					"ws_id", ws.ID,
					"old_value", utils.SafeStringDereference(ws.Litemanager),
					"new_value", val)
			}
		}

		// Rustdesk
		if val, ok := agentData["rustdesk"].(string); ok && val != "" {
			if ws.Rustdesk == nil || *ws.Rustdesk != val {
				updates["rustdesk"] = val
				hasChanges = true
				r.logger.Debug("CompareEntityData: Workstation — rustdesk будет обновлён",
					"ws_id", ws.ID,
					"old_value", utils.SafeStringDereference(ws.Rustdesk),
					"new_value", val)
			}
		}

		// Hostname (DeviceName) — только если пустой
		if val, ok := agentData["hostname"].(string); ok && val != "" {
			if ws.DeviceName == nil || *ws.DeviceName == "" {
				updates["device_name"] = val
				hasChanges = true
				r.logger.Debug("CompareEntityData: Workstation — device_name будет установлен",
					"ws_id", ws.ID,
					"new_value", val)
			}
		}

		// ВАЖНО: Anydesk НЕ обновляем (пока не починим агента), как указано в ТЗ.

	case EntityTypeFiscalRegister:
		fr, ok := entity.(*fiscal.FiscalRegister)
		if !ok {
			r.logger.Warn("CompareEntityData: неверный тип сущности для FiscalRegister")
			return false, nil
		}
		entityUUID = fr.ID

		r.logger.Debug("CompareEntityData: сравнение FiscalRegister (Full Trust)",
			"fr_id", fr.ID,
			"current_rn_kkt", utils.SafeStringDereference(fr.RNKKT),
			"current_inn", utils.SafeStringDereference(fr.INN))

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
		compareAndSet(updates, "fn_execution", fr.FNExecution, agentData["fn_execution"])
		compareAndSet(updates, "address", fr.Address, agentData["address"])
		compareAndSet(updates, "ofd_name", fr.OFDName, agentData["ofd_name"])

		// Сложные типы (Даты, JSON, Bool)
		if checkDateChanged(fr.FNExpireDate, agentData["dateTime_end"]) {
			updates["fn_expire_date"] = utils.ParseAgentTime(agentData["dateTime_end"].(string))
			hasChanges = true
			r.logger.Debug("CompareEntityData: FiscalRegister — fn_expire_date будет обновлён",
				"fr_id", fr.ID)
		}
		if checkDateChanged(fr.KKTRegDate, agentData["kkt_reg_date"]) {
			updates["kkt_reg_date"] = utils.ParseAgentTime(agentData["kkt_reg_date"].(string))
			hasChanges = true
			r.logger.Debug("CompareEntityData: FiscalRegister — kkt_reg_date будет обновлён",
				"fr_id", fr.ID)
		}

		// Licenses (JSON)
		if agentData["licenses"] != nil {
			jsonBytes, _ := json.Marshal(agentData["licenses"])
			if string(fr.Licenses) != string(jsonBytes) {
				updates["licenses"] = datatypes.JSON(jsonBytes)
				hasChanges = true
				r.logger.Debug("CompareEntityData: FiscalRegister — licenses будет обновлён",
					"fr_id", fr.ID)
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
				r.logger.Debug("CompareEntityData: FiscalRegister — attribute_excise будет обновлён",
					"fr_id", fr.ID,
					"new_value", *newVal)
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
				r.logger.Debug("CompareEntityData: FiscalRegister — attribute_marked будет обновлён",
					"fr_id", fr.ID,
					"new_value", *newVal)
			}
		}

		// Проверка hasChanges для строковых полей, если они были добавлены через compareAndSet
		if len(updates) > 0 {
			hasChanges = true
		}
	}

	// Формируем Action, если есть изменения
	if hasChanges {
		updates["last_modified_date"] = time.Now()
		updates["last_updated_by"] = "agent"

		r.logger.Info("CompareEntityData: обнаружены расхождения, требуется обновление",
			"entity_type", entityType,
			"entity_uuid", entityUUID,
			"updates_count", len(updates))

		return true, &Action{
			Type:       ActionUpdate,
			EntityType: entityType,
			EntityUUID: entityUUID,
			Updates:    updates,
		}
	}

	r.logger.Debug("CompareEntityData: расхождений не обнаружено",
		"entity_type", entityType,
		"entity_uuid", entityUUID)

	return false, nil
}

// CompareModelsForUpdate — сравнивает две модели одного типа и возвращает diff для обновления.
//
// Алгоритм:
//  1. Определение типа сущности
//  2. Делегирование сравнения соответствующей функции get*Diff
//  3. Возврат map с изменёнными полями
//
// Параметры:
//   - entityType: Тип сущности (Company, Server, Workstation, FiscalRegister)
//   - current: Текущая модель
//   - new: Новая модель для сравнения
//
// Возвращает:
//   - map[string]interface{}: Поля для обновления (ключ — имя поля, значение — новое значение)
//   - error: Ошибка при несоответствии типов или неподдерживаемом типе
//
// Критерии принятия решений:
//   - Сравнение производится по значению (не по указателю)
//   - nil и пустые значения считаются различными
//   - Если сущность была удалена (DeletedAt.Valid), добавляется сброс deleted_at
func (r *reconciliationEngineImpl) CompareModelsForUpdate(entityType string, current, new interface{}) (map[string]interface{}, error) {
	r.logger.Debug("CompareModelsForUpdate: сравнение моделей",
		"entity_type", entityType)

	switch entityType {
	case "Company":
		c, okC := current.(*company.Company)
		n, okN := new.(*company.Company)
		if !okC || !okN {
			r.logger.Error("CompareModelsForUpdate: неверные типы для Company")
			return nil, fmt.Errorf("неверные типы для сравнения Company")
		}
		diff := getCompanyDiff(c, n)
		r.logger.Debug("CompareModelsForUpdate: diff для Company",
			"updates_count", len(diff))
		return diff, nil
	case EntityTypeServer:
		c, okC := current.(*server.Server)
		n, okN := new.(*server.Server)
		if !okC || !okN {
			r.logger.Error("CompareModelsForUpdate: неверные типы для Server")
			return nil, fmt.Errorf("неверные типы для сравнения Server")
		}
		diff := getServerDiff(c, n)
		r.logger.Debug("CompareModelsForUpdate: diff для Server",
			"updates_count", len(diff))
		return diff, nil
	case EntityTypeWorkstation:
		c, okC := current.(*workstation.Workstation)
		n, okN := new.(*workstation.Workstation)
		if !okC || !okN {
			r.logger.Error("CompareModelsForUpdate: неверные типы для Workstation")
			return nil, fmt.Errorf("неверные типы для сравнения Workstation")
		}
		diff := getWorkstationDiff(c, n)
		r.logger.Debug("CompareModelsForUpdate: diff для Workstation",
			"updates_count", len(diff))
		return diff, nil
	case EntityTypeFiscalRegister:
		c, okC := current.(*fiscal.FiscalRegister)
		n, okN := new.(*fiscal.FiscalRegister)
		if !okC || !okN {
			r.logger.Error("CompareModelsForUpdate: неверные типы для FiscalRegister")
			return nil, fmt.Errorf("неверные типы для сравнения FiscalRegister")
		}
		diff := getFiscalRegisterDiff(c, n)
		r.logger.Debug("CompareModelsForUpdate: diff для FiscalRegister",
			"updates_count", len(diff))
		return diff, nil
	}

	r.logger.Error("CompareModelsForUpdate: неподдерживаемый тип",
		"entity_type", entityType)
	return nil, fmt.Errorf("неподдерживаемый тип для сравнения: %s", entityType)
}

// ============================================================================
// Вспомогательные функции для сравнения полей
// ============================================================================

// compareAndLog — обобщённая функция сравнения для указательных типов.
// Добавляет значение в updates, если текущее и новое значения различаются.
//
// Параметры:
//   - updates: Map для накопления изменений
//   - key: Имя поля
//   - current: Текущее значение (указатель)
//   - new: Новое значение (указатель)
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

// getCompanyDiff — возвращает diff для обновления компании.
//
// Сравниваемые поля:
//   - title: Название компании
//   - address: Адрес
//   - additional_name: Дополнительное название
//   - parent_id: ID родительской компании
//   - deleted_at: Сброс, если была удалена
func getCompanyDiff(current *company.Company, new *company.Company) map[string]interface{} {
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

// getServerDiff — возвращает diff для обновления сервера.
//
// Сравниваемые поля:
//   - owner_id: ID компании-владельца
//   - unique_id: Уникальный идентификатор
//   - rdp: RDP-доступ
//   - server_version: Версия сервера
//   - deleted_at: Сброс, если был удалён
func getServerDiff(current *server.Server, new *server.Server) map[string]interface{} {
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

// getWorkstationDiff — возвращает diff для обновления рабочей станции.
//
// Сравниваемые поля:
//   - owner_id: ID компании-владельца
//   - deleted_at: Сброс, если была удалена
func getWorkstationDiff(current *workstation.Workstation, new *workstation.Workstation) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

// getFiscalRegisterDiff — возвращает diff для обновления фискального регистратора.
//
// Сравниваемые поля:
//   - owner_id: ID компании-владельца
//   - deleted_at: Сброс, если был удалён
func getFiscalRegisterDiff(current *fiscal.FiscalRegister, new *fiscal.FiscalRegister) map[string]interface{} {
	updates := make(map[string]interface{})
	compareAndLog(updates, "owner_id", current.OwnerID, new.OwnerID)
	if current.DeletedAt.Valid {
		updates["deleted_at"] = gorm.Expr("NULL")
	}
	return updates
}

// compareAndSet — сравнивает строковое поле и добавляет в updates при отличии.
// Используется для сравнения полей ФР в режиме Full Trust.
//
// Параметры:
//   - updates: Map для накопления изменений
//   - key: Имя поля в БД
//   - currentPtr: Текущее значение (указатель, может быть nil)
//   - newVal: Новое значение из данных агента (interface{})
//
// Критерии:
//   - Пропускает nil и пустые значения из agentData
//   - Добавляет в updates, если текущее значение nil или отличается
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

// checkDateChanged — проверяет, изменилась ли дата.
// Используется для сравнения дат ФР (fn_expire_date, kkt_reg_date).
//
// Параметры:
//   - current: Текущее значение даты (указатель, может быть nil)
//   - newDateInterface: Новое значение из данных агента (строка)
//
// Возвращает:
//   - true: Дата изменилась или была добавлена
//   - false: Дата не изменилась или не может быть распарсена
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
