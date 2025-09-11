package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"fmt"
	"time"

	"gorm.io/gorm"
)

var (
	ErrAgentNotFound      = errors.New("агент не найден")
	ErrAgentAlreadyExists = errors.New("агент с таким UUID уже существует")
	ErrOwnerNotDetermined = errors.New("не удалось определить владельца для агента")
)

// AgentService определяет интерфейс для бизнес-логики управления агентами.
type AgentService interface {
	RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error)
	ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) error
	GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error)
}

type agentServiceImpl struct {
	logger      logger.LoggerInterface
	agentRepo   repositories.AgentRepo
	companyRepo repositories.CompanyRepo
	db          *gorm.DB
	bus         eventbus.EventBus
}

// NewAgentService создает новый экземпляр сервиса агентов.
func NewAgentService(logger logger.LoggerInterface, agentRepo repositories.AgentRepo, companyRepo repositories.CompanyRepo, db *gorm.DB, bus eventbus.EventBus) AgentService {
	return &agentServiceImpl{
		logger:      logger,
		agentRepo:   agentRepo,
		companyRepo: companyRepo,
		db:          db,
		bus:         bus,
	}
}

// RegisterAgent обрабатывает запрос на регистрацию нового агента.
func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error) {
	existingAgent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки существования агента: %w", err)
	}
	if existingAgent != nil {
		return nil, ErrAgentAlreadyExists
	}

	// Создаем "пустого" агента в статусе ожидания.
	agent := &models.Agent{
		UUID:          req.AgentUUID,
		Hostname:      req.Hostname,
		Version:       req.AgentVersion,
		LastHeartbeat: time.Now(),
		Type:          "workstation",
		Status:        models.StatusPendingOwner,
	}

	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, fmt.Errorf("не удалось создать агента в БД: %w", err)
	}

	payload := events.AgentDataPayload{
		Source: req.AgentUUID,
		Data:   req.InitialData,
	}

	// Публикуем событие для Оркестратора, чтобы он запустил логику сверки и определения владельца.
	s.bus.Publish(eventbus.Event{
		Type:    events.AgentDataReceived,
		Payload: payload,
	})
	s.logger.Info("Новый агент зарегистрирован, событие на обработку данных отправлено", "uuid", req.AgentUUID)

	return agent, nil
}

// ProcessData обрабатывает данные от уже зарегистрированного агента.
func (s *agentServiceImpl) ProcessData(ctx context.Context, agentUUID string, data *api.AgentDataDTO) error {
	agent, err := s.agentRepo.GetByUUID(ctx, agentUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения агента: %w", err)
	}
	if agent == nil {
		return ErrAgentNotFound
	}

	agent.LastHeartbeat = time.Now()
	if data.AgentVersion != "" {
		agent.Version = data.AgentVersion
	}
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		s.logger.Error("Не удалось обновить heartbeat агента", "uuid", agentUUID, "error", err)
	}

	payload := events.AgentDataPayload{
		Source: agentUUID,
		Data:   *data,
	}
	// Просто публикуем событие, вся логика сверки  выполняeтся в Оркестраторе.
	s.bus.Publish(eventbus.Event{
		Type:    events.AgentDataReceived,
		Payload: payload,
	})
	s.logger.Info("Данные от агента получены, событие на обработку отправлено", "uuid", agentUUID)

	return nil
}

// GetAgentConfig возвращает конфигурацию для агента, если он активен.
func (s *agentServiceImpl) GetAgentConfig(ctx context.Context, uuid string) (*api.AgentConfigDTO, error) {
	agent, err := s.agentRepo.GetByUUID(ctx, uuid)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения агента: %w", err)
	}
	if agent == nil || agent.Status != models.StatusActive {
		// Если агент не найден или его регистрация не завершена, возвращаем ошибку
		return nil, ErrAgentNotFound
	}

	// Распарсим JSON из БД в DTO
	var configDTO api.AgentConfigDTO
	if agent.Config != nil {
		if err := json.Unmarshal(agent.Config, &configDTO); err != nil {
			return nil, fmt.Errorf("не удалось распарсить конфигурацию агента из БД: %w", err)
		}
	} else {
		// Этого быть не должно, если статус active, но на всякий случай
		return nil, errors.New("у активного агента отсутствует конфигурация")
	}

	return &configDTO, nil
}

// // createTaskForUndefinedOwner создает задачу для администратора.
// func (s *agentServiceImpl) createTaskForUndefinedOwner(ctx context.Context, req *api.RegistrationRequestDTO) error {
// 	details, _ := json.Marshal(req)
// 	task := models.ReconciliationTask{
// 		TaskType:   "agent_owner_required",
// 		EntityType: "Agent",
// 		EntityUUID: req.AgentUUID,
// 		Details:    datatypes.JSON(details),
// 		Status:     "new",
// 		Comment:    fmt.Sprintf("Требуется вручную определить и привязать владельца для нового агента с хостом %s.", req.Hostname),
// 	}
// 	return s.db.WithContext(ctx).Create(&task).Error
// }

// // generateZabbixHostname создает имя хоста по формату {$COMPANY_NAME_ENG_SHORT}-{$DeviceNameFromSD}-{$INNER_COMPANY_ID}
// func (s *agentServiceImpl) generateZabbixHostname(ctx context.Context, ownerUUID, agentHostname string) (string, error) {
// 	company, err := s.companyRepo.GetByUUID(ctx, ownerUUID)
// 	if err != nil || company == nil {
// 		return "", fmt.Errorf("не удалось найти компанию-владельца по UUID %s", ownerUUID)
// 	}

// 	// 1. $COMPANY_NAME_ENG_SHORT
// 	companyShortName := s.transliterate(*company.Title)

// 	// 2. $DeviceNameFromSD
// 	// На этапе регистрации у нас еще нет точной привязки к Workstation, используем hostname агента
// 	deviceName := agentHostname
// 	if govalidator.IsDNSName(deviceName) {
// 		deviceName = strings.Split(deviceName, ".")[0]
// 	}
// 	deviceName = strings.ToUpper(deviceName)

// 	// 3. $INNER_COMPANY_ID
// 	count, err := s.agentRepo.CountByOwnerUUID(ctx, ownerUUID)
// 	if err != nil {
// 		return "", fmt.Errorf("не удалось посчитать агентов для компании %s: %w", ownerUUID, err)
// 	}
// 	innerID := fmt.Sprintf("%02d", count+1) // +1, так как текущий агент еще не сохранен

// 	return fmt.Sprintf("%s-%s-%s", companyShortName, deviceName, innerID), nil
// }

// // transliterate преобразует кириллический текст в латиницу.
// func (s *agentServiceImpl) transliterate(text string) string {
// 	// Простая транслитерация
// 	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
// 	result, _, _ := transform.String(t, text)
// 	result = strings.ToLower(result)

// 	// Заменяем специфичные русские буквы и символы
// 	var replacements = map[string]string{
// 		" ": "-", "ъ": "", "ь": "", "ы": "y", "і": "i", "ї": "i", "є": "e",
// 		"а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ё": "e",
// 		"ж": "zh", "з": "z", "и": "i", "й": "y", "к": "k", "л": "l", "м": "m",
// 		"н": "n", "о": "o", "п": "p", "р": "r", "с": "s", "т": "t", "у": "u",
// 		"ф": "f", "х": "h", "ц": "c", "ч": "ch", "ш": "sh", "щ": "sch",
// 		"ю": "yu", "я": "ya",
// 	}

// 	for rus, lat := range replacements {
// 		result = strings.ReplaceAll(result, rus, lat)
// 	}

// 	// Удаляем все неалфавитно-цифровые символы, кроме дефиса
// 	reg, err := regexp.Compile("[^a-z0-9-]+")
// 	if err != nil {
// 		s.logger.Error("Ошибка компиляции regex для транслитерации", zap.Error(err))
// 		return "unknown"
// 	}
// 	return reg.ReplaceAllString(result, "")
// }
