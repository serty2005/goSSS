package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/interfaces"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"

	"etalon-server/pkg/eventbus"
)

var (
	ErrTaskNotFound      = errors.New("задача не найдена")
	ErrTaskAlreadyDone   = errors.New("задача уже была решена или отклонена")
	ErrInvalidPayload    = errors.New("некорректные данные в resolution_payload для этого типа задачи")
	ErrUnsupportedTask   = errors.New("этот тип задачи не поддерживает автоматическое выполнение")
	ErrInternalExecution = errors.New("внутренняя ошибка при выполнении действия по задаче")
)

// TaskResolutionService определяет интерфейс для сервиса выполнения задач.
type TaskResolutionService interface {
	Resolve(ctx context.Context, taskID uint, dto *api.ResolveTaskRequestDTO) (*models.ReconciliationTask, error)
	RequestSDEntityCreation(ctx context.Context, taskID uint, entityType string) (*models.ReconciliationTask, error)
}

type taskResolutionServiceImpl struct {
	logger          logger.LoggerInterface
	tm              interfaces.Transactor
	bus             eventbus.EventBus
	taskRepo        repositories.TaskRepo
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

// NewTaskResolutionService создает новый экземпляр сервиса.
func NewTaskResolutionService(logger logger.LoggerInterface, tm interfaces.Transactor, bus eventbus.EventBus, taskRepo repositories.TaskRepo, serverRepo server.Repository, workstationRepo workstation.Repository, frRepo fiscal.Repository) TaskResolutionService {
	return &taskResolutionServiceImpl{
		logger:          logger,
		tm:              tm,
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
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
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
			case "delete_duplicates":
				err = s.handleResolveDuplicate(txCtx, task, dto.ResolutionPayload)
			}
			if err != nil {
				return err
			}
		}

		comment := task.Comment
		if dto.Comment != "" {
			comment = fmt.Sprintf("%s\n[РЕШЕНИЕ] %s", task.Comment, dto.Comment)
		}
		ok, err := s.taskRepo.Update(txCtx, task.ID, map[string]interface{}{"status": dto.Status, "comment": comment})
		if err != nil {
			return err
		}
		if !ok {
			return ErrTaskNotFound
		}

		task.Status = dto.Status
		task.Comment = comment
		updatedTask = task
		return nil
	})

	return updatedTask, err
}

// RequestSDEntityCreation инициирует асинхронное создание сущности в ServiceDesk.
func (s *taskResolutionServiceImpl) RequestSDEntityCreation(ctx context.Context, taskID uint, entityType string) (*models.ReconciliationTask, error) {
	var updatedTask *models.ReconciliationTask
	err := s.tm.WithinTransaction(ctx, func(txCtx context.Context) error {
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
		if task.TaskType != "add_equipment" {
			return fmt.Errorf("создание сущности возможно только для задач типа 'add_equipment'")
		}
		if task.EntityType != entityType {
			return fmt.Errorf("несоответствие типа сущности: в задаче %s, в запросе %s", task.EntityType, entityType)
		}

		comment := fmt.Sprintf("%s\n[ДЕЙСТВИЕ] Отправлен запрос на создание сущности в ServiceDesk.", task.Comment)
		ok, err := s.taskRepo.Update(txCtx, task.ID, map[string]interface{}{"status": "pending_sd_action", "comment": comment})
		if err != nil {
			return err
		}
		if !ok {
			return ErrTaskNotFound
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
		s.bus.Publish(eventbus.Event{Type: events.ServiceDeskCreateRequested, Payload: payload})
		s.logger.Info("Опубликовано событие на создание сущности в ServiceDesk", "taskID", task.ID)

		task.Status = "pending_sd_action"
		task.Comment = comment
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
		EntityUUID:        task.EntityUUID,
		TriggeredByUserID: userID,
	}
	s.bus.Publish(eventbus.Event{Type: events.ServiceDeskUpdateRequested, Payload: payload})
	s.logger.Info("Опубликовано событие на обновление сущности в ServiceDesk", "taskID", task.ID, "entityUUID", task.EntityUUID)
	return nil
}

// handleResolveDuplicate обрабатывает задачу об удалении дубликатов.
func (s *taskResolutionServiceImpl) handleResolveDuplicate(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	rawUUIDs, ok := payload["delete_record_uuids"].([]interface{})
	if !ok {
		return ErrInvalidPayload
	}

	for _, uuidInterface := range rawUUIDs {
		uuid, ok := uuidInterface.(string)
		if !ok {
			continue
		}
		var err error
		switch task.EntityType {
		case "Server":
			_, err = s.serverRepo.Delete(ctx, nil, uuid)
		case "Workstation":
			_, err = s.workstationRepo.Delete(ctx, nil, uuid)
		}
		if err != nil {
			s.logger.Error("Ошибка 'мягкого удаления' дубликата", "uuid", uuid, "error", err)
		}
	}
	return nil
}

// handleAddEquipment обрабатывает задачу о добавлении нового оборудования.
func (s *taskResolutionServiceImpl) handleAddEquipment(ctx context.Context, task *models.ReconciliationTask, payload map[string]interface{}) error {
	_ = payload
	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_id"`
	}
	if err := json.Unmarshal(task.Details, &details); err != nil || details.EtalonOwnerUUID == "" {
		s.logger.Error("Не удалось извлечь agent_data или etalon_owner_id из деталей задачи", "task_id", task.ID)
		return ErrInternalExecution
	}

	var err error
	switch task.EntityType {
	case "Workstation":
		ws := &workstation.Workstation{
			OwnerID:      &details.EtalonOwnerUUID,
			DeviceName:   &details.AgentData.Hostname,
			Teamviewer:   &details.AgentData.TeamviewerID,
			Litemanager:  &details.AgentData.LitemanagerID,
			Anydesk:      &details.AgentData.AnydeskID,
			HealthStatus: "",
		}
		err = s.workstationRepo.Create(ctx, nil, ws)
	case "FiscalRegister":
		fr := &fiscal.FiscalRegister{
			OwnerID:        &details.EtalonOwnerUUID,
			FRSerialNumber: &details.AgentData.SerialNumber,
			ModelKKT:       &details.AgentData.ModelName,
			INN:            &details.AgentData.INN,
			RNKKT:          &details.AgentData.RNM,
		}
		err = s.frRepo.Create(ctx, nil, fr)
	}
	if err != nil {
		return ErrInternalExecution
	}

	return nil
}
