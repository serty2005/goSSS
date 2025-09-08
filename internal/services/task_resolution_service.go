// internal/services/task_resolution_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/contextkeys"
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

type contextKey string

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
		if task.Status != "new" && task.Status != "sd_error" {
			return ErrTaskAlreadyDone
		}

		if dto.ResolutionPayload != nil {
			action, _ := dto.ResolutionPayload["action"].(string)
			switch action {
			case "update_in_sd":
				err = s.handleUpdateInSD(ctx, task)
			case "create":
				err = s.handleAddEquipment(txCtx, task, dto.ResolutionPayload)
			case "update_owner":
				err = s.handleOwnerMismatch(txCtx, task, dto.ResolutionPayload)
			case "delete_duplicates":
				err = s.handleResolveDuplicate(txCtx, task, dto.ResolutionPayload)
			}
			if err != nil {
				return err
			}
		}

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
		if task.Status != "new" && task.Status != "sd_error" {
			return ErrTaskAlreadyDone
		}
		if task.TaskType != "add_equipment" {
			return fmt.Errorf("создание сущности возможно только для задач типа 'add_equipment'")
		}
		if task.EntityType != entityType {
			return fmt.Errorf("несоответствие типа сущности: в задаче %s, в запросе %s", task.EntityType, entityType)
		}

		task.Status = "pending_sd_action"
		task.Comment = fmt.Sprintf("%s\n[ДЕЙСТВИЕ] Отправлен запрос на создание сущности в ServiceDesk.", task.Comment)
		if err := tx.Save(task).Error; err != nil {
			return err
		}

		userID, _ := ctx.Value(contextkeys.UserIDContextKey).(string)
		if userID == "" {
			userID = "unknown"
		}

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

// handleUpdateInSD публикует событие для асинхронного обновления.
func (s *taskResolutionServiceImpl) handleUpdateInSD(ctx context.Context, task *models.ReconciliationTask) error {
	userID, _ := ctx.Value(contextkeys.UserIDContextKey).(string)
	if userID == "" {
		userID = "unknown"
	}

	payload := events.ServiceDeskModificationPayload{
		TaskID:            task.ID,
		EntityType:        task.EntityType,
		EntityUUID:        task.EntityUUID, // Здесь EntityUUID это внутренний ID
		TriggeredByUserID: userID,
	}
	s.bus.Publish(eventbus.Event{
		Type:    events.ServiceDeskUpdateRequested,
		Payload: payload,
	})
	s.logger.Info("Опубликовано событие на обновление сущности в ServiceDesk", zap.Uint("taskID", task.ID), zap.String("entityUUID", task.EntityUUID))
	return nil
}

// handleResolveDuplicate обрабатывает задачу об удалении дубликатов.
func (s *taskResolutionServiceImpl) handleResolveDuplicate(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
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
	newOwnerID, ok := payload["new_owner_id"].(string) // Ожидаем внутренний ID
	if !ok {
		return ErrInvalidPayload
	}

	tx := ctx.Value(transactionKey).(*gorm.DB)
	updates := map[string]interface{}{"owner_id": newOwnerID}
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
	return nil
}

// handleAddEquipment обрабатывает задачу о добавлении нового оборудования.
func (s *taskResolutionServiceImpl) handleAddEquipment(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_id"` // Это должен быть внутренний ID
	}
	if err := json.Unmarshal(task.Details, &details); err != nil || details.EtalonOwnerUUID == "" {
		s.logger.Error("Не удалось извлечь agent_data или etalon_owner_id из деталей задачи", zap.Uint("task_id", task.ID))
		return ErrInternalExecution
	}

	tx := ctx.Value(transactionKey).(*gorm.DB)
	var err error
	switch task.EntityType {
	case "Workstation":
		ws := &models.Workstation{
			OwnerID:     &details.EtalonOwnerUUID,
			DeviceName:  &details.AgentData.Hostname,
			Teamviewer:  &details.AgentData.TeamviewerID,
			Litemanager: &details.AgentData.LitemanagerID,
			Anydesk:     &details.AgentData.AnydeskID,
			Status:      utils.StringPtr("offline"),
		}
		err = s.workstationRepo.Create(ctx, tx, ws)
	case "FiscalRegister":
		fr := &models.FiscalRegister{
			OwnerID:        &details.EtalonOwnerUUID,
			FRSerialNumber: &details.AgentData.SerialNumber,
			ModelKKT:       &details.AgentData.ModelName,
			INN:            &details.AgentData.INN,
			RNKKT:          &details.AgentData.RNM,
		}
		err = s.frRepo.Create(ctx, tx, fr)
	}
	if err != nil {
		return ErrInternalExecution
	}

	return nil
}
