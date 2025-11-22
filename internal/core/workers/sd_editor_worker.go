// Файл: internal/core/workers/sd_editor_worker.go
package workers

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
	"etalon-server/internal/infra/external"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/utils"
	api "etalon-server/internal/transport/http/dtos"
	"fmt"
	"regexp"
	"strings"
	"time"

	"etalon-server/pkg/eventbus"

	"gorm.io/gorm"
)

// SDEditorWorker определяет интерфейс для воркера, который асинхронно изменяет данные в ServiceDesk.
type SDEditorWorker interface {
	Start(ctx context.Context)
}

// sdEditorWorkerImpl реализует интерфейс SDEditorWorker.
type sdEditorWorkerImpl struct {
	logger          logger.LoggerInterface
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo
	companyRepo     company.Repository
	serverRepo      server.Repository
	workstationRepo workstation.Repository
	frRepo          fiscal.Repository
}

// NewSDEditorWorker создает новый экземпляр воркера SDEditorWorker.
func NewSDEditorWorker(
	logger logger.LoggerInterface,
	db *gorm.DB,
	bus eventbus.EventBus,
	sdClient external.ExternalSystemClient,
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
		sdClient:        sdClient,
		taskRepo:        taskRepo,
		linkRepo:        linkRepo,
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер и подписывает его на события.
func (s *sdEditorWorkerImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера SDEditorWorker")
	s.bus.Subscribe(events.ServiceDeskCreateRequested, s.handleCreateRequest)
	s.bus.Subscribe(events.ServiceDeskUpdateRequested, s.handleUpdateRequest)
}

// handleUpdateRequest обрабатывает запрос на обновление сущности в ServiceDesk.
func (s *sdEditorWorkerImpl) handleUpdateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}

	log := s.logger.With("taskID", payload.TaskID, "internalUUID", payload.EntityUUID)
	log.Info("Получен запрос на обновление сущности в ServiceDesk")

	link, err := s.linkRepo.GetByInternalID(ctx, nil, "naumen", payload.EntityUUID)
	if err != nil || link == nil {
		msg := fmt.Sprintf("Не найдена связь с ServiceDesk для сущности с внутренним ID %s", payload.EntityUUID)
		log.Error(msg, "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", msg)
		return
	}

	payloadForSD, err := s.buildUpdatePayload(ctx, payload.EntityType, payload.EntityUUID)
	if err != nil {
		log.Error("Не удалось собрать payload для обновления", "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка сборки данных для SD: %v", err))
		return
	}
	log.Info("Подготовлен payload для обновления сущности в ServiceDesk", "payload", payloadForSD)

	err = s.sdClient.UpdateEntity(ctx, link.ServiceDeskUUID, payload.EntityType, payloadForSD)
	if err != nil {
		log.Error("Ошибка при обновлении сущности в ServiceDesk", "error", err, "sent_payload", payloadForSD)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	log.Info("Сущность в ServiceDesk успешно обновлена (или выполнен dry run)")
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", "Сущность успешно обновлена в ServiceDesk.")
}

// handleCreateRequest обрабатывает запрос на создание сущности в ServiceDesk.
func (s *sdEditorWorkerImpl) handleCreateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}
	log := s.logger.With("taskID", payload.TaskID)
	log.Info("Получен запрос на создание сущности в ServiceDesk")

	var newExternalUUID string
	var err error

	switch payload.EntityType {
	case "FiscalRegister":
		newExternalUUID, err = s.createFiscalRegisterFromTask(ctx, payload.TaskID)
	default:
		err = fmt.Errorf("создание сущности типа '%s' не поддерживается", payload.EntityType)
	}

	if err != nil {
		log.Error("Ошибка при создании сущности в ServiceDesk", "error", err)
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	task, _ := s.taskRepo.GetByID(ctx, payload.TaskID)
	if task != nil {
		var internalID string
		switch task.EntityType {
		case "FiscalRegister":
			// В задаче add_equipment EntityUUID - это серийный номер. Ищем по нему.
			fr, _ := s.frRepo.FindBySerialNumber(ctx, task.EntityUUID)
			if fr != nil {
				internalID = fr.ID
			}
		}

		if internalID != "" {
			newLink := models.ExternalSystemLink{
				InternalID:      internalID,
				SystemName:      "naumen",
				ServiceDeskUUID: newExternalUUID,
				EntityType:      task.EntityType,
				LastSyncedAt:    time.Now(),
			}
			if err := s.linkRepo.Create(ctx, nil, &newLink); err != nil {
				log.Error("Критическая ошибка: сущность в SD создана, но не удалось создать для нее связь в локальной БД", "error", err)
				s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Создано в SD (extUUID: %s), но не удалось создать связь в БД!", newExternalUUID))
				return
			}
		} else {
			log.Warn("Не удалось найти локальную сущность для создания связи после создания в SD", "task_id", task.ID, "entity_type", task.EntityType, "entity_identifier", task.EntityUUID)
		}
	}

	log.Info("Сущность в ServiceDesk успешно создана и связана", "newExternalUUID", newExternalUUID)
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", fmt.Sprintf("Сущность успешно создана в ServiceDesk с UUID: %s", newExternalUUID))
}

func (s *sdEditorWorkerImpl) buildUpdatePayload(ctx context.Context, entityType, internalID string) (map[string]interface{}, error) {
	payload := make(map[string]interface{})

	switch entityType {
	case "FiscalRegister":
		fr, err := s.frRepo.GetByID(ctx, internalID)
		if err != nil || fr == nil {
			return nil, fmt.Errorf("не удалось найти ФР с ID %s в локальной БД: %w", internalID, err)
		}
		payload["RNKKT"] = utils.FormatRNKKT(utils.SafeStringDereference(fr.RNKKT))
		payload["FNNumber"] = utils.SafeStringDereference(fr.FNNumber)
		payload["FRDownloader"] = utils.SafeStringDereference(fr.FRDownloader)
		payload["FRFirmware"] = utils.SafeStringDereference(fr.FRFirmware)

		var legalName string
		if fr.LegalName != nil && *fr.LegalName != "" {
			legalName = *fr.LegalName
			if fr.INN != nil && *fr.INN != "" {
				legalName = fmt.Sprintf("%s ИНН:%s", *fr.LegalName, *fr.INN)
			}
		}
		payload["LegalName"] = legalName

		if fr.FNExpireDate != nil {
			payload["FNExpireDate"] = fr.FNExpireDate.Format(utils.TimeLayoutServiceDesk)
		}
		if fr.KKTRegDate != nil {
			payload["KKTRegDate"] = fr.KKTRegDate.Format(utils.TimeLayoutServiceDesk)
		}

		for k, v := range payload {
			if strVal, ok := v.(string); ok && strVal == "" {
				delete(payload, k)
			}
		}

	default:
		return nil, fmt.Errorf("обновление для типа сущности '%s' не поддерживается", entityType)
	}

	return payload, nil
}

func (s *sdEditorWorkerImpl) createFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return "", fmt.Errorf("ошибка получения задачи %d: %w", taskID, err)
	}

	var details struct {
		AgentData   api.AgentDataDTO `json:"agent_data"`
		EtalonOwner struct {
			ExternalID string `json:"external_id"`
		} `json:"etalon_owner"`
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return "", err
	}
	if details.EtalonOwner.ExternalID == "" {
		return "", fmt.Errorf("в деталях задачи отсутствует внешний ID владельца (etalon_owner.external_id)")
	}

	agentData := details.AgentData
	log := s.logger.With("taskID", taskID)
	log.Debug("Начало сборки payload для создания ФР")
	payload := make(map[string]interface{})

	addStringFieldToPayload(log, payload, "owner", details.EtalonOwner.ExternalID)
	addStringFieldToPayload(log, payload, "RNKKT", utils.FormatRNKKT(agentData.RNM))
	addStringFieldToPayload(log, payload, "FRSerialNumber", strings.TrimSpace(agentData.SerialNumber))
	addStringFieldToPayload(log, payload, "FNNumber", strings.TrimSpace(agentData.FNSerial))

	var legalName string
	if agentData.OrganizationName != "" && agentData.INN != "" {
		legalName = fmt.Sprintf("%s ИНН:%s", strings.TrimSpace(agentData.OrganizationName), strings.TrimSpace(agentData.INN))
	} else {
		legalName = strings.TrimSpace(agentData.OrganizationName)
	}
	addStringFieldToPayload(log, payload, "LegalName", legalName)

	if regDate := utils.ParseAgentTime(agentData.DateTimeReg); regDate != nil {
		addStringFieldToPayload(log, payload, "KKTRegDate", regDate.Format(utils.TimeLayoutServiceDesk))
	}
	if expDate := utils.ParseAgentTime(agentData.DateTimeEnd); expDate != nil {
		addStringFieldToPayload(log, payload, "FNExpireDate", expDate.Format(utils.TimeLayoutServiceDesk))
	}

	addStringFieldToPayload(log, payload, "FRDownloader", strings.TrimSpace(agentData.BootVersion))
	frFirmwareValue := utils.CalculateFRFirmware(agentData.Licenses)
	addStringFieldToPayload(log, payload, "FRFirmware", frFirmwareValue)

	modelName := strings.TrimSpace(agentData.ModelName)
	modelUUID, err := s.sdClient.FindReferenceID(ctx, "ModeliFR", modelName, false)
	if err != nil {
		return "", fmt.Errorf("не удалось найти модель ККТ '%s': %w", modelName, err)
	}
	addStringFieldToPayload(log, payload, "ModelKKT", modelUUID)

	ffdVersion := utils.FormatFFDVersion(agentData.FFDVersion)
	ffdUUID, err := s.sdClient.FindReferenceID(ctx, "FFD", ffdVersion, false)
	if err != nil {
		return "", fmt.Errorf("не удалось найти версию ФФД '%s': %w", ffdVersion, err)
	}
	addStringFieldToPayload(log, payload, "FFD", ffdUUID)

	srokMatches := srokFnRegex.FindStringSubmatch(agentData.FNExecution)
	if len(srokMatches) < 2 {
		return "", fmt.Errorf("не удалось извлечь срок ФН из '%s'", agentData.FNExecution)
	}
	srokUUID, err := s.sdClient.FindReferenceID(ctx, "SrokiFN", srokMatches[1], true)
	if err != nil {
		return "", fmt.Errorf("не удалось найти срок ФН '%s': %w", srokMatches[1], err)
	}
	addStringFieldToPayload(log, payload, "SrokFN", srokUUID)

	log.Info("Подготовлен итоговый payload для создания ФР в ServiceDesk", "payload", payload)

	response, err := s.sdClient.CreateEntity(ctx, "FiscalRegister", payload)
	if err != nil {
		return "", fmt.Errorf("ошибка создания ФР в ServiceDesk: %w", err)
	}

	newUUID, _ := response["UUID"].(string)
	return newUUID, nil
}

func (s *sdEditorWorkerImpl) updateTaskStatus(ctx context.Context, taskID uint, newStatus, commentText string) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		task, err := s.taskRepo.GetByID(ctx, taskID)
		if err != nil {
			return err
		}
		if task == nil {
			return fmt.Errorf("задача с ID %d не найдена для обновления статуса", taskID)
		}
		task.Status = newStatus
		task.Comment = fmt.Sprintf("%s\n[SD_WORKER] %s", task.Comment, commentText)
		return tx.Save(task).Error
	})
	if err != nil {
		s.logger.Error("Критическая ошибка: не удалось обновить статус задачи после операции с SD",
			"taskID", taskID,
			"newStatus", newStatus,
			"error", err,
		)
	}
}

var srokFnRegex = regexp.MustCompile(`(13|15|36)`)

func addStringFieldToPayload(log logger.LoggerInterface, payload map[string]interface{}, key, value string) {
	if value != "" {
		log.Debug("Добавление поля в payload", "поле", key, "значение", value)
		payload[key] = value
	} else {
		log.Debug("Пропуск пустого поля", "поле", key)
	}
}
