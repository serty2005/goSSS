// internal/services/sd_editor_service.go
package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/api"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"regexp"
	"strings"

	"go.uber.org/zap"
)

// SDEditorService определяет интерфейс для сервиса, изменяющего данные в ServiceDesk.
type SDEditorService interface {
	CreateFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error)
}

type sdEditorServiceImpl struct {
	logger   *zap.Logger
	sdClient ServiceDeskClient
	taskRepo repositories.TaskRepo
}

// NewSDEditorService создает новый экземпляр сервиса.
func NewSDEditorService(logger *zap.Logger, sdClient ServiceDeskClient, taskRepo repositories.TaskRepo) SDEditorService {
	return &sdEditorServiceImpl{logger, sdClient, taskRepo}
}

var srokFnRegex = regexp.MustCompile(`(13|15|36)`)

// CreateFiscalRegisterFromTask создает ФР в ServiceDesk на основе данных из задачи.
func (s *sdEditorServiceImpl) CreateFiscalRegisterFromTask(ctx context.Context, taskID uint) (string, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("ошибка получения задачи: %w", err)
	}
	if task == nil {
		return "", ErrTaskNotFound
	}
	if task.TaskType != "add_equipment" || task.EntityType != "FiscalRegister" {
		return "", fmt.Errorf("задача %d не является задачей на добавление ФР", taskID)
	}

	var details struct {
		AgentData       api.AgentDataDTO `json:"agent_data"`
		EtalonOwnerUUID string           `json:"etalon_owner_uuid"`
	}
	if err := json.Unmarshal(task.Details, &details); err != nil {
		return "", fmt.Errorf("не удалось извлечь детали из задачи %d: %w", taskID, err)
	}

	agentData := details.AgentData
	log := s.logger.With(zap.Uint("taskID", taskID)) // Логгер с контекстом задачи

	// --- Подготовка данных для создания ---
	log.Debug("Начало сборки payload для создания ФР")
	payload := make(map[string]interface{})

	// 1. Простые поля
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

	// 2. Новые поля FRFirmware и FRDownloader
	addStringFieldToPayload(log, payload, "FRDownloader", strings.TrimSpace(agentData.BootVersion))
	frFirmwareValue := utils.CalculateFRFirmware(agentData.Licenses)
	addStringFieldToPayload(log, payload, "FRFirmware", frFirmwareValue)

	// 3. Поля из справочников
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

	// 4. Владелец
	addStringFieldToPayload(log, payload, "owner", details.EtalonOwnerUUID)

	// --- Логирование и создание сущности ---
	log.Info("Подготовлен итоговый payload для создания ФР в ServiceDesk", zap.Any("payload", payload))

	response, err := s.sdClient.CreateEntity(ctx, "objectBase$FR", payload)
	if err != nil {
		return "", fmt.Errorf("ошибка создания ФР в ServiceDesk: %w", err)
	}

	newUUID, _ := response["UUID"].(string)
	log.Info("Фискальный регистратор успешно создан в ServiceDesk (или выполнен dry run)", zap.String("newUUID", newUUID))
	return newUUID, nil
}

// addStringFieldToPayload - хелпер для логирования и добавления непустых строковых полей в payload.
func addStringFieldToPayload(log *zap.Logger, payload map[string]interface{}, key, value string) {
	if value != "" {
		log.Debug("Добавление поля в payload", zap.String("поле", key), zap.String("значение", value))
		payload[key] = value
	} else {
		log.Debug("Пропуск пустого поля", zap.String("поле", key))
	}
}
