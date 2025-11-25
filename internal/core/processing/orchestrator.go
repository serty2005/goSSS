package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"fmt"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type contextKey string

const transactionKey contextKey = "tx"

// Orchestrator - центральный сервис для обработки бизнес-логики на основе событий.
// Он является "Исполнителем": получает события, делегирует сложную логику
// движку (ProcessingEngine) и выполняет полученный план действий в транзакциях.
type Orchestrator struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	engine          ProcessingEngine
}

// NewOrchestrator создает новый экземпляр Оркестратора.
func NewOrchestrator(
	logger logger.LoggerInterface, db *gorm.DB, bus eventbus.EventBus, sdClient external.ExternalSystemClient,
	companyRepo company.Repository, serverRepo server.Repository,
	workstationRepo workstation.Repository, frRepo fiscal.Repository,
	taskRepo repositories.TaskRepo, linkRepo repositories.LinkRepo, engine ProcessingEngine,
) *Orchestrator {
	return &Orchestrator{
		logger, db, bus, sdClient, companyRepo, serverRepo, workstationRepo,
		frRepo, taskRepo, linkRepo, engine,
	}
}

// Start запускает Оркестратор, подписывая его на необходимые события.
func (o *Orchestrator) Start(ctx context.Context) {
	o.logger.Info("Оркестратор запущен и подписан на события.")
	o.bus.Subscribe(events.ServiceDeskEntityUpdated, o.handleServiceDeskEntityUpdate)
	o.bus.Subscribe(events.ServiceDeskEntityDeleted, o.handleServiceDeskEntityDelete)
	o.bus.Subscribe(events.ContractsStatusRecalculated, o.handleContractsStatusRecalculated)
	o.bus.Subscribe(events.DuplicatesFound, o.handleDuplicatesFound)
	o.bus.Subscribe(events.AgentDataReceived, o.handleAgentDataReceived)
	o.bus.Subscribe(events.ServerPollingSucceeded, o.handleServerPollingSucceeded)
	o.bus.Subscribe(events.ServerPollingFailed, o.handleServerPollingFailed)
	o.bus.Subscribe(events.FiscalRegisterDiscrepancyFound, o.handleFiscalRegisterDiscrepancy)
}

// handleServiceDeskEntityUpdate обрабатывает обновление сущности из внешней системы.
func (o *Orchestrator) handleServiceDeskEntityUpdate(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskEntityPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityUpdated")
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)

	var isNewEntity bool
	var internalID string

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, transactionKey, tx)
		mapperCtx := &external.MapperContext{DB: tx, LinkRepo: o.linkRepo, Logger: log}

		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ServiceDeskUUID)
		if err != nil {
			return fmt.Errorf("ошибка поиска связи по внешнему ID: %w", err)
		}
		isNewEntity = link == nil

		var newEntityModel, currentEntity interface{}

		switch payload.EntityType {
		case "Company":
			newEntityModel, err = o.sdClient.Mapper().DataToCompany(txCtx, mapperCtx, payload.Data)
			if err == nil && !isNewEntity {
				currentEntity, _ = o.companyRepo.GetByIDUnscoped(txCtx, link.InternalID)
			}
		case "Server":
			newEntityModel, err = o.sdClient.Mapper().DataToServer(txCtx, mapperCtx, payload.Data)
			if err == nil && !isNewEntity {
				currentEntity, _ = o.serverRepo.GetByIDUnscoped(txCtx, link.InternalID)
			}
		case "Workstation":
			newEntityModel, err = o.sdClient.Mapper().DataToWorkstation(txCtx, mapperCtx, payload.Data)
			if err == nil && !isNewEntity {
				currentEntity, _ = o.workstationRepo.GetByIDUnscoped(txCtx, link.InternalID)
			}
		case "FiscalRegister":
			newEntityModel, err = o.sdClient.Mapper().DataToFiscalRegister(txCtx, mapperCtx, payload.Data)
			if err == nil && !isNewEntity {
				currentEntity, _ = o.frRepo.GetByIDUnscoped(txCtx, link.InternalID)
			}
		default:
			return fmt.Errorf("неизвестный тип сущности для обработки: %s", payload.EntityType)
		}

		if err != nil {
			log.Warn("Пропуск обработки сущности из-за ошибки маппинга", "error", err)
			return nil
		}

		// Делегируем логику принятия решений движку
		result, err := o.engine.ProcessServiceDeskUpdate(txCtx, isNewEntity, payload.EntityType, currentEntity, newEntityModel)
		if err != nil {
			return fmt.Errorf("ошибка в движке обработки: %w", err)
		}

		// Исполняем план, полученный от движка
		for _, action := range result.Actions {
			switch action.Type {
			case ActionCreate:
				createdID, err := o.createEntity(txCtx, action.EntityToCreate)
				if err != nil {
					return err
				}
				internalID = createdID // Сохраняем ID для создания связи
			case ActionUpdate:
				if err := o.performUpdate(txCtx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					return err
				}
				internalID = action.EntityUUID
			}
		}

		// Если была создана новая сущность, создаем для нее связь
		if isNewEntity && internalID != "" {
			newLink := &models.ExternalSystemLink{
				InternalID: internalID, SystemName: "naumen", ServiceDeskUUID: payload.ServiceDeskUUID,
				EntityType: payload.EntityType, LastSyncedAt: time.Now(),
			}
			return o.linkRepo.Create(txCtx, tx, newLink)
		}

		return nil
	})

	if err != nil {
		log.Error("Ошибка в транзакции обработки обновления из SD", "error", err)
		return
	}

	if isNewEntity {
		log.Info("Новая сущность успешно создана.", "internalID", internalID)
	} else {
		log.Debug("Обработка существующей сущности завершена.")
	}
}

func (o *Orchestrator) handleServiceDeskEntityDelete(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskEntityDeletePayload)
	if !ok {
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "serviceDeskUUID", payload.ServiceDeskUUID)

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, transactionKey, tx)
		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ServiceDeskUUID)
		if err != nil {
			return err
		}
		if link == nil {
			log.Warn("Связь для удаляемой сущности не найдена, возможно, она уже удалена.")
			return nil
		}

		if err := o.performDelete(txCtx, tx, payload.EntityType, link.InternalID); err != nil {
			return err
		}
		return tx.Delete(link).Error
	})

	if err != nil {
		log.Error("Ошибка при 'мягком удалении' сущности", "error", err)
	} else {
		log.Info("Сущность и ее связь успешно 'мягко удалены'.")
	}
}

func (o *Orchestrator) handleContractsStatusRecalculated(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ContractsStatusPayload)
	if !ok {
		return
	}
	log := o.logger.With("event", event.Type)
	log.Info("Получено событие для обновления статусов контрактов у компаний", "count", len(payload.CompanyActiveContract))

	activeIDs := make([]string, 0)
	inactiveIDs := make([]string, 0)
	for id, isActive := range payload.CompanyActiveContract {
		if isActive {
			activeIDs = append(activeIDs, id)
		} else {
			inactiveIDs = append(inactiveIDs, id)
		}
	}

	err := o.db.Transaction(func(tx *gorm.DB) error {
		source := "contract_gateway"

		if len(activeIDs) > 0 {
			if res := tx.Model(&company.Company{}).Where("id IN ?", activeIDs).Updates(map[string]interface{}{"active_contract": true, "last_updated_by": source}); res.Error != nil {
				return res.Error
			}
		}
		if len(inactiveIDs) > 0 {
			if res := tx.Model(&company.Company{}).Where("id IN ?", inactiveIDs).Updates(map[string]interface{}{"active_contract": false, "last_updated_by": source}); res.Error != nil {
				return res.Error
			}
		}

		if len(inactiveIDs) > 0 {
			if err := o.lockEquipment(ctx, tx, inactiveIDs, log); err != nil {
				return err
			}
		}
		if len(activeIDs) > 0 {
			if err := o.unlockEquipment(ctx, tx, activeIDs, log); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов контрактов и оборудования", "error", err)
	} else {
		log.Info("Обновление статусов контрактов и оборудования успешно завершено.")
	}
}

func (o *Orchestrator) handleDuplicatesFound(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.DuplicatesFoundPayload)
	if !ok {
		return
	}
	log := o.logger.With("entityType", payload.EntityType, "field", payload.Field, "value", payload.Value)

	result := o.engine.ProcessDuplicates(ctx, payload)

	if len(result.Actions) == 0 {
		log.Debug("Движок не вернул действий для обработки дубликатов.")
		return
	}

	err := o.db.Transaction(func(tx *gorm.DB) error {
		for _, action := range result.Actions {
			if action.Type == ActionUpdate {
				if err := o.performUpdate(ctx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					log.Error("Не удалось обновить статус для сущности-дубликата", "internalID", action.EntityUUID, "error", err)
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов для дубликатов", "error", err)
	} else {
		log.Info("Статусы для группы дубликатов успешно обновлены.", "count", len(result.Actions))
	}
}

func (o *Orchestrator) handleAgentDataReceived(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.AgentDataPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события AgentDataReceived")
		return
	}

	log := o.logger.With("source", payload.Source)
	log.Debug("Оркестратор НАЧАЛ обработку события AgentDataReceived")

	result := o.engine.ProcessAgentData(ctx, payload.Source, &payload.Data)

	if len(result.Actions) == 0 {
		log.Debug("Движок не вернул никаких действий для выполнения.")
		return
	}

	err := o.db.Transaction(func(tx *gorm.DB) error {
		for _, action := range result.Actions {
			switch action.Type {
			case ActionCreateTask:
				if err := tx.Create(action.Task).Error; err != nil {
					log.Error("Ошибка создания задачи", "error", err)
					return err
				}
			case ActionUpdate:
				action.Updates["last_updated_by"] = "agent"
				if err := o.performUpdate(ctx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					log.Error("Ошибка обновления сущности", "error", err)
					return err
				}
			case ActionAddAdditionalOwner:
				server := &server.Server{Base: common.Base{ID: action.EntityUUID}}
				company := &company.Company{Base: common.Base{ID: action.AdditionalOwnerUUID}}
				if err := tx.Model(server).Association("AdditionalOwners").Append(company); err != nil {
					log.Error("Не удалось добавить дополнительного владельца", "serverID", server.ID, "companyID", company.ID, "error", err)
					return err
				}
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при выполнении плана действий от движка", "error", err)
	} else {
		log.Info("План действий от движка успешно выполнен.", "actions_count", len(result.Actions))
	}
}

func (o *Orchestrator) handleServerPollingSucceeded(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingSucceededPayload)
	if !ok {
		return
	}
	log := o.logger.With("request_id", payload.RequestID)
	updates := map[string]interface{}{
		"server_name":     payload.ServerName,
		"server_edition":  payload.ServerEdition,
		"server_version":  payload.ServerVersion,
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}
	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить данные сервера после успешного опроса", "error", err)
	} else {
		log.Info("Данные сервера успешно обновлены", "new_status", payload.NewStatus)
	}
}

func (o *Orchestrator) handleServerPollingFailed(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingFailedPayload)
	if !ok {
		return
	}
	log := o.logger.With("request_id", payload.RequestID)
	updates := map[string]interface{}{
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}
	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить статус сервера после неудачного опроса", "error", err)
	} else {
		log.Info("Статус сервера обновлен после неудачного опроса", "new_status", payload.NewStatus)
	}
}

// handleFiscalRegisterDiscrepancy обрабатывает событие о расхождении данных ФР.
func (o *Orchestrator) handleFiscalRegisterDiscrepancy(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.FiscalRegisterDiscrepancyPayload)
	if !ok {
		return
	}
	log := o.logger.With("fr_internal_uuid", payload.FRInternalUUID)

	existingTask, err := o.taskRepo.FindActiveTask(ctx, "need_update", payload.FRInternalUUID)
	if err != nil {
		log.Error("Ошибка при поиске существующей задачи 'need_update'", "error", err)
		return
	}
	if existingTask != nil {
		log.Debug("Активная задача 'need_update' для этого ФР уже существует, новая не создается.")
		return
	}

	var commentBuilder strings.Builder
	commentBuilder.WriteString(fmt.Sprintf("Обнаружено расхождение данных для ФР (внутр. ID: %s, внешн. ID: %s) между эталонной БД и ServiceDesk. Требуется обновить данные в ServiceDesk.\n\nРасхождения:\n", payload.FRInternalUUID, payload.FRServiceDeskUUID))
	for field, details := range payload.Discrepancies {
		commentBuilder.WriteString(fmt.Sprintf("- Поле '%s':\n  - Эталон: %v\n  - ServiceDesk: %v\n", field, details.EtalonValue, details.ServiceDeskValue))
	}

	detailsJSON, _ := json.Marshal(payload) // Сохраняем всю полезную нагрузку для контекста

	task := models.ReconciliationTask{
		TaskType:   "need_update",
		EntityType: "FiscalRegister",
		EntityUUID: payload.FRInternalUUID,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    commentBuilder.String(),
	}
	if err := o.db.WithContext(ctx).Create(&task).Error; err != nil {
		log.Error("Не удалось создать задачу 'need_update'", "error", err)
	} else {
		log.Info("Успешно создана задача 'need_update' на основе расхождений данных ФР.")
	}
}

// --- Вспомогательные функции-исполнители (ранее в orchestrator_helpers.go) ---

func (o *Orchestrator) createEntity(ctx context.Context, entity interface{}) (string, error) {
	tx := ctx.Value(transactionKey).(*gorm.DB)
	var id string
	var err error
	switch v := entity.(type) {
	case *company.Company:
		err = o.companyRepo.Create(ctx, v)
		id = v.ID
	case *server.Server:
		err = o.serverRepo.Create(ctx, tx, v)
		id = v.ID
	case *workstation.Workstation:
		err = o.workstationRepo.Create(ctx, tx, v)
		id = v.ID
	case *fiscal.FiscalRegister:
		err = o.frRepo.Create(ctx, tx, v)
		id = v.ID
	default:
		return "", fmt.Errorf("неподдерживаемый тип для создания: %T", entity)
	}
	return id, err
}

func (o *Orchestrator) performUpdate(ctx context.Context, tx *gorm.DB, entityType, internalID string, updates map[string]interface{}) error {
	var err error
	switch entityType {
	case "Company":
		_, err = o.companyRepo.Update(ctx, internalID, updates)
	case "Server":
		_, err = o.serverRepo.Update(ctx, tx, internalID, updates)
	case "Workstation":
		_, err = o.workstationRepo.Update(ctx, tx, internalID, updates)
	case "FiscalRegister":
		_, err = o.frRepo.Update(ctx, tx, internalID, updates)
	default:
		return fmt.Errorf("неподдерживаемый тип для обновления: %s", entityType)
	}
	return err
}

func (o *Orchestrator) performDelete(ctx context.Context, tx *gorm.DB, entityType, internalID string) error {
	var err error
	switch entityType {
	case "Company":
		_, err = o.companyRepo.Delete(ctx, internalID)
	case "Server":
		_, err = o.serverRepo.Delete(ctx, tx, internalID)
	case "Workstation":
		_, err = o.workstationRepo.Delete(ctx, tx, internalID)
	case "FiscalRegister":
		_, err = o.frRepo.Delete(ctx, tx, internalID)
	default:
		return fmt.Errorf("неподдерживаемый тип для удаления: %s", entityType)
	}
	return err
}

func (o *Orchestrator) lockEquipment(ctx context.Context, tx *gorm.DB, inactiveIDs []string, log logger.LoggerInterface) error {
	for _, model := range []interface{}{&server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status != ?", inactiveIDs, "locked").
			Updates(map[string]interface{}{"status_before_lock": gorm.Expr("status"), "status": "locked"})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Заморожено единиц оборудования", "count", res.RowsAffected)
		}
	}
	return nil
}

func (o *Orchestrator) unlockEquipment(ctx context.Context, tx *gorm.DB, activeIDs []string, log logger.LoggerInterface) error {
	for _, model := range []interface{}{&server.Server{}, &workstation.Workstation{}, &fiscal.FiscalRegister{}} {
		res := tx.WithContext(ctx).Model(model).Where("owner_id IN ? AND status = ? AND status_before_lock IS NOT NULL", activeIDs, "locked").
			Updates(map[string]interface{}{"status": gorm.Expr("status_before_lock"), "status_before_lock": nil})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			log.Info("Разморожено единиц оборудования", "count", res.RowsAffected)
		}
	}
	return nil
}

func getLMDFromModel(entity interface{}) *time.Time {
	switch v := entity.(type) {
	case *company.Company:
		return v.LastModifiedDate
	case *server.Server:
		return v.LastModifiedDate
	case *workstation.Workstation:
		return v.LastModifiedDate
	case *fiscal.FiscalRegister:
		return v.LastModifiedDate
	}
	return nil
}
