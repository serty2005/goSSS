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

	// --- Подготовка данных для создания ---
	payload := make(map[string]interface{})

	// 1. Простые поля
	payload["RNKKT"] = utils.FormatRNKKT(agentData.RNM)
	payload["FRSerialNumber"] = strings.TrimSpace(agentData.SerialNumber)
	payload["FNNumber"] = strings.TrimSpace(agentData.FNSerial)

	if agentData.OrganizationName != "" && agentData.INN != "" {
		payload["LegalName"] = fmt.Sprintf("%s ИНН:%s", strings.TrimSpace(agentData.OrganizationName), strings.TrimSpace(agentData.INN))
	} else {
		payload["LegalName"] = strings.TrimSpace(agentData.OrganizationName)
	}

	if regDate := utils.ParseAgentTime(agentData.DateTimeReg); regDate != nil {
		payload["KKTRegDate"] = regDate.Format(utils.TimeLayoutServiceDesk)
	}
	if expDate := utils.ParseAgentTime(agentData.DateTimeEnd); expDate != nil {
		payload["FNExpireDate"] = expDate.Format(utils.TimeLayoutServiceDesk)
	}

	// 2. Поля из справочников (теперь с ручным форматированием)
	modelName := strings.TrimSpace(agentData.ModelName)
	modelUUID, err := s.sdClient.FindReferenceID(ctx, "ModeliFR", modelName, false)
	if err != nil {
		return "", fmt.Errorf("не удалось найти модель ККТ '%s': %w", modelName, err)
	}
	payload["ModelKKT"] = modelUUID

	ffdVersion := utils.FormatFFDVersion(agentData.FFDVersion)
	ffdUUID, err := s.sdClient.FindReferenceID(ctx, "FFD", ffdVersion, false)
	if err != nil {
		return "", fmt.Errorf("не удалось найти версию ФФД '%s': %w", ffdVersion, err)
	}
	payload["FFD"] = ffdUUID

	srokMatches := srokFnRegex.FindStringSubmatch(agentData.FNExecution)
	if len(srokMatches) < 2 {
		return "", fmt.Errorf("не удалось извлечь срок ФН из '%s'", agentData.FNExecution)
	}
	srokUUID, err := s.sdClient.FindReferenceID(ctx, "SrokiFN", srokMatches[1], true)
	if err != nil {
		return "", fmt.Errorf("не удалось найти срок ФН '%s': %w", srokMatches[1], err)
	}
	payload["SrokFN"] = srokUUID

	// 3. Владелец
	payload["owner"] = details.EtalonOwnerUUID

	// --- Логирование и создание сущности ---
	s.logger.Info("Подготовлен payload для создания ФР в ServiceDesk",
		zap.Uint("taskID", taskID),
		zap.Any("payload", payload),
	)

	response, err := s.sdClient.CreateEntity(ctx, "objectBase$FR", payload)
	if err != nil {
		return "", fmt.Errorf("ошибка создания ФР в ServiceDesk: %w", err)
	}

	newUUID, _ := response["UUID"].(string)
	s.logger.Info("Фискальный регистратор успешно создан в ServiceDesk (или выполнен dry run)",
		zap.String("newUUID", newUUID),
	)
	return newUUID, nil
}
