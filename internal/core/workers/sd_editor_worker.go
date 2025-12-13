// internal/core/workers/sd_editor_worker.go
package workers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/core/integrations"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/fiscal"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// SDEditorWorker - воркер для записи изменений во внешние системы (Spokes).
type SDEditorWorker interface {
	Start(ctx context.Context)
}

type sdEditorWorkerImpl struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	bus             eventbus.EventBus
	manager         *integrations.Manager
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

func NewSDEditorWorker(
	logger logger.LoggerInterface,
	db *gorm.DB,
	bus eventbus.EventBus,
	manager *integrations.Manager,
	taskRepo repositories.TaskRepo,
	linkRepo repositories.LinkRepo,
	companyRepo company.Repository,
	serverRepo server.Repository,
	workstationRepo workstation.Repository,
	frRepo fiscal.Repository,
) SDEditorWorker {
	return &sdEditorWorkerImpl{
		logger:          logger,
		db:              db,
		bus:             bus,
		manager:         manager,
		taskRepo:        taskRepo,
		linkRepo:        linkRepo,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

func (s *sdEditorWorkerImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера SDEditorWorker (запись во внешние системы)")
	s.bus.Subscribe(events.ServiceDeskCreateRequested, s.handleCreateRequest)
	s.bus.Subscribe(events.ServiceDeskUpdateRequested, s.handleUpdateRequest)
}

// handleUpdateRequest - обновление сущности.
func (s *sdEditorWorkerImpl) handleUpdateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}
	log := s.logger.With("taskID", payload.TaskID, "internalUUID", payload.EntityUUID)
	log.Info("Запрос на обновление сущности во внешней системе")

	// 1. Получаем провайдеров (пока шлём во все подключенные Inventory системы)
	providers := s.manager.GetInventoryProviders()
	if len(providers) == 0 {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", "Нет активных провайдеров интеграции")
		return
	}

	successCount := 0

	// 2. Итерируем провайдеров
	for _, provider := range providers {
		// Ищем связь именно для этой системы
		link, err := s.linkRepo.GetByInternalID(ctx, nil, provider.SystemName(), payload.EntityUUID)
		if err != nil || link == nil {
			log.Warn("Не найдена связь с системой", "system", provider.SystemName())
			continue
		}

		var errUpdate error
		switch payload.EntityType {
		case "FiscalRegister":
			fr, err := s.frRepo.GetByID(ctx, payload.EntityUUID)
			if err == nil && fr != nil {
				errUpdate = provider.UpdateFiscalRegister(ctx, link.ServiceDeskUUID, fr)
			} else {
				errUpdate = fmt.Errorf("сущность не найдена в БД")
			}
		default:
			errUpdate = fmt.Errorf("обновление типа '%s' не поддерживается", payload.EntityType)
		}

		if errUpdate != nil {
			log.Error("Ошибка обновления в системе", "system", provider.SystemName(), "error", errUpdate)
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		s.updateTaskStatus(ctx, payload.TaskID, "resolved", "Сущность успешно обновлена.")
	} else {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", "Не удалось обновить сущность ни в одной системе.")
	}
}

// handleCreateRequest - создание сущности.
func (s *sdEditorWorkerImpl) handleCreateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}
	log := s.logger.With("taskID", payload.TaskID)
	log.Info("Запрос на создание сущности во внешней системе")

	providers := s.manager.GetInventoryProviders()
	if len(providers) == 0 {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", "Нет активных провайдеров интеграции")
		return
	}

	task, err := s.taskRepo.GetByID(ctx, payload.TaskID)
	if err != nil || task == nil {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", "Задача не найдена")
		return
	}

	// Подготавливаем модель из данных задачи (AgentData)
	// Это нужно для того, чтобы передать в адаптер унифицированный объект
	model, internalID, err := s.prepareModelFromTask(ctx, task)
	if err != nil {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка подготовки данных: %v", err))
		return
	}

	successCount := 0

	for _, provider := range providers {
		var newExtUUID string
		var createErr error

		switch v := model.(type) {
		case *fiscal.FiscalRegister:
			newExtUUID, createErr = provider.CreateFiscalRegister(ctx, v)
		default:
			createErr = fmt.Errorf("создание типа '%s' не поддерживается", payload.EntityType)
		}

		if createErr != nil {
			log.Error("Ошибка создания в системе", "system", provider.SystemName(), "error", createErr)
			continue
		}

		// Создаем связь
		newLink := models.ExternalSystemLink{
			InternalID:      internalID,
			SystemName:      provider.SystemName(),
			ServiceDeskUUID: newExtUUID,
			EntityType:      payload.EntityType,
			LastSyncedAt:    time.Now(),
		}
		if err := s.linkRepo.Create(ctx, nil, &newLink); err != nil {
			log.Error("Критическая ошибка: связь не создана", "system", provider.SystemName(), "error", err)
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		s.updateTaskStatus(ctx, payload.TaskID, "resolved", "Сущность успешно создана во внешних системах.")
	} else {
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", "Не удалось создать сущность ни в одной системе.")
	}
}

// prepareModelFromTask извлекает данные из задачи и формирует доменную модель.
// Возвращает (Model Interface, InternalID, error).
func (s *sdEditorWorkerImpl) prepareModelFromTask(ctx context.Context, task *models.ReconciliationTask) (interface{}, string, error) {
	var details struct {
		AgentData     api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerID string           `json:"etalon_owner_id"` // Internal ID
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return nil, "", err
	}
	if details.EtalonOwnerID == "" {
		return nil, "", fmt.Errorf("в задаче нет owner_id")
	}

	switch task.EntityType {
	case "FiscalRegister":
		// Ищем уже созданную (в TaskResolutionService) запись в БД по серийнику
		fr, err := s.frRepo.FindBySerialNumber(ctx, details.AgentData.SerialNumber)
		if err != nil || fr == nil {
			return nil, "", fmt.Errorf("ФР с serial=%s не найден в БД (должен быть создан при resolve)", details.AgentData.SerialNumber)
		}

		// Обогащаем модель данными, которые не хранятся в полях БД, но нужны адаптеру (через Attributes)
		// Например, FNExecution для справочника сроков
		attrs := make(map[string]interface{})
		attrs["fn_execution"] = details.AgentData.FNExecution
		if b, err := json.Marshal(attrs); err == nil {
			fr.Attributes = datatypes.JSON(b)
		}

		// Обновляем поля, которые могли прийти свежими
		fr.FRFirmware = utils.StringPtr(utils.CalculateFRFirmware(details.AgentData.Licenses))
		fr.FRDownloader = utils.StringPtr(details.AgentData.BootVersion)

		return fr, fr.ID, nil
	}

	return nil, "", fmt.Errorf("неподдерживаемый тип: %s", task.EntityType)
}

func (s *sdEditorWorkerImpl) updateTaskStatus(ctx context.Context, taskID uint, newStatus, commentText string) {
	s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return nil
		}
		task.Status = newStatus
		task.Comment = fmt.Sprintf("%s\n[SD_WORKER] %s", task.Comment, commentText)
		return tx.Save(task).Error
	})
}
