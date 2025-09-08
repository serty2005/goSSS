// internal/processing/orchestrator.go
package processing

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/external"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/pkg/eventbus"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Orchestrator - центральный сервис для обработки бизнес-логики на основе событий.
type Orchestrator struct {
	logger          *zap.Logger
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	engine          ProcessingEngine
}

// NewOrchestrator создает новый экземпляр Оркестратора.
func NewOrchestrator(
	logger *zap.Logger, db *gorm.DB, bus eventbus.EventBus, sdClient external.ExternalSystemClient,
	companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo,
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
	log := o.logger.With(zap.String("entityType", payload.EntityType), zap.String("externalUUID", payload.ExternalUUID))

	var updates map[string]interface{}
	var diffLog []zap.Field
	var isNewEntity bool
	var internalID string
	var currentEntity, newEntityModel interface{}

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, "tx", tx)
		mapperCtx := &external.MapperContext{DB: tx, LinkRepo: o.linkRepo, Logger: log}

		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ExternalUUID)
		if err != nil {
			return fmt.Errorf("ошибка поиска связи по внешнему ID: %w", err)
		}

		isNewEntity = link == nil

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
			log.Warn("Пропуск обработки сущности из-за ошибки маппинга", zap.Error(err))
			return nil
		}

		if isNewEntity {
			internalID, err = o.createEntity(txCtx, payload.EntityType, newEntityModel)
			if err != nil {
				return err
			}

			newLink := &models.ExternalSystemLink{
				InternalID: internalID, SystemName: "naumen", ExternalID: payload.ExternalUUID,
				EntityType: payload.EntityType, LastSyncedAt: time.Now(),
			}
			return o.linkRepo.Create(txCtx, tx, newLink)
		}

		internalID = link.InternalID
		switch payload.EntityType {
		case "Company":
			updates, diffLog = getCompanyDiff(currentEntity.(*models.Company), newEntityModel.(*models.Company))
		case "Server":
			updates, diffLog = getServerDiff(currentEntity.(*models.Server), newEntityModel.(*models.Server))
		case "Workstation":
			updates, diffLog = getWorkstationDiff(currentEntity.(*models.Workstation), newEntityModel.(*models.Workstation))
		case "FiscalRegister":
			updates, diffLog = getFiscalRegisterDiff(currentEntity.(*models.FiscalRegister), newEntityModel.(*models.FiscalRegister))
		}

		if newLMD := getLMDFromModel(newEntityModel); newLMD != nil {
			if updates == nil {
				updates = make(map[string]interface{})
			}
			updates["last_modified_date"] = newLMD
		}

		if len(updates) > 0 {
			updates["last_updated_by"] = "naumen_gateway"
			return o.performUpdate(txCtx, tx, payload.EntityType, internalID, updates)
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка в транзакции обработки обновления из SD", zap.Error(err))
		return
	}

	if !isNewEntity && len(diffLog) > 0 {
		log.Warn("Обнаружено критическое расхождение данных. Создание/обновление задачи.", diffLog...)
	}

	if isNewEntity {
		log.Info("Новая сущность успешно создана.", zap.String("internalID", internalID))
	} else if len(updates) == 0 {
		log.Debug("Изменений не найдено, обновление не требуется.")
	} else {
		log.Info("Сущность успешно обновлена.", zap.Any("updates", updates), zap.String("internalID", internalID))
	}
}

// handleServiceDeskEntityDelete обрабатывает удаление сущности.
func (o *Orchestrator) handleServiceDeskEntityDelete(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskEntityDeletePayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("entityType", payload.EntityType), zap.String("externalUUID", payload.ExternalUUID))

	err := o.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, "tx", tx)
		link, err := o.linkRepo.GetByExternalID(txCtx, tx, "naumen", payload.ExternalUUID)
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
		log.Error("Ошибка при 'мягком удалении' сущности", zap.Error(err))
	} else {
		log.Info("Сущность и ее связь успешно 'мягко удалены'.")
	}
}

// handleContractsStatusRecalculated обрабатывает событие о пересчете статусов контрактов.
func (o *Orchestrator) handleContractsStatusRecalculated(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ContractsStatusPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("event", event.Type))
	log.Info("Получено событие для обновления статусов контрактов у компаний", zap.Int("count", len(payload.CompanyActiveContract)))

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
			if res := tx.Model(&models.Company{}).Where("id IN ?", activeIDs).Updates(map[string]interface{}{"active_contract": true, "last_updated_by": source}); res.Error != nil {
				return res.Error
			}
		}
		if len(inactiveIDs) > 0 {
			if res := tx.Model(&models.Company{}).Where("id IN ?", inactiveIDs).Updates(map[string]interface{}{"active_contract": false, "last_updated_by": source}); res.Error != nil {
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
		log.Error("Ошибка транзакции при обновлении статусов контрактов и оборудования", zap.Error(err))
	} else {
		log.Info("Обновление статусов контрактов и оборудования успешно завершено.")
	}
}

// handleDuplicatesFound создает или обновляет задачу на разрешение дубликатов.
func (o *Orchestrator) handleDuplicatesFound(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.DuplicatesFoundPayload)
	if !ok {
		return
	}
	log := o.logger.With(
		zap.String("entityType", payload.EntityType),
		zap.String("field", payload.Field),
		zap.String("value", payload.Value),
	)

	taskIdentifier := fmt.Sprintf("duplicate-%s-%s-%s", payload.EntityType, payload.Field, payload.Value)
	existingTask, err := o.taskRepo.FindActiveTask(ctx, "resolve_duplicate", taskIdentifier)
	if err != nil || existingTask != nil {
		if err != nil {
			log.Error("Ошибка проверки существующей задачи на дубликат", zap.Error(err))
		}
		return
	}

	detailsJSON, _ := json.Marshal(map[string]interface{}{"uuids": payload.InternalIDs})
	comment := fmt.Sprintf("Обнаружены дубликаты (%d шт.) по полю '%s'. Требуется выбрать эталонную запись.", len(payload.InternalIDs), payload.Field)
	task := models.ReconciliationTask{
		TaskType: "resolve_duplicate", EntityType: payload.EntityType, EntityUUID: taskIdentifier,
		Details: datatypes.JSON(detailsJSON), Status: "new", Comment: comment,
	}
	if err := o.db.WithContext(ctx).Create(&task).Error; err != nil {
		log.Error("Не удалось создать задачу на разрешение дубликатов", zap.Error(err))
	}
}

// handleServerPollingSucceeded обрабатывает успешный результат опроса сервера.
func (o *Orchestrator) handleServerPollingSucceeded(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingSucceededPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("internalServerUUID", payload.ServerUUID))
	updates := map[string]interface{}{
		"server_name":     payload.ServerName,
		"server_edition":  payload.ServerEdition,
		"server_version":  payload.ServerVersion,
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}
	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить данные сервера после успешного опроса", zap.Error(err))
	} else {
		log.Info("Данные сервера успешно обновлены", zap.String("new_status", payload.NewStatus))
	}
}

// handleServerPollingFailed обрабатывает неудачный результат опроса сервера.
func (o *Orchestrator) handleServerPollingFailed(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingFailedPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("internalServerUUID", payload.ServerUUID))
	updates := map[string]interface{}{
		"status":          payload.NewStatus,
		"last_polled_at":  payload.LastPolledAt,
		"last_updated_by": "rms_polling",
	}
	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить статус сервера после неудачного опроса", zap.Error(err))
	} else {
		log.Info("Статус сервера обновлен после неудачного опроса", zap.String("new_status", payload.NewStatus))
	}
}

// handleFiscalRegisterDiscrepancy обрабатывает событие о расхождении данных ФР.
func (o *Orchestrator) handleFiscalRegisterDiscrepancy(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.FiscalRegisterDiscrepancyPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("fr_external_uuid", payload.FRServiceDeskUUID))

	existingTask, err := o.taskRepo.FindActiveTask(ctx, "need_update", payload.FRServiceDeskUUID)
	if err != nil {
		log.Error("Ошибка при поиске существующей задачи 'need_update'", zap.Error(err))
		return
	}
	if existingTask != nil {
		log.Debug("Активная задача 'need_update' для этого ФР уже существует, новая не создается.")
		return
	}

	var commentBuilder strings.Builder
	commentBuilder.WriteString(fmt.Sprintf("Обнаружено расхождение данных для ФР (%s) между эталонной БД и ServiceDesk. Требуется обновить данные в ServiceDesk.\n\nРасхождения:\n", payload.FRServiceDeskUUID))
	for field, details := range payload.Discrepancies {
		commentBuilder.WriteString(fmt.Sprintf("- Поле '%s':\n  - Эталон: %v\n  - ServiceDesk: %v\n", field, details.EtalonValue, details.ServiceDeskValue))
	}

	detailsJSON, _ := json.Marshal(payload.Discrepancies)
	task := models.ReconciliationTask{
		TaskType: "need_update", EntityType: "FiscalRegister", EntityUUID: payload.FRServiceDeskUUID,
		Details: datatypes.JSON(detailsJSON), Status: "new", Comment: commentBuilder.String(),
	}
	if err := o.db.WithContext(ctx).Create(&task).Error; err != nil {
		log.Error("Не удалось создать задачу 'need_update'", zap.Error(err))
	} else {
		log.Info("Успешно создана задача 'need_update' на основе расхождений данных ФР.")
	}
}

// handleAgentDataReceived вызывает движок обработки и исполняет его план.
func (o *Orchestrator) handleAgentDataReceived(ctx context.Context, event eventbus.Event) {
	// ИЗМЕНЕНИЕ: Распаковываем новую полезную нагрузку.
	payload, ok := event.Payload.(events.AgentDataPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события AgentDataReceived")
		return
	}

	// ИЗМЕНЕНИЕ: Используем Source для логирования.
	log := o.logger.With(zap.String("source", payload.Source))
	log.Debug("Оркестратор НАЧАЛ обработку события AgentDataReceived")

	// ИЗМЕНЕНИЕ: Передаем source и data в движок.
	result := o.engine.ProcessAgentData(ctx, payload.Source, &payload.Data)

	if len(result.Actions) == 0 {
		log.Info("Движок не вернул никаких действий для выполнения.")
		return
	}

	err := o.db.Transaction(func(tx *gorm.DB) error {
		for _, action := range result.Actions {
			log.Debug("Выполнение действия из плана", zap.String("action", string(action.Type)), zap.String("entity", action.EntityType))
			switch action.Type {
			case ActionCreateTask:
				if err := tx.Create(action.Task).Error; err != nil {
					return err
				}
			case ActionUpdate:
				action.Updates["last_updated_by"] = "agent"
				if err := o.performUpdate(ctx, tx, action.EntityType, action.EntityUUID, action.Updates); err != nil {
					return err
				}
			case ActionAddAdditionalOwner:
				// TODO: Адаптировать под внутренние ID
			}
		}
		return nil
	})

	if err != nil {
		log.Error("Ошибка при выполнении плана действий от движка", zap.Error(err))
	} else {
		log.Info("План действий от движка успешно выполнен.", zap.Int("actions_count", len(result.Actions)))
	}
}
