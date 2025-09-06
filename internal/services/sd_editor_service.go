// internal/services/sd_editor_service.go
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/core/events"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"etalon-server/pkg/eventbus"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SDEditorService определяет интерфейс для сервиса, который асинхронно изменяет данные в ServiceDesk.
// Он работает как фоновый воркер, слушающий события из шины.
type SDEditorService interface {
	Start(ctx context.Context)
}

// sdEditorServiceImpl реализует интерфейс SDEditorService.
type sdEditorServiceImpl struct {
	logger          *zap.Logger
	db              *gorm.DB
	bus             eventbus.EventBus
	sdClient        ServiceDeskClient
	taskRepo        repositories.TaskRepo
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
	sdClient ServiceDeskClient,
	taskRepo repositories.TaskRepo,
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
		companyRepo:     companyRepo,
		serverRepo:      serverRepo,
		workstationRepo: workstationRepo,
		frRepo:          frRepo,
	}
}

// Start запускает воркер и подписывает его на события создания и обновления сущностей в ServiceDesk.
func (s *sdEditorServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера SDEditorService")
	s.bus.Subscribe(events.ServiceDeskCreateRequested, s.handleCreateRequest)
	s.bus.Subscribe(events.ServiceDeskUpdateRequested, s.handleUpdateRequest)
}

// handleUpdateRequest обрабатывает событие запроса на обновление сущности в ServiceDesk.
func (s *sdEditorServiceImpl) handleUpdateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		s.logger.Error("Некорректная полезная нагрузка для события ServiceDeskUpdateRequested")
		return
	}

	log := s.logger.With(zap.Uint("taskID", payload.TaskID), zap.String("entityUUID", payload.EntityUUID))
	log.Info("Получен запрос на обновление сущности в ServiceDesk")

	// 1. Собираем payload для ServiceDesk на основе эталонных данных из нашей БД.
	payloadForSD, metaClass, err := s.buildUpdatePayload(ctx, payload.EntityType, payload.EntityUUID)
	if err != nil {
		log.Error("Не удалось собрать payload для обновления", zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка сборки данных для SD: %v", err))
		return
	}

	// 2. Вызываем клиент ServiceDesk для выполнения обновления.
	err = s.sdClient.UpdateEntity(ctx, metaClass, payload.EntityUUID, payloadForSD)
	if err != nil {
		log.Error("Ошибка при обновлении сущности в ServiceDesk", zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	// 3. В случае успеха, обновляем статус задачи на "resolved".
	log.Info("Сущность в ServiceDesk успешно обновлена (или выполнен dry run)")
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", "Сущность успешно обновлена в ServiceDesk.")
}

// handleCreateRequest обрабатывает событие запроса на создание сущности в ServiceDesk.
func (s *sdEditorServiceImpl) handleCreateRequest(ctx context.Context, event eventbus.Event) {
	payload, ok := event.Payload.(events.ServiceDeskModificationPayload)
	if !ok {
		s.logger.Error("Некорректная полезная нагрузка для события ServiceDeskCreateRequested")
		return
	}

	log := s.logger.With(zap.Uint("taskID", payload.TaskID))
	log.Info("Получен запрос на создание сущности в ServiceDesk")

	var newUUID string
	var err error

	// В зависимости от типа сущности, вызываем соответствующий метод для сборки данных и создания.
	switch payload.EntityType {
	case "FiscalRegister":
		newUUID, err = s.createFiscalRegisterFromTask(ctx, payload.TaskID)
	// Сюда можно будет добавить другие типы сущностей в будущем
	// case "Workstation":
	// 	newUUID, err = s.createWorkstationFromTask(ctx, payload.TaskID)
	default:
		err = fmt.Errorf("создание сущности типа '%s' не поддерживается", payload.EntityType)
	}

	if err != nil {
		log.Error("Ошибка при создании сущности в ServiceDesk", zap.Error(err))
		s.updateTaskStatus(ctx, payload.TaskID, "sd_error", fmt.Sprintf("Ошибка API ServiceDesk: %v", err))
		return
	}

	log.Info("Сущность в ServiceDesk успешно создана (или выполнен dry run)", zap.String("newUUID", newUUID))
	s.updateTaskStatus(ctx, payload.TaskID, "resolved", fmt.Sprintf("Сущность успешно создана в ServiceDesk с UUID: %s", newUUID))
}

// buildUpdatePayload формирует map[string]interface{} для обновления сущности в SD.
// Источником данных ("эталоном") служит локальная база данных.
func (s *sdEditorServiceImpl) buildUpdatePayload(ctx context.Context, entityType, entityUUID string) (map[string]interface{}, string, error) {
	payload := make(map[string]interface{})
	var metaClass string

	switch entityType {
	case "FiscalRegister":
		metaClass = "objectBase$FR"
		fr, err := s.frRepo.GetByUUID(ctx, entityUUID)
		if err != nil || fr == nil {
			return nil, "", fmt.Errorf("не удалось найти ФР с UUID %s в локальной БД: %w", entityUUID, err)
		}
		// Собираем payload на основе эталонных полей
		addStringFieldToPayload(s.logger, payload, "RNKKT", utils.FormatRNKKT(utils.SafeStringDereference(fr.RNKKT)))
		addStringFieldToPayload(s.logger, payload, "FRFirmware", utils.SafeStringDereference(fr.FRFirmware))
		addStringFieldToPayload(s.logger, payload, "FRDownloader", utils.SafeStringDereference(fr.FRDownloader))
		if fr.FNExpireDate != nil {
			addStringFieldToPayload(s.logger, payload, "FNExpireDate", fr.FNExpireDate.Format(utils.TimeLayoutServiceDesk))
		}
	// case "Server":
	// 	metaClass = "objectBase$Server"
	// 	...
	default:
		return nil, "", fmt.Errorf("обновление для типа сущности '%s' не поддерживается", entityType)
	}

	return payload, metaClass, nil
}

// createFiscalRegisterFromTask извлекает данные из задачи и создает ФР в ServiceDesk.
func (s *sdEditorServiceImpl) createFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("ошибка получения задачи: %w", err)
	}
	if task == nil {
		return "", ErrTaskNotFound
	}

	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_uuid"`
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return "", fmt.Errorf("не удалось извлечь детали из задачи %d: %w", taskID, err)
	}

	agentData := details.AgentData
	log := s.logger.With(zap.Uint("taskID", taskID))

	log.Debug("Начало сборки payload для создания ФР")
	payload := make(map[string]interface{})

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
	response, err := s.sdClient.CreateEntity(ctx, "objectBase$FR", payload)
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
