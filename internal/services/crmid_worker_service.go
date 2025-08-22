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
	"net"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	"go.uber.org/zap"
)

// CRMidWorkerService определяет интерфейс для фонового воркера обогащения CRMid.
type CRMidWorkerService interface {
	Start(ctx context.Context)
}

type crmidWorkerServiceImpl struct {
	cfg        *config.Config
	logger     *zap.Logger
	db         *gorm.DB
	serverRepo repositories.ServerRepo
	rmsClient  utils.RMSClient
}

// NewCRMidWorkerService создает новый экземпляр сервиса.
func NewCRMidWorkerService(cfg *config.Config, logger *zap.Logger, db *gorm.DB, serverRepo repositories.ServerRepo, rmsClient utils.RMSClient) CRMidWorkerService {
	return &crmidWorkerServiceImpl{
		cfg:        cfg,
		logger:     logger,
		db:         db,
		serverRepo: serverRepo,
		rmsClient:  rmsClient,
	}
}

// Start запускает сервис в фоновом режиме.
func (s *crmidWorkerServiceImpl) Start(ctx context.Context) {
	s.logger.Info("Запуск воркера для обогащения CRMid", zap.Duration("interval", s.cfg.CRMidWorkerInterval))
	ticker := time.NewTicker(s.cfg.CRMidWorkerInterval)
	defer ticker.Stop()

	// Первый запуск сразу, не дожидаясь тикера
	s.runCycle(ctx)

	for {
		select {
		case <-ticker.C:
			s.runCycle(ctx)
		case <-ctx.Done():
			s.logger.Info("Остановка воркера для обогащения CRMid...")
			return
		}
	}
}

// runCycle выполняет один цикл работы воркера.
func (s *crmidWorkerServiceImpl) runCycle(ctx context.Context) {
	s.logger.Info("Начало нового цикла обогащения CRMid...")

	servers, err := s.serverRepo.FindWithEmptyCRMid(ctx, s.cfg.CRMidWorkerBatchSize)
	if err != nil {
		s.logger.Error("Не удалось получить список серверов для обогащения", zap.Error(err))
		return
	}

	if len(servers) == 0 {
		s.logger.Info("Не найдено серверов, подлежащих проверке. Цикл завершен.")
		return
	}

	s.logger.Info("Найдено серверов для обработки", zap.Int("count", len(servers)))

	for _, server := range servers {
		select {
		case <-ctx.Done():
			s.logger.Info("Выход из приложения, прерывание воркера опроса RMS")
			return
		default:
			s.processServer(ctx, server)
			time.Sleep(2 * time.Second) // Уменьшим задержку, т.к. фильтрация стала умнее
		}
	}
	s.logger.Info("Цикл обогащения CRMid завершен.")
}

// processServer обрабатывает один сервер.
func (s *crmidWorkerServiceImpl) processServer(ctx context.Context, server models.Server) {
	log := s.logger.With(zap.String("server_uuid", utils.SafeStringDereference(server.ServiceDeskUUID)), zap.String("server_ip", utils.SafeStringDereference(server.IP)))

	// Обработка серверов без IP
	if server.IP == nil || *server.IP == "" {
		log.Warn("У сервера отсутствует IP-адрес. Установка статуса 'to_delete'.")
		updateData := map[string]interface{}{"status": "to_delete"}
		if _, err := s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updateData); err != nil {
			log.Error("Не удалось обновить статус сервера на 'to_delete'", zap.Error(err))
		}
		return
	}

	var url string
	parts := strings.SplitN(*server.IP, ":", 2)
	host := parts[0]

	if len(parts) == 2 && parts[1] == "443" {
		url = "https://" + host
	} else {
		url = "http://" + *server.IP
	}

	crmid, err := s.rmsClient.GetCRMid(ctx, url, s.cfg.RMSLogin, s.cfg.RMSPassword1, s.cfg.RMSPassword2)
	if err != nil {
		// Умная обработка ошибок
		s.handleProcessingError(ctx, server, url, err, log)
		return
	}

	log.Info("CRMid успешно получен", zap.String("crmid", crmid), zap.String("url", url))
	updateData := map[string]interface{}{
		"crm_id":             crmid,
		"status":             "active", // Если он был inactive, возвращаем в работу
		"crmid_last_attempt": nil,      // Сбрасываем таймер неудачных попыток
	}
	_, err = s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updateData)
	if err != nil {
		log.Error("Не удалось обновить CRMid в базе данных", zap.Error(err))
	} else {
		log.Info("CRMid успешно сохранен в базе данных")
	}
}

// handleProcessingError анализирует ошибку и принимает решение о дальнейших действиях.
func (s *crmidWorkerServiceImpl) handleProcessingError(ctx context.Context, server models.Server, url string, err error, log *zap.Logger) {
	log.Warn("Не удалось получить CRMid для сервера", zap.String("url", url), zap.Error(err))

	// Проверяем, является ли ошибка сетевой (DNS lookup, connection refused и т.д.)
	var dnsError *net.DNSError
	isNetworkError := strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "i/o timeout") || errors.As(err, &dnsError)

	updateData := make(map[string]interface{})
	now := time.Now()
	updateData["crmid_last_attempt"] = &now

	if isNetworkError {
		log.Info("Обнаружена сетевая ошибка. Сервер будет помечен как 'inactive' на 30 дней.")
		updateData["status"] = "inactive"
	}

	// Обновляем сервер в БД
	if _, updateErr := s.serverRepo.Update(ctx, nil, *server.ServiceDeskUUID, updateData); updateErr != nil {
		log.Error("Не удалось обновить статус/время последней попытки для сервера", zap.Error(updateErr))
	}

	// Создаем/обновляем задачу для администратора в любом случае
	s.createOrUpdateTask(ctx, server, err, log)
}

// createOrUpdateTask создает или обновляет задачу для администратора.
func (s *crmidWorkerServiceImpl) createOrUpdateTask(ctx context.Context, server models.Server, processErr error, log *zap.Logger) {
	taskType := "crmid_enrichment_failed"
	entityUUID := utils.SafeStringDereference(server.ServiceDeskUUID)

	detailsMap := map[string]string{
		"serverIP":      utils.SafeStringDereference(server.IP),
		"serverUUID":    entityUUID,
		"lastError":     processErr.Error(),
		"lastAttemptAt": time.Now().Format(time.RFC3339),
	}
	detailsJSON, _ := json.Marshal(detailsMap)

	var existingTask models.ReconciliationTask
	err := s.db.WithContext(ctx).
		Where("entity_uuid = ? AND task_type = ?", entityUUID, taskType).
		First(&existingTask).Error

	if err == gorm.ErrRecordNotFound {
		task := models.ReconciliationTask{
			TaskType:   taskType,
			EntityType: "Server",
			EntityUUID: entityUUID,
			Details:    datatypes.JSON(detailsJSON),
			Status:     "new",
			Comment:    fmt.Sprintf("Первая неудачная попытка получения CRMid. Ошибка: %v", processErr),
		}
		if createErr := s.db.WithContext(ctx).Create(&task).Error; createErr != nil {
			log.Error("Не удалось создать задачу", zap.Error(createErr))
		} else {
			log.Info("Создана новая задача на ручное обогащение CRMid")
		}
		return
	} else if err != nil {
		log.Error("Ошибка при поиске существующей задачи", zap.Error(err))
		return
	}

	commentUpdate := fmt.Sprintf("\n[%s] Повторная неудачная попытка. Ошибка: %v", time.Now().Format(time.RFC3339), processErr)
	updateResult := s.db.WithContext(ctx).Model(&existingTask).Updates(map[string]interface{}{
		"details": datatypes.JSON(detailsJSON),
		"status":  "new", // Если задача была решена, но проблема вернулась - переоткрываем.
		"comment": gorm.Expr("comment || ?", commentUpdate),
	})

	if updateResult.Error != nil {
		log.Error("Не удалось обновить существующую задачу", zap.Error(updateResult.Error))
	} else {
		log.Info("Обновлена существующая задача на ручное обогащение CRMid")
	}
}
