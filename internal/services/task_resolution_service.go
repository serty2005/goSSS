// internal/services/task_resolution_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
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

// НОВЫЙ ТИП: Создаем собственный тип для ключа контекста, чтобы избежать коллизий.
type contextKey string

// НОВАЯ КОНСТАНТА: Определяем ключ для транзакции.
const transactionKey contextKey = "tx"

// TaskResolutionService определяет интерфейс для сервиса выполнения задач.
type TaskResolutionService interface {
	Resolve(ctx context.Context, taskID uint, dto *api.ResolveTaskRequestDTO) (*models.ReconciliationTask, error)
}

type taskResolutionServiceImpl struct {
	logger          *zap.Logger
	db              *gorm.DB
	taskRepo        repositories.TaskRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewTaskResolutionService создает новый экземпляр сервиса.
func NewTaskResolutionService(logger *zap.Logger, db *gorm.DB, taskRepo repositories.TaskRepo, serverRepo repositories.ServerRepo, workstationRepo repositories.WorkstationRepo, frRepo repositories.FiscalRegisterRepo) TaskResolutionService {
	return &taskResolutionServiceImpl{logger, db, taskRepo, serverRepo, workstationRepo, frRepo}
}

// Resolve выполняет задачу на основе resolution_payload.
func (s *taskResolutionServiceImpl) Resolve(ctx context.Context, taskID uint, dto *api.ResolveTaskRequestDTO) (*models.ReconciliationTask, error) {
	var updatedTask *models.ReconciliationTask
	err := s.db.Transaction(func(tx *gorm.DB) error {
		// ИСПРАВЛЕНО: Используем наш типизированный ключ вместо строки.
		txCtx := context.WithValue(ctx, transactionKey, tx)

		task, err := s.taskRepo.GetByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return ErrTaskNotFound
		}
		if task.Status != "new" {
			return ErrTaskAlreadyDone
		}

		// Выполняем действие в зависимости от типа задачи
		if dto.ResolutionPayload != nil {
			switch task.TaskType {
			case "data_conflict":
				err = s.handleDataConflict(txCtx, task, dto.ResolutionPayload)
			case "resolve_duplicate":
				err = s.handleResolveDuplicate(txCtx, task, dto.ResolutionPayload)
			case "owner_mismatch":
				err = s.handleOwnerMismatch(txCtx, task, dto.ResolutionPayload)
			case "add_equipment":
				err = s.handleAddEquipment(txCtx, task, dto.ResolutionPayload)
			default:
				// Для других типов задач payload пока не поддерживается
			}
			if err != nil {
				return err // Возвращаем ошибку, чтобы откатить транзакцию
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

		// ИСПРАВЛЕНО: Используем наш типизированный ключ.
		tx := ctx.Value(transactionKey).(*gorm.DB)
		var err error
		switch task.EntityType {
		case "Server":
			server, _ := DataToServer(details.RemoteEntity)
			_, err = s.serverRepo.Update(ctx, tx, task.EntityUUID, map[string]interface{}{"unique_id": server.UniqueID, "rdp": server.RDP, "server_version": server.ServerVersion})
		case "Workstation":
			ws, _ := DataToWorkstation(details.RemoteEntity)
			_, err = s.workstationRepo.Update(ctx, tx, task.EntityUUID, map[string]interface{}{"teamviewer": ws.Teamviewer, "anydesk": ws.Anydesk, "litemanager": ws.Litemanager})
		// Добавить другие сущности по аналогии
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
	deleteUUIDs, ok := payload["delete_record_uuids"].([]interface{})
	if !ok {
		return ErrInvalidPayload
	}

	// ИСПРАВЛЕНО: Используем наш типизированный ключ.
	tx := ctx.Value(transactionKey).(*gorm.DB)
	for _, uuidInterface := range deleteUUIDs {
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
			// Добавить другие сущности по аналогии
		}
		if err != nil {
			s.logger.Error("Ошибка 'мягкого удаления' дубликата", zap.String("uuid", uuid), zap.Error(err))
			// Не возвращаем ошибку, чтобы продолжить удаление остальных
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
		// ИСПРАВЛЕНО: Используем наш типизированный ключ.
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

		// ИСПРАВЛЕНО: Используем наш типизированный ключ.
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
