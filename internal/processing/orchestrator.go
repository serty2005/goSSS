package processing

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
	"fmt"
	"reflect"
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
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
	matcherSvc      services.EntityMatcherService
}

func NewOrchestrator(logger *zap.Logger, db *gorm.DB, bus eventbus.EventBus, companyRepo repositories.CompanyRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo,matcherSvc services.EntityMatcherService,) *Orchestrator {
	return &Orchestrator{logger, db, bus, companyRepo, serverRepo, workstationRepo, frRepo, matcherSvc,}
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
}

// handleContractsStatusRecalculated обрабатывает событие о пересчете статусов контрактов.
func (o *Orchestrator) handleContractsStatusRecalculated(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Оркестратор НАЧАЛ обработку события", zap.String("type", event.Type))
	payload, ok := event.Payload.(events.ContractsStatusPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ContractsStatusRecalculated")
		return
	}
	log := o.logger.With(zap.String("event", event.Type))
	log.Info("Получено событие для обновления статусов контрактов у компаний", zap.Int("count", len(payload.CompanyActiveContract)))
	activeUUIDs := make([]string, 0)
	inactiveUUIDs := make([]string, 0)
	for uuid, isActive := range payload.CompanyActiveContract {
		if isActive {
			activeUUIDs = append(activeUUIDs, uuid)
		} else {
			inactiveUUIDs = append(inactiveUUIDs, uuid)
		}
	}
	err := o.db.Transaction(func(tx *gorm.DB) error {
		source := "contract_gateway"
		if len(activeUUIDs) > 0 {
			res := tx.WithContext(ctx).Model(&models.Company{}).
				Where("service_desk_uuid IN ?", activeUUIDs).
				Updates(map[string]interface{}{"active_contract": true, "last_updated_by": source})
			if res.Error != nil {
				return res.Error
			}
			log.Info("Установлен статус ActiveContract=true для компаний", zap.Int64("updated_count", res.RowsAffected))
		}
		if len(inactiveUUIDs) > 0 {
			res := tx.WithContext(ctx).Model(&models.Company{}).
				Where("service_desk_uuid IN ?", inactiveUUIDs).
				Updates(map[string]interface{}{"active_contract": false, "last_updated_by": source})
			if res.Error != nil {
				return res.Error
			}
			log.Info("Установлен статус ActiveContract=false для компаний", zap.Int64("updated_count", res.RowsAffected))
		}
		return nil
	})
	if err != nil {
		log.Error("Ошибка транзакции при обновлении статусов контрактов", zap.Error(err))
	}
}

// handleServiceDeskEntityDelete обрабатывает событие удаления сущности.
func (o *Orchestrator) handleServiceDeskEntityDelete(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Оркестратор НАЧАЛ обработку события", zap.String("type", event.Type))
	payload, ok := event.Payload.(events.ServiceDeskEntityDeletePayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityDeleted")
		return
	}
	log := o.logger.With(zap.String("metaClass", payload.MetaClass), zap.String("uuid", payload.UUID))
	err := o.db.Transaction(func(tx *gorm.DB) error {
		var txErr error
		switch payload.MetaClass {
		case "ou$company":
			_, txErr = o.companyRepo.Delete(ctx, tx, payload.UUID)
		case "objectBase$Server":
			_, txErr = o.serverRepo.Delete(ctx, tx, payload.UUID)
		case "objectBase$Workstation":
			_, txErr = o.workstationRepo.Delete(ctx, tx, payload.UUID)
		case "objectBase$FR":
			_, txErr = o.frRepo.Delete(ctx, tx, payload.UUID)
		default:
			txErr = fmt.Errorf("неизвестный metaClass для удаления: %s", payload.MetaClass)
		}
		return txErr
	})
	if err != nil {
		log.Error("Ошибка при 'мягком удалении' сущности", zap.Error(err))
	} else {
		log.Info("Сущность успешно 'мягко удалена' по событию из ServiceDesk.")
	}
}

// handleServiceDeskEntityUpdate обрабатывает событие обновления сущности из ServiceDesk.
func (o *Orchestrator) handleServiceDeskEntityUpdate(ctx context.Context, event eventbus.Event) {
	o.logger.Debug("Оркестратор НАЧАЛ обработку события", zap.String("type", event.Type))
	payload, ok := event.Payload.(events.ServiceDeskEntityPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события ServiceDeskEntityUpdated")
		return
	}
	log := o.logger.With(zap.String("metaClass", payload.MetaClass), zap.String("uuid", payload.UUID))
	var updates map[string]interface{}
	var diffLog []zap.Field
	var currentEntity interface{}
	var isNewEntity bool
	source := "servicedesk_gateway"

	err := o.db.Transaction(func(tx *gorm.DB) error {
		switch payload.MetaClass {
		case "ou$company":
			newData, mapErr := services.DataToCompany(ctx, payload.Data, log)
			if mapErr != nil {
				return mapErr
			}
			currentData, getErr := o.companyRepo.GetByUUIDUnscoped(ctx, payload.UUID)
			if getErr != nil {
				return fmt.Errorf("не удалось получить текущую компанию: %w", getErr)
			}
			if currentData == nil {
				isNewEntity = true
				newData.LastUpdatedBy = source
				return o.companyRepo.Create(ctx, tx, newData)
			}
			currentEntity = currentData
			updates, diffLog = getCompanyDiff(currentData, newData)

		case "objectBase$Server":
			newData, mapErr := services.DataToServer(payload.Data)
			if mapErr != nil {
				return mapErr
			}
			currentData, getErr := o.serverRepo.GetByUUIDUnscoped(ctx, payload.UUID)
			if getErr != nil {
				return fmt.Errorf("не удалось получить текущий сервер: %w", getErr)
			}
			if currentData == nil {
				isNewEntity = true
				newData.LastUpdatedBy = source
				return o.serverRepo.Create(ctx, tx, newData)
			}
			currentEntity = currentData
			updates, diffLog = getServerDiff(currentData, newData)

		case "objectBase$Workstation":
			newData, mapErr := services.DataToWorkstation(payload.Data)
			if mapErr != nil {
				return mapErr
			}
			currentData, getErr := o.workstationRepo.GetByUUIDUnscoped(ctx, payload.UUID)
			if getErr != nil {
				return fmt.Errorf("не удалось получить текущую станцию: %w", getErr)
			}
			if currentData == nil {
				isNewEntity = true
				newData.LastUpdatedBy = source
				return o.workstationRepo.Create(ctx, tx, newData)
			}
			currentEntity = currentData
			updates, diffLog = getWorkstationDiff(currentData, newData)

		case "objectBase$FR":
			newData, mapErr := services.DataToFiscalRegister(payload.Data)
			if mapErr != nil {
				return mapErr
			}
			currentData, getErr := o.frRepo.GetByUUIDUnscoped(ctx, payload.UUID)
			if getErr != nil {
				return fmt.Errorf("не удалось получить текущий ФР: %w", getErr)
			}
			if currentData == nil {
				isNewEntity = true
				newData.LastUpdatedBy = source
				return o.frRepo.Create(ctx, tx, newData)
			}
			currentEntity = currentData
			updates, diffLog = getFiscalRegisterDiff(currentData, newData)

		default:
			return fmt.Errorf("неизвестный metaClass для обработки: %s", payload.MetaClass)
		}
		if len(updates) > 0 {
			updates["last_updated_by"] = source
			return o.performUpdate(ctx, tx, payload.MetaClass, payload.UUID, updates)
		}
		return nil
	})
	if err != nil {
		log.Error("Ошибка в транзакции обработки обновления", zap.Error(err))
		return
	}
	if isNewEntity {
		log.Info("Новая сущность успешно создана.")
		return
	}
	if len(updates) == 0 {
		log.Debug("Изменений не найдено, обновление не требуется. Конфликт (если был) устранен.")
		o.resolveConflictTaskIfNeeded(ctx, payload.UUID, log)
	} else {
		_, isRestorationOnly := updates["deleted_at"]
		if len(updates) == 1 && isRestorationOnly {
			log.Info("Сущность была удалена локально, но найдена в SD. Автоматически восстановлена.", diffLog...)
		} else {
			log.Warn("Обнаружено расхождение данных. Создание/обновление задачи.", diffLog...)
			o.createConflictTask(ctx, payload.MetaClass, payload.UUID, currentEntity, payload.Data, diffLog, log)
		}
	}
}
// handleDuplicatesFound создает или обновляет задачу на разрешение дубликатов.
func (o *Orchestrator) handleDuplicatesFound(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.DuplicatesFoundPayload)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события DuplicatesFound")
		return
	}

	log := o.logger.With(
		zap.String("entityType", payload.EntityType),
		zap.String("field", payload.Field),
		zap.String("value", payload.Value),
	)

	// Создаем уникальный идентификатор для задачи, чтобы избежать дублирования
	// задач для одной и той же группы дубликатов.
	taskIdentifier := fmt.Sprintf("duplicate-%s-%s-%s", payload.EntityType, payload.Field, payload.Value)

	detailsMap := map[string]interface{}{
		"field":      payload.Field,
		"value":      payload.Value,
		"entityUUIDs": payload.UUIDs,
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	comment := fmt.Sprintf(
		"Обнаружены дубликаты (%d шт.) по полю '%s' со значением '%s' для сущности '%s'. Требуется выбрать эталонную запись, а остальные удалить.",
		len(payload.UUIDs), payload.Field, payload.Value, payload.EntityType,
	)

	task := models.ReconciliationTask{
		TaskType:   "resolve_duplicate",
		EntityType: payload.EntityType,
		EntityUUID: taskIdentifier, // Используем наш уникальный идентификатор
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    comment,
	}

	// Используем FirstOrCreate, чтобы не создавать повторные задачи.
	// Если задача с таким EntityUUID (нашим идентификатором) уже есть, ничего не делаем.
	result := o.db.WithContext(ctx).
		Where(models.ReconciliationTask{EntityUUID: taskIdentifier, Status: "new"}).
		FirstOrCreate(&task)

	if result.Error != nil {
		log.Error("Не удалось создать задачу на разрешение дубликатов", zap.Error(result.Error))
	} else if result.RowsAffected > 0 {
		log.Info("Создана новая задача на разрешение дубликатов.")
	} else {
		log.Debug("Активная задача на разрешение этих дубликатов уже существует.")
	}
}
func (o *Orchestrator) performUpdate(ctx context.Context, tx *gorm.DB, metaClass, uuid string, updates map[string]interface{}) error {
	switch metaClass {
	case "ou$company":
		_, err := o.companyRepo.Update(ctx, tx, uuid, updates)
		return err
	case "objectBase$Server":
		_, err := o.serverRepo.Update(ctx, tx, uuid, updates)
		return err
	case "objectBase$Workstation":
		_, err := o.workstationRepo.Update(ctx, tx, uuid, updates)
		return err
	case "objectBase$FR":
		_, err := o.frRepo.Update(ctx, tx, uuid, updates)
		return err
	}
	return errors.New("неизвестный metaClass для транзакции обновления")
}

func (o *Orchestrator) createConflictTask(ctx context.Context, metaClass, uuid string, currentEntity interface{}, remoteDetails map[string]interface{}, diffLog []zap.Field, log *zap.Logger) {
	detailsMap := make(map[string]interface{})
	diffs := make(map[string]string)
	for _, field := range diffLog {
		if field.Key == "status" && field.String == "deleted -> restored" {
			continue
		}
		diffs[field.Key] = field.String
	}

	if len(diffs) == 0 {
		return
	}

	detailsMap["conflicts"] = diffs
	detailsMap["local_entity"] = currentEntity
	detailsMap["remote_entity"] = remoteDetails
	detailsJSON, _ := json.Marshal(detailsMap)

	entityType := metaClass
	if parts := strings.Split(metaClass, "$"); len(parts) > 1 {
		entityType = parts[1]
	}
	comment := fmt.Sprintf("Обнаружено расхождение данных для сущности '%s' (%s). Требуется ручная сверка.", uuid, entityType)

	task := models.ReconciliationTask{
		TaskType:   "data_conflict",
		EntityType: entityType,
		EntityUUID: uuid,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    comment,
	}

	err := o.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		FirstOrCreate(&task).Error

	if err != nil {
		log.Error("Не удалось создать или найти задачу о конфликте данных", zap.String("uuid", uuid), zap.Error(err))
	}
}

func (o *Orchestrator) resolveConflictTaskIfNeeded(ctx context.Context, uuid string, log *zap.Logger) {
	result := o.db.WithContext(ctx).Model(&models.ReconciliationTask{}).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", uuid, "data_conflict").
		Updates(map[string]interface{}{
			"status":  "resolved",
			"comment": gorm.Expr("comment || ?", "\n[АВТОМАТИЧЕСКИ] Конфликт устранен, данные синхронизированы."),
		})

	if result.Error != nil {
		log.Error("Ошибка при попытке автоматического разрешения задачи о конфликте", zap.String("uuid", uuid), zap.Error(result.Error))
		return
	}
	if result.RowsAffected > 0 {
		log.Info("Конфликт данных устранен. Существующая задача автоматически разрешена.", zap.String("uuid", uuid))
	}
}

// handleAgentDataReceived выполняет основную "водопадную" логику сверки данных от агента.
func (o *Orchestrator) handleAgentDataReceived(ctx context.Context, event eventbus.Event) {
	data, ok := event.Payload.(api.AgentDataDTO)
	if !ok {
		o.logger.Error("Некорректная полезная нагрузка для события AgentDataReceived")
		return
	}
	
	log := o.logger.With(zap.String("agent_hostname", data.Hostname))
	log.Info("Оркестратор НАЧАЛ обработку события AgentDataReceived")

	matched := o.matcherSvc.FindEntityByAgentData(ctx, &data)

	if matched == nil {
		log.Warn("Не найдено совпадений. Создание задачи 'new_client'.")
		// o.createTask(ctx, "new_client", "", "", &data, "Не удалось идентифицировать оборудование. Требуется создать нового клиента и привязать оборудование.", "")
		return
	}
	
	// Здесь будет полная логика из reconcileFromServerContext, reconcileFromWorkstationContext и т.д.
	// Пока что оставим заглушку, чтобы не перегружать ответ.
	log.Info("Найдено совпадение, логика сверки будет запущена",
		zap.String("entityType", matched.EntityType),
		zap.String("ownerUUID", matched.OwnerUUID),
	)
}

// handleServerPollingSucceeded обрабатывает успешный результат опроса сервера.
func (o *Orchestrator) handleServerPollingSucceeded(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingSucceededPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("uuid", payload.ServerUUID))
	updates := map[string]interface{}{
		"server_name":      payload.ServerName,
		"server_edition":   payload.ServerEdition,
		"server_version":   payload.ServerVersion,
		"status":           payload.NewStatus,
		"last_polled_at":   payload.LastPolledAt,
		"last_updated_by":  "rms_polling",
	}

	if _, err := o.serverRepo.Update(ctx, nil, payload.ServerUUID, updates); err != nil {
		log.Error("Не удалось обновить данные сервера после успешного опроса", zap.Error(err))
	} else {
		log.Info("Данные сервера успешно обновлены", zap.String("new_status", payload.NewStatus))
	}
	// TODO: В будущем здесь можно создавать задачу на установку лицензии, если NewStatus = "license"
}

// handleServerPollingFailed обрабатывает неудачный результат опроса сервера.
func (o *Orchestrator) handleServerPollingFailed(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServerPollingFailedPayload)
	if !ok {
		return
	}
	log := o.logger.With(zap.String("uuid", payload.ServerUUID))
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


// --- Вспомогательные функции для сравнения (diff) ---

func formatDiffValue(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return "<nil>"
		}
		return fmt.Sprintf("'%v'", val.Elem().Interface())
	}
	return fmt.Sprintf("'%v'", v)
}

func compareAndLog[T comparable](updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *T) {
	currentVal := reflect.ValueOf(current)
	newVal := reflect.ValueOf(new)
	isCurrentNil := current == nil || currentVal.IsNil()
	isNewNil := new == nil || newVal.IsNil()
	if isCurrentNil && isNewNil {
		return
	}
	if isCurrentNil != isNewNil {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
		return
	}
	if *current != *new {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

func compareTimeAndLog(updates map[string]interface{}, diffs *[]zap.Field, key string, current, new *time.Time) {
	if current == nil && new == nil {
		return
	}
	if (current == nil && new != nil) || (current != nil && new == nil) || (current != nil && new != nil && !current.Equal(*new)) {
		updates[key] = new
		logString := fmt.Sprintf("%s -> %s", formatDiffValue(current), formatDiffValue(new))
		*diffs = append(*diffs, zap.String(key, logString))
	}
}

func getCompanyDiff(current *models.Company, new *models.Company) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)
	compareAndLog(updates, &diffs, "title", current.Title, new.Title)
	compareAndLog(updates, &diffs, "address", current.Address, new.Address)
	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

func getServerDiff(current *models.Server, new *models.Server) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)
	compareAndLog(updates, &diffs, "unique_id", current.UniqueID, new.UniqueID)
	compareAndLog(updates, &diffs, "rdp", current.RDP, new.RDP)
	compareAndLog(updates, &diffs, "server_version", current.ServerVersion, new.ServerVersion)
	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

func getWorkstationDiff(current *models.Workstation, new *models.Workstation) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)
	compareAndLog(updates, &diffs, "teamviewer", current.Teamviewer, new.Teamviewer)
	compareAndLog(updates, &diffs, "anydesk", current.Anydesk, new.Anydesk)
	compareAndLog(updates, &diffs, "litemanager", current.Litemanager, new.Litemanager)
	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}

func getFiscalRegisterDiff(current *models.FiscalRegister, new *models.FiscalRegister) (map[string]interface{}, []zap.Field) {
	updates := make(map[string]interface{})
	diffs := make([]zap.Field, 0)
	compareTimeAndLog(updates, &diffs, "fn_expire_date", current.FNExpireDate, new.FNExpireDate)
	if len(updates) > 0 || current.DeletedAt.Valid {
		updates["last_modified_date"] = new.LastModifiedDate
		if current.DeletedAt.Valid {
			updates["deleted_at"] = gorm.Expr("NULL")
			diffs = append(diffs, zap.String("status", "deleted -> restored"))
		}
	}
	return updates, diffs
}