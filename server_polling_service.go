// internal/services/server_polling_service.go
package services

import (
	"context"
	"encoding/json"
	"errors"
	"etalon-server/internal/config"
	"etalon-server/internal/models"
	"etalon-server/internal/repositories"
	"etalon-server/internal/utils"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var (
	// ErrRateLimitExceeded возвращается, когда превышен лимит запросов на опрос для одного сервера.
	ErrRateLimitExceeded = errors.New("слишком много запросов на опрос сервера")
)

const (
	rateLimitCount  = 3
	rateLimitWindow = 2 * time.Minute
)

// ServerPollingService определяет интерфейс для фонового воркера опроса статусов серверов.
type ServerPollingService interface {
	Start(ctx context.Context)
	InstallLicense(ctx context.Context, serverUUID, uniqueID string) error
	PollSingleServer(ctx context.Context, serverUUID string) error // НОВЫЙ МЕТОД
}

type serverPollingServiceImpl struct {
	cfg        *config.Config
	logger     *zap.Logger
	db         *gorm.DB
	serverRepo repositories.ServerRepo
	rmsClient  utils.RMSClient

	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

// NewServerPollingService создает новый экземпляр сервиса.
func NewServerPollingService(cfg *config.Config, db *gorm.DB, serverRepo repositories.ServerRepo, rmsClient utils.RMSClient, logger *zap.Logger) ServerPollingService {
	return &serverPollingServiceImpl{
		cfg:           cfg,
		logger:        logger,
		db:            db,
		serverRepo:    serverRepo,
		rmsClient:     rmsClient,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

// PollSingleServer запускает асинхронную задачу опроса для одного сервера с проверкой rate limit.
func (s *serverPollingServiceImpl) PollSingleServer(ctx context.Context, serverUUID string) error {
	if !s.checkRateLimit(serverUUID) {
		return ErrRateLimitExceeded
	}

	server, err := s.serverRepo.GetByUUID(ctx, serverUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера из БД: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	s.logger.Info("Получен ручной запрос на опрос сервера", zap.String("uuid", serverUUID))

	// Запускаем реальную обработку в отдельной горутине, чтобы не блокировать ответ API.
	go func() {
		// Используем новый контекст, так как родительский контекст запроса может быть отменен после ответа.
		s.processServer(context.Background(), *server)
	}()

	return nil
}

// checkRateLimit проверяет, можно ли выполнить запрос для данного serverUUID.
func (s *serverPollingServiceImpl) checkRateLimit(serverUUID string) bool {
	s.rateLimiter.Lock()
	defer s.rateLimiter.Unlock()

	now := time.Now()
	limitWindowStart := now.Add(-rateLimitWindow)

	// Получаем историю запросов для этого UUID
	stamps := s.requestStamps[serverUUID]

	// Очищаем старые временные метки
	recentStamps := make([]time.Time, 0, len(stamps))
	for _, stamp := range stamps {
		if stamp.After(limitWindowStart) {
			recentStamps = append(recentStamps, stamp)
		}
	}

	// Проверяем лимит
	if len(recentStamps) >= rateLimitCount {
		s.logger.Warn("Превышен лимит запросов на опрос для сервера", zap.String("uuid", serverUUID))
		s.requestStamps[serverUUID] = recentStamps // Сохраняем очищенный список
		return false
	}

	// Добавляем текущую метку и разрешаем запрос
	recentStamps = append(recentStamps, now)
	s.requestStamps[serverUUID] = recentStamps
	return true
}

// Start запускает сервис в фоновом режиме.
// ИЗМЕНЕНИЕ: Переделано на тикер для корректного прерывания.
func (s *serverPollingServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера для опроса статусов серверов", zap.Duration("interval", 1*time.Minute))
	ticker := time.NewTicker(1 * time.Minute) // Пауза между циклами
	defer ticker.Stop()

	// Первый запуск сразу, не дожидаясь тикера
	s.runCycle(ctx)

	for {
		select {
		case <-ticker.C:
			s.runCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка воркера для опроса статусов серверов...")
			return
		}
	}
}

// runCycle выполняет один цикл работы воркера.
func (s *serverPollingServiceImpl) runCycle(ctx context.Context) {
	s.logger.Info("Начало нового цикла опроса статусов серверов...")

	servers, err := s.serverRepo.FindForPolling(ctx, s.cfg.ServerPollingBatchSize, s.cfg.ServerPollingInterval)
	if err != nil {
		s.logger.Error("Не удалось получить список серверов для опроса", zap.Error(err))
		return
	}

	if len(servers) == 0 {
		s.logger.Info("Не найдено серверов, подлежащих опросу. Цикл завершен.")
		return
	}

	s.logger.Info("Найдено серверов для обработки", zap.Int("count", len(servers)))

	for _, server := range servers {
		select {
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание цикла опроса серверов.")
			return
		default:
			s.processServer(ctx, server)
			time.Sleep(2 * time.Second)
		}
	}
	s.logger.Info("Цикл опроса статусов серверов завершен.")
}

// processServer обрабатывает один сервер.
func (s *serverPollingServiceImpl) processServer(ctx context.Context, server models.Server) {
	log := s.logger.With(zap.String("server_uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.String("server_ip", utils.SafeStringDereference(server.IP)))

	if server.IP == nil || *server.IP == "" {
		log.Warn("У сервера отсутствует IP-адрес, опрос невозможен.")
		updates := map[string]interface{}{"last_polled_at": time.Now()}
		s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updates)
		return
	}

	var url string
	parts := strings.SplitN(*server.IP, ":", 2)
	host := parts[0]
	if len(parts) == 2 && (parts[1] == "443" || strings.Contains(*server.IP, "iiko.it") || strings.Contains(*server.IP, "syrve.online")) {
		url = "https://" + host
	} else {
		url = "http://" + *server.IP
	}

	info, err := s.rmsClient.GetServerMonitoringInfo(ctx, url)

	updates := make(map[string]interface{})
	updates["last_polled_at"] = time.Now()

	if err != nil {
		log.Warn("Не удалось получить информацию о сервере", zap.String("url", url), zap.Error(err))
		// Проверяем на специфическую ошибку DNS
		if strings.Contains(err.Error(), "no such host") {
			log.Info("Обнаружена ошибка DNS lookup. Сервер будет архивирован.")
			updates["status"] = "archived"
		} else {
			// Для всех остальных ошибок просто ставим 'offline'
			updates["status"] = "offline"
		}
	} else {
		log.Info("Информация о сервере успешно получена", zap.String("state", info.ServerState), zap.String("version", info.Version))
		updates["server_name"] = info.ServerName
		updates["server_edition"] = info.Edition
		updates["server_version"] = shortenVersion(info.Version)
		status := mapServerStateToStatus(info.ServerState)
		updates["status"] = status

		if status == "license" {
			s.createLicenseTask(ctx, server)
		}
	}

	_, updateErr := s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updates)
	if updateErr != nil {
		log.Error("Не удалось обновить информацию о сервере в базе данных", zap.Error(updateErr))
	} else {
		log.Info("Информация о сервере успешно сохранена в базе данных")
	}
}

// createLicenseTask создает задачу для администратора на установку лицензии.
func (s *serverPollingServiceImpl) createLicenseTask(ctx context.Context, server models.Server) {
	log := s.logger.With(zap.String("server_uuid", *server.ServiceDeskUUID))
	taskType := "license_installation_required"
	entityUUID := *server.ServiceDeskUUID

	var existingTask models.ReconciliationTask
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ? AND status = 'new'", entityUUID, taskType).
		First(&existingTask).Error

	if err == nil {
		return
	}
	if err != gorm.ErrRecordNotFound {
		log.Error("Ошибка при поиске существующей задачи на установку лицензии", zap.Error(err))
		return
	}

	detailsMap := map[string]string{
		"serverName":      utils.SafeStringDereference(server.ServerName),
		"serverUUID":      entityUUID,
		"suggestedUnique": utils.SafeStringDereference(server.UniqueID),
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	task := models.ReconciliationTask{
		TaskType:   taskType,
		EntityType: "Server",
		EntityUUID: entityUUID,
		Details:    datatypes.JSON(detailsJSON),
		Status:     "new",
		Comment:    fmt.Sprintf("Сервер '%s' ожидает установку лицензии. Предлагаемый UniqueID: %s", utils.SafeStringDereference(server.ServerName), utils.SafeStringDereference(server.UniqueID)),
	}
	if createErr := s.db.WithContext(ctx).Create(&task).Error; createErr != nil {
		log.Error("Не удалось создать задачу на установку лицензии", zap.Error(createErr))
	} else {
		log.Info("Создана новая задача на установку лицензии")
	}
}

// InstallLicense - это метод-заглушка для ручного запуска установки лицензии.
func (s *serverPollingServiceImpl) InstallLicense(ctx context.Context, serverUUID, uniqueID string) error {
	server, err := s.serverRepo.GetByUUID(ctx, serverUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	s.logger.Info("ЗАГЛУШКА: Запущена установка лицензии",
		zap.String("server_uuid", serverUUID),
		zap.String("server_name", utils.SafeStringDereference(server.ServerName)),
		zap.String("unique_id", uniqueID),
	)

	return nil
}

// mapServerStateToStatus преобразует статус из ответа сервера в наш внутренний статус.
func mapServerStateToStatus(state string) string {
	switch state {
	case "STARTED_SUCCESSFULLY":
		return "active"
	case "WAITING_LICENSE":
		return "license"
	case "STARTING":
		return "starting"
	default:
		return "unknown"
	}
}

// shortenVersion обрезает версию до формата X.Y.Z
func shortenVersion(fullVersion string) string {
	if fullVersion == "" {
		return ""
	}
	re := regexp.MustCompile(`^(\d+\.\d+\.\d+)`)
	matches := re.FindStringSubmatch(fullVersion)
	if len(matches) > 1 {
		return matches[1]
	}
	return fullVersion
}
