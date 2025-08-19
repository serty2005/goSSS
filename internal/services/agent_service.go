package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/api"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"fmt"
	"regexp"
	"strings"
	"time"

	"unicode"

	"github.com/asaskevich/govalidator"
	"go.uber.org/zap"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"gorm.io/datatypes"
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
	logger        *zap.Logger
	agentRepo     repositories.AgentRepo
	companyRepo   repositories.CompanyRepo
	reconcilerSvc ReconcilerService
	db            *gorm.DB // Для транзакций и создания задач
}

// NewAgentService создает новый экземпляр сервиса агентов.
func NewAgentService(logger *zap.Logger, agentRepo repositories.AgentRepo, companyRepo repositories.CompanyRepo, reconcilerSvc ReconcilerService, db *gorm.DB) AgentService {
	return &agentServiceImpl{
		logger:        logger,
		agentRepo:     agentRepo,
		companyRepo:   companyRepo,
		reconcilerSvc: reconcilerSvc,
		db:            db,
	}
}

// RegisterAgent обрабатывает запрос на регистрацию нового агента.
// RegisterAgent обрабатывает запрос на регистрацию нового агента.
func (s *agentServiceImpl) RegisterAgent(ctx context.Context, req *api.RegistrationRequestDTO) (*models.Agent, error) {
	// 1. Проверяем, не существует ли уже такой агент
	existingAgent, err := s.agentRepo.GetByUUID(ctx, req.AgentUUID)
	if err != nil {
		return nil, fmt.Errorf("ошибка проверки существования агента: %w", err)
	}
	if existingAgent != nil {
		return nil, ErrAgentAlreadyExists
	}

	// 2. Вызываем ReconcilerService для определения владельца
	// ИСПРАВЛЕНИЕ: Сигнатура вызова изменена (убраны votes).
	ownerUUID, _, _, err := s.reconcilerSvc.ProcessAgentData(ctx, &req.InitialData)
	if err != nil {
		s.logger.Error("Ошибка при обработке данных для определения владельца", zap.Error(err))
	}

	agent := &models.Agent{
		UUID:          req.AgentUUID,
		Hostname:      req.Hostname,
		Version:       req.AgentVersion,
		LastHeartbeat: time.Now(),
		Type:          "workstation", // Пока хардкод, в будущем можно определять
	}

	// 3. Логика на основе результата определения владельца
	if ownerUUID == "" {
		s.logger.Warn("Владелец для нового агента не определен. Создание задачи.", zap.String("agent_uuid", req.AgentUUID))
		agent.Status = models.StatusPendingOwner
		if err := s.createTaskForUndefinedOwner(ctx, req); err != nil {
			return nil, err
		}
	} else {
		s.logger.Info("Владелец для нового агента определен", zap.String("agent_uuid", req.AgentUUID), zap.String("owner_uuid", ownerUUID))
		agent.Status = models.StatusPendingZabbix
		agent.OwnerServiceDeskUUID = ownerUUID

		// Генерируем предварительное имя для Zabbix
		zabbixHostname, err := s.generateZabbixHostname(ctx, ownerUUID, req.InitialData.Hostname)
		if err != nil {
			return nil, fmt.Errorf("ошибка генерации имени хоста Zabbix: %w", err)
		}
		agent.ZabbixHostname = zabbixHostname
	}

	// 4. Сохраняем агента в БД
	if err := s.agentRepo.Create(ctx, agent); err != nil {
		return nil, fmt.Errorf("не удалось создать агента в БД: %w", err)
	}

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

	// Обновляем heartbeat
	agent.LastHeartbeat = time.Now()
	if data.AgentVersion != "" {
		agent.Version = data.AgentVersion
	}
	if err := s.agentRepo.Update(ctx, agent); err != nil {
		s.logger.Error("Не удалось обновить heartbeat агента", zap.String("uuid", agentUUID), zap.Error(err))
		// Не возвращаем ошибку, чтобы сверка все равно прошла
	}

	// Запускаем сверку данных
	_, _, _, err = s.reconcilerSvc.ProcessAgentData(ctx, data)
	if err != nil {
		return fmt.Errorf("ошибка сверки данных агента: %w", err)
	}

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

// createTaskForUndefinedOwner создает задачу для администратора.
func (s *agentServiceImpl) createTaskForUndefinedOwner(ctx context.Context, req *api.RegistrationRequestDTO) error {
	details, _ := json.Marshal(req)
	task := models.ReconciliationTask{
		TaskType:   "agent_owner_required",
		EntityType: "Agent",
		EntityUUID: req.AgentUUID,
		Details:    datatypes.JSON(details),
		Status:     "new",
		Comment:    fmt.Sprintf("Требуется вручную определить и привязать владельца для нового агента с хостом %s.", req.Hostname),
	}
	return s.db.WithContext(ctx).Create(&task).Error
}

// generateZabbixHostname создает имя хоста по формату {$COMPANY_NAME_ENG_SHORT}-{$DeviceNameFromSD}-{$INNER_COMPANY_ID}
func (s *agentServiceImpl) generateZabbixHostname(ctx context.Context, ownerUUID, agentHostname string) (string, error) {
	company, err := s.companyRepo.GetByUUID(ctx, ownerUUID)
	if err != nil || company == nil {
		return "", fmt.Errorf("не удалось найти компанию-владельца по UUID %s", ownerUUID)
	}

	// 1. $COMPANY_NAME_ENG_SHORT
	companyShortName := s.transliterate(*company.Title)

	// 2. $DeviceNameFromSD
	// На этапе регистрации у нас еще нет точной привязки к Workstation, используем hostname агента
	deviceName := agentHostname
	if govalidator.IsDNSName(deviceName) {
		deviceName = strings.Split(deviceName, ".")[0]
	}
	deviceName = strings.ToUpper(deviceName)

	// 3. $INNER_COMPANY_ID
	count, err := s.agentRepo.CountByOwnerUUID(ctx, ownerUUID)
	if err != nil {
		return "", fmt.Errorf("не удалось посчитать агентов для компании %s: %w", ownerUUID, err)
	}
	innerID := fmt.Sprintf("%02d", count+1) // +1, так как текущий агент еще не сохранен

	return fmt.Sprintf("%s-%s-%s", companyShortName, deviceName, innerID), nil
}

// transliterate преобразует кириллический текст в латиницу.
func (s *agentServiceImpl) transliterate(text string) string {
	// Простая транслитерация
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, text)
	result = strings.ToLower(result)

	// Заменяем специфичные русские буквы и символы
	var replacements = map[string]string{
		" ": "-", "ъ": "", "ь": "", "ы": "y", "і": "i", "ї": "i", "є": "e",
		"а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ё": "e",
		"ж": "zh", "з": "z", "и": "i", "й": "y", "к": "k", "л": "l", "м": "m",
		"н": "n", "о": "o", "п": "p", "р": "r", "с": "s", "т": "t", "у": "u",
		"ф": "f", "х": "h", "ц": "c", "ч": "ch", "ш": "sh", "щ": "sch",
		"ю": "yu", "я": "ya",
	}

	for rus, lat := range replacements {
		result = strings.ReplaceAll(result, rus, lat)
	}

	// Удаляем все неалфавитно-цифровые символы, кроме дефиса
	reg, err := regexp.Compile("[^a-z0-9-]+")
	if err != nil {
		s.logger.Error("Ошибка компиляции regex для транслитерации", zap.Error(err))
		return "unknown"
	}
	return reg.ReplaceAllString(result, "")
}
