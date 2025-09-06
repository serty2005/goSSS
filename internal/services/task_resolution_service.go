// internal/services/task_resolution_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/contextkeys" // ИЗМЕНЕНИЕ: Новый импорт
	"etalon-server/internal/core/events"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

var (
	ErrTaskNotFound      = errors.New("задача не найдена")
	ErrTaskAlreadyDone   = errors.New("задача уже была решена или отклонена")
	ErrInvalidPayload    = errors.New("некорректные данные в resolution_payload для этого типа задачи")
	ErrUnsupportedTask   = errors.New("этот тип задачи не поддерживает автоматическое выполнение")
	ErrInternalExecution = errors.New("внутренняя ошибка при выполнении действия по задаче")
)

// contextKey - создаем собственный тип для ключа контекста, чтобы избежать коллизий.
type contextKey string

// transactionKey - определяем ключ для транзакции.
const transactionKey contextKey = "tx"

// TaskResolutionService определяет интерфейс для сервиса выполнения задач.
type TaskResolutionService interface {
	Resolve(ctx context.Context, taskID uint, dto *api.ResolveTaskRequestDTO) (*models.ReconciliationTask, error)
	RequestSDEntityCreation(ctx context.Context, taskID uint, entityType string) (*models.ReconciliationTask, error)
}

type taskResolutionServiceImpl struct {
	logger          *zap.Logger
	db              *gorm.DB
	bus             eventbus.EventBus
	taskRepo        repositories.TaskRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewTaskResolutionService создает новый экземпляр сервиса.
func NewTaskResolutionService(logger *zap.Logger, db *gorm.DB, bus eventbus.EventBus, taskRepo repositories.TaskRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) TaskResolutionService {
	return &taskResolutionServiceImpl{
		logger:          logger,
		db:              db,
		bus:             bus,
		taskRepo:        taskRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Resolve выполняет задачу на основе resolution_payload.
func (s *taskResolutionServiceImpl) Resolve(ctx context.Context, taskID uint, dto *api.ResolveTaskRequestDTO) (*models.ReconciliationTask, error) {
	var updatedTask *models.ReconciliationTask
	err := s.db.Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, transactionKey, tx)

		task, err := s.taskRepo.GetByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return ErrTaskNotFound
		}
		// Разрешаем повторную отправку в SD для задач со статусом ошибки
		if task.Status != "new" && task.Status != "sd_error" {
			return ErrTaskAlreadyDone
		}

		// Выполняем действие в зависимости от payload
		if dto.ResolutionPayload != nil {
			action, _ := dto.ResolutionPayload["action"].(string)
			switch action {
			case "update_in_sd":
				// Асинхронное действие: публикуем событие
				err = s.handleUpdateInSD(ctx, task)
			case "create":
				// Синхронное действие внутри транзакции
				err = s.handleAddEquipment(txCtx, task, dto.ResolutionPayload)
			case "update_owner":
				// Синхронное действие внутри транзакции
				err = s.handleOwnerMismatch(txCtx, task, dto.ResolutionPayload)
			case "use_remote", "use_local":
				// Синхронное действие внутри транзакции
				err = s.handleDataConflict(txCtx, task, dto.ResolutionPayload)
			case "delete_duplicates":
				// Синхронное действие внутри транзакции
				err = s.handleResolveDuplicate(txCtx, task, dto.ResolutionPayload)
			}
			if err != nil {
				return err // Откатываем транзакцию при любой ошибке
			}
		}

		// Обновляем саму задачу
		task.Status = dto.Status
		if dto.Comment != "" {
			task.Comment = fmt.Sprintf("%s\n[РЕШЕНИЕ] %s", task.Comment, dto.Comment)
		}
		if err := tx.Save(task).Error; err != nil {
			return err
		}

		updatedTask = task
		return nil
	})

	return updatedTask, err
}

// handleUpdateInSD публикует событие для асинхронного обновления сущности в ServiceDesk.
func (s *taskResolutionServiceImpl) handleUpdateInSD(ctx context.Context, task *models.ReconciliationTask) error {
	// 1. Получаем ID пользователя из контекста для аудита
	// ИЗМЕНЕНИЕ: Используем константу из contextkeys
	userID, ok := ctx.Value(contextkeys.UserIDContextKey).(string)
	if !ok {
		s.logger.Warn("Не удалось получить ID пользователя из контекста для события обновления SD", zap.Uint("taskID", task.ID))
		userID = "unknown"
	}

	// 2. Создаем полезную нагрузку для события.
	payload := events.ServiceDeskModificationPayload{
		TaskID:            task.ID,
		EntityType:        task.EntityType,
		EntityUUID:        task.EntityUUID,
		TriggeredByUserID: userID,
		PayloadForSD:      nil, // Будет сформирован обработчиком события
	}

	// 3. Публикуем событие
	s.bus.Publish(eventbus.Event{
		Type:    events.ServiceDeskUpdateRequested,
		Payload: payload,
	})

	s.logger.Info("Опубликовано событие на обновление сущности в ServiceDesk",
		zap.Uint("taskID", task.ID),
		zap.String("entityUUID", task.EntityUUID),
	)

	return nil
}

// RequestSDEntityCreation инициирует асинхронное создание сущности в ServiceDesk.
func (s *taskResolutionServiceImpl) RequestSDEntityCreation(ctx context.Context, taskID uint, entityType string) (*models.ReconciliationTask, error) {
	var updatedTask *models.ReconciliationTask
	err := s.db.Transaction(func(tx *gorm.DB) error {
		task, err := s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return ErrTaskNotFound
		}
		// Разрешаем повторную отправку в SD для задач со статусом ошибки
		if task.Status != "new" && task.Status != "sd_error" {
			return ErrTaskAlreadyDone
		}
		if task.TaskType != "add_equipment" {
			return fmt.Errorf("создание сущности возможно только для задач типа 'add_equipment'")
		}
		if task.EntityType != entityType {
			return fmt.Errorf("несоответствие типа сущности: в задаче %s, в запросе %s", task.EntityType, entityType)
		}

		// Меняем статус задачи
		task.Status = "pending_sd_action"
		task.Comment = fmt.Sprintf("%s\n[ДЕЙСТВИЕ] Отправлен запрос на создание сущности в ServiceDesk.", task.Comment)
		if err := tx.Save(task).Error; err != nil {
			return err
		}

		// ИЗМЕНЕНИЕ: Используем константу из contextkeys
		userID, _ := ctx.Value(contextkeys.UserIDContextKey).(string)
		if userID == "" {
			userID = "unknown"
		}

		// Публикуем событие
		payload := events.ServiceDeskModificationPayload{
			TaskID:            task.ID,
			EntityType:        task.EntityType,
			TriggeredByUserID: userID,
		}
		s.bus.Publish(eventbus.Event{
			Type:    events.ServiceDeskCreateRequested,
			Payload: payload,
		})
		s.logger.Info("Опубликовано событие на создание сущности в ServiceDesk", zap.Uint("taskID", task.ID))

		updatedTask = task
		return nil
	})
	return updatedTask, err
}

// handleDataConflict обрабатывает задачу о конфликте данных.
func (s *taskResolutionServiceImpl) handleDataConflict(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	strategy, ok := payload["strategy"].(string)
	if !ok {
		return ErrInvalidPayload
	}

	if strategy == "use_remote" {
		var details struct {
			RemoteEntity map[string]interface{} `json:"remote_entity"`
		}
		if err := json.Unmarshal(task.Details, &details); err != nil {
			return ErrInternalExecution
		}

		tx := ctx.Value(transactionKey).(*gorm.DB)
		var err error
		switch task.EntityType {
		case "Server":
			server, _ := DataToServer(details.RemoteEntity)
			_, err = s.serverRepo.Update(ctx, tx, task.EntityUUID, map[string]interface{}{"unique_id": server.UniqueID, "rdp": server.RDP, "server_version": server.ServerVersion})
		case "Workstation":
			ws, _ := DataToWorkstation(details.RemoteEntity)
			_, err = s.workstationRepo.Update(ctx, tx, task.EntityUUID, map[string]interface{}{"teamviewer": ws.Teamviewer, "anydesk": ws.Anydesk, "litemanager": ws.Litemanager})
		default:
			return ErrUnsupportedTask
		}
		if err != nil {
			return ErrInternalExecution
		}
	} else if strategy != "use_local" {
		return ErrInvalidPayload
	}

	return nil
}

// handleResolveDuplicate обрабатывает задачу об удалении дубликатов.
func (s *taskResolutionServiceImpl) handleResolveDuplicate(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	// ИЗМЕНЕНИЕ: Ключ в payload должен быть "delete_record_uuids"
	rawUUIDs, ok := payload["delete_record_uuids"].([]interface{})
	if !ok {
		return ErrInvalidPayload
	}

	tx := ctx.Value(transactionKey).(*gorm.DB)
	for _, uuidInterface := range rawUUIDs {
		uuid, ok := uuidInterface.(string)
		if !ok {
			continue
		}
		var err error
		switch task.EntityType {
		case "Server":
			_, err = s.serverRepo.Delete(ctx, tx, uuid)
		case "Workstation":
			_, err = s.workstationRepo.Delete(ctx, tx, uuid)
		}
		if err != nil {
			s.logger.Error("Ошибка 'мягкого удаления' дубликата", zap.String("uuid", uuid), zap.Error(err))
		}
	}
	return nil
}

// handleOwnerMismatch обрабатывает задачу о смене владельца.
func (s *taskResolutionServiceImpl) handleOwnerMismatch(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	action, ok := payload["action"].(string)
	if !ok {
		return ErrInvalidPayload
	}

	if action == "update_owner" {
		newOwnerUUID, ok := payload["new_owner_uuid"].(string)
		if !ok {
			return ErrInvalidPayload
		}
		tx := ctx.Value(transactionKey).(*gorm.DB)
		updates := map[string]interface{}{"owner_service_desk_uuid": newOwnerUUID}
		var err error
		switch task.EntityType {
		case "Server":
			_, err = s.serverRepo.Update(ctx, tx, task.EntityUUID, updates)
		case "Workstation":
			_, err = s.workstationRepo.Update(ctx, tx, task.EntityUUID, updates)
		case "FiscalRegister":
			_, err = s.frRepo.Update(ctx, tx, task.EntityUUID, updates)
		}
		if err != nil {
			return ErrInternalExecution
		}
	} else if action != "ignore" {
		return ErrInvalidPayload
	}
	return nil
}

// handleAddEquipment обрабатывает задачу о добавлении нового оборудования.
func (s *taskResolutionServiceImpl) handleAddEquipment(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	action, ok := payload["action"].(string)
	if !ok {
		return ErrInvalidPayload
	}

	if action == "create" {
		var details struct {
			AgentData       api.AgentDataDTO `json:"agent_data"`
			EtalonOwnerUUID string           `json:"etalon_owner_uuid"`
		}
		if err := json.Unmarshal(task.Details, &details); err != nil || details.EtalonOwnerUUID == "" {
			s.logger.Error("Не удалось извлечь agent_data или etalon_owner_uuid из деталей задачи", zap.Uint("task_id", task.ID))
			return ErrInternalExecution
		}

		tx := ctx.Value(transactionKey).(*gorm.DB)
		var err error
		switch task.EntityType {
		case "Workstation":
			ws := &models.Workstation{
				OwnerServiceDeskUUID: &details.EtalonOwnerUUID,
				DeviceName:           &details.AgentData.Hostname,
				Teamviewer:           &details.AgentData.TeamviewerID,
				Litemanager:          &details.AgentData.LitemanagerID,
				Anydesk:              &details.AgentData.AnydeskID,
				Status:               utils.StringPtr("offline"),
			}
			err = s.workstationRepo.Create(ctx, tx, ws)
		case "FiscalRegister":
			fr := &models.FiscalRegister{
				OwnerServiceDeskUUID: &details.EtalonOwnerUUID,
				FRSerialNumber:       &details.AgentData.SerialNumber,
				ModelKKT:             &details.AgentData.ModelName,
				INN:                  &details.AgentData.INN,
				RNKKT:                &details.AgentData.RNM,
			}
			err = s.frRepo.Create(ctx, tx, fr)
		}
		if err != nil {
			return ErrInternalExecution
		}
	} else if action != "reject" {
		return ErrInvalidPayload
	}

	return nil
}
