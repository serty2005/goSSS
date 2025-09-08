// internal/services/sd_editor_service.go
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/core/events"
	"etalon-server/internal/external"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SDEditorService определяет интерфейс для сервиса, который асинхронно изменяет данные в ServiceDesk.
type SDEditorService interface {
	Start(ctx context.Context)
}

// sdEditorServiceImpl реализует интерфейс SDEditorService.
type sdEditorServiceImpl struct {
	logger          *zap.Logger
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        external.ExternalSystemClient
	taskRepo        repositories.TaskRepo
	linkRepo        repositories.LinkRepo // Новая зависимость
	companyRepo     repositories.CompanyRepo
	serverRepo      repositories.ServerRepo
	workstationRepo repositories.WorkstationRepo
	frRepo          repositories.FiscalRegisterRepo
}

// NewSDEditorService создает новый экземпляр воркера SDEditorService.
func NewSDEditorService(
	logger *zap.Logger,
	db *gorm.DB,
	bus eventbus.EventBus,
	sdClient external.ExternalSystemClient,
	taskRepo repositories.TaskRepo,
	linkRepo repositories.LinkRepo, // Новая зависимость
	companyRepo repositories.CompanyRepo,
	serverRepo repositories.ServerRepo,
	workstationRepo repositories.WorkstationRepo,
	frRepo repositories.FiscalRegisterRepo,
) SDEditorService {
	return &sdEditorServiceImpl{
		logger:          logger,
		db:              db,
		bus:             bus,
		sdClient:        sdClient,
		taskRepo:        taskRepo,
		linkRepo:        linkRepo, // Инициализация
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер и подписывает его на события.
func (s *sdEditorServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера SDEditorService")
	s.bus.Subscribe(events.ServiceDeskCreateRequested, s.handleCreateRequest)
	s.bus.Subscribe(events.ServiceDeskUpdateRequested, s.handleUpdateRequest)
}

// handleUpdateRequest обрабатывает запрос на обновление сущности в ServiceDesk.
func (s *sdEditorServiceImpl) handleUpdateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}

	log := s.logger.With(zap.Uint("taskID", payload.TaskID), zap.String("internalUUID", payload.EntityUUID))
	log.Info("Получен запрос на обновление сущности в ServiceDesk")

	// 1. Находим внешний ID по внутреннему ID.
	link, err := s.linkRepo.GetByInternalID(ctx, nil, "naumen", payload.EntityUUID)
	if err != nil || link == nil {
		msg := fmt.Sprintf("Не найдена связь с ServiceDesk для сущности с внутренним ID %s", payload.EntityUUID)
		log.Error(msg, zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", msg)
		return
	}

	// 2. Собираем payload для ServiceDesk на основе эталонных данных из нашей БД.
	payloadForSD, err := s.buildUpdatePayload(ctx, payload.EntityType, payload.EntityUUID)
	if err != nil {
		log.Error("Не удалось собрать payload для обновления", zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка сборки данных для SD: %v", err))
		return
	}
	log.Info("Подготовлен payload для обновления сущности в ServiceDesk", zap.Any("payload", payloadForSD))

	// 3. Выполняем обновление, используя внешний ID.
	err = s.sdClient.UpdateEntity(ctx, link.ExternalID, payload.EntityType, payloadForSD)
	if err != nil {
		log.Error("Ошибка при обновлении сущности в ServiceDesk", zap.Error(err), zap.Any("sent_payload", payloadForSD))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	// 4. В случае успеха, обновляем статус задачи.
	log.Info("Сущность в ServiceDesk успешно обновлена (или выполнен dry run)")
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", "Сущность успешно обновлена в ServiceDesk.")
}

// handleCreateRequest обрабатывает запрос на создание сущности в ServiceDesk.
func (s *sdEditorServiceImpl) handleCreateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		return
	}
	log := s.logger.With(zap.Uint("taskID", payload.TaskID))
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
		log.Error("Ошибка при создании сущности в ServiceDesk", zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	// После успешного создания в SD, нам нужно связать наш внутренний объект с новым внешним.
	task, _ := s.taskRepo.GetByID(ctx, payload.TaskID)
	if task != nil {
		// EntityUUID в задаче типа add_equipment - это уникальный идентификатор оборудования (например, серийный номер).
		// Нам нужно найти внутренний объект по этому идентификатору.
		var internalID string
		switch task.EntityType {
		case "FiscalRegister":
			fr, _ := s.frRepo.FindBySerialNumber(ctx, task.EntityUUID)
			if fr != nil {
				internalID = fr.ID
			}
		}

		if internalID != "" {
			newLink := models.ExternalSystemLink{
				InternalID: internalID, SystemName: "naumen", ExternalID: newExternalUUID,
				EntityType: task.EntityType, LastSyncedAt: time.Now(),
			}
			if err := s.linkRepo.Create(ctx, nil, &newLink); err != nil {
				log.Error("Критическая ошибка: сущность в SD создана, но не удалось создать для нее связь в локальной БД", zap.Error(err))
				// Статус задачи не меняем на resolved, чтобы оператор увидел проблему
				s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Создано в SD (extUUID: %s), но не удалось создать связь в БД!", newExternalUUID))
				return
			}
		}
	}

	log.Info("Сущность в ServiceDesk успешно создана и связана", zap.String("newExternalUUID", newExternalUUID))
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", fmt.Sprintf("Сущность успешно создана в ServiceDesk с UUID: %s", newExternalUUID))
}

// buildUpdatePayload формирует map[string]interface{} для обновления сущности в SD.
func (s *sdEditorServiceImpl) buildUpdatePayload(ctx context.Context, entityType, internalID string) (map[string]interface{}, error) {
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

// createFiscalRegisterFromTask извлекает данные из задачи и создает ФР в ServiceDesk.
func (s *sdEditorServiceImpl) createFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return "", fmt.Errorf("ошибка получения задачи %d: %w", taskID, err)
	}

	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_uuid"` // Здесь все еще внешний ID владельца
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return "", err
	}

	agentData := details.AgentData
	log := s.logger.With(zap.Uint("taskID", taskID))
	log.Debug("Начало сборки payload для создания ФР")
	payload := make(map[string]interface{})

	addStringFieldToPayload(log, payload, "owner", details.EtalonOwnerUUID)

	// 1. Простые текстовые и временные поля
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

	// 2. Новые поля для прошивки и загрузчика
	addStringFieldToPayload(log, payload, "FRDownloader", strings.TrimSpace(agentData.BootVersion))
	frFirmwareValue := utils.CalculateFRFirmware(agentData.Licenses)
	addStringFieldToPayload(log, payload, "FRFirmware", frFirmwareValue)

	// 3. Поля, требующие поиска в справочниках ServiceDesk
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

	// 4. Владелец сущности
	addStringFieldToPayload(log, payload, "owner", details.EtalonOwnerUUID)

	log.Info("Подготовлен итоговый payload для создания ФР в ServiceDesk", zap.Any("payload", payload))

	// 5. Вызов клиента для создания сущности
	response, err := s.sdClient.CreateEntity(ctx, "FiscalRegister", payload)
	if err != nil {
		return "", fmt.Errorf("ошибка создания ФР в ServiceDesk: %w", err)
	}

	newUUID, _ := response["UUID"].(string)
	return newUUID, nil
}

// updateTaskStatus обновляет статус и комментарий задачи. Выполняется в отдельной транзакции
// для обеспечения атомарности и независимости от основного контекста операции.
func (s *sdEditorServiceImpl) updateTaskStatus(ctx context.Context, taskID uint, newStatus, commentText string) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Используем .WithContext(ctx) внутри транзакции для передачи таймаутов, если они есть
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
			zap.Uint("taskID", taskID),
			zap.String("newStatus", newStatus),
			zap.Error(err),
		)
	}
}

// srokFnRegex - регулярное выражение для извлечения срока действия ФН (13, 15 или 36 месяцев).
var srokFnRegex = regexp.MustCompile(`(13|15|36)`)

// addStringFieldToPayload - хелпер для логирования и добавления непустых строковых полей в payload.
func addStringFieldToPayload(log *zap.Logger, payload map[string]interface{}, key, value string) {
	if value != "" {
		log.Debug("Добавление поля в payload", zap.String("поле", key), zap.String("значение", value))
		payload[key] = value
	} else {
		log.Debug("Пропуск пустого поля", zap.String("поле", key))
	}
}
