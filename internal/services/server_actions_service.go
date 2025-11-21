// Файл: internal/services/server_actions_service.go
package services

import (
	"context"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/repositories"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/iiko"
	"etalon-server/internal/infra/logger"
	"etalon-server/pkg/eventbus"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

var (
	ErrRateLimitExceeded = errors.New("слишком много запросов на опрос сервера")
)

const (
	rateLimitCount  = 3
	rateLimitWindow = 2 * time.Minute
)

type ServerActionsService interface {
	PollSingleServer(ctx context.Context, serverID string) error
	InstallLicense(ctx context.Context, serverID, uniqueID string) error
	AddAdditionalOwner(ctx context.Context, serverID, companyID string) error
	RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error
}

type serverActionsServiceImpl struct {
	cfg           *config.Config
	logger        logger.LoggerInterface
	bus           eventbus.EventBus
	serverRepo    repositories.ServerRepo
	companyRepo   company.Repository
	db            *gorm.DB
	iikoClient    iiko.IikoClient
	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

func NewServerActionsService(cfg *config.Config, logger logger.LoggerInterface, bus eventbus.EventBus, serverRepo repositories.ServerRepo, companyRepo company.Repository, db *gorm.DB, iikoClient iiko.IikoClient) ServerActionsService {
	return &serverActionsServiceImpl{
		cfg:           cfg,
		logger:        logger,
		bus:           bus,
		serverRepo:    serverRepo,
		companyRepo:   companyRepo,
		db:            db,
		iikoClient:    iikoClient,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

func (s *serverActionsServiceImpl) PollSingleServer(ctx context.Context, serverID string) error {
	if !s.checkRateLimit(serverID) {
		return ErrRateLimitExceeded
	}

	server, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера из БД: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	s.logger.Info("Получен ручной запрос на опрос сервера. Публикация события...", "serverID", serverID)

	s.bus.Publish(eventbus.Event{
		Type: events.ServerPollingRequested,
		Payload: events.ServerPollingRequestedPayload{
			ServerUUID: serverID,
		},
	})

	return nil
}

func (s *serverActionsServiceImpl) checkRateLimit(serverID string) bool {
	s.rateLimiter.Lock()
	defer s.rateLimiter.Unlock()
	now := time.Now()
	limitWindowStart := now.Add(-rateLimitWindow)
	stamps := s.requestStamps[serverID]
	recentStamps := make([]time.Time, 0, len(stamps))
	for _, stamp := range stamps {
		if stamp.After(limitWindowStart) {
			recentStamps = append(recentStamps, stamp)
		}
	}
	if len(recentStamps) >= rateLimitCount {
		s.logger.Warn("Превышен лимит запросов на опрос для сервера", "serverID", serverID)
		s.requestStamps[serverID] = recentStamps
		return false
	}
	recentStamps = append(recentStamps, now)
	s.requestStamps[serverID] = recentStamps
	return true
}

// InstallLicense выполняет установку лицензии на iiko-сервер.
func (s *serverActionsServiceImpl) InstallLicense(ctx context.Context, serverID, uniqueID string) error {
	server, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}
	if server.IP == nil || *server.IP == "" {
		return fmt.Errorf("у сервера отсутствует IP-адрес для подключения")
	}

	serverURL := fmt.Sprintf("http://%s", *server.IP)
	if strings.Contains(*server.IP, ":443") {
		serverURL = fmt.Sprintf("https://%s", strings.Split(*server.IP, ":")[0])
	}

	log := s.logger.With("server_id", serverID, "server_url", serverURL, "uid", uniqueID)
	log.Info("Запуск установки лицензии")

	success, err := s.iikoClient.InstallLicense(ctx, serverURL, s.cfg.RMSLogin, s.cfg.RMSPassword1, s.cfg.RMSPassword2, uniqueID)
	if err != nil {
		log.Error("Ошибка при установке лицензии", "error", err)
		return fmt.Errorf("не удалось установить лицензию: %w", err)
	}
	if !success {
		log.Warn("Установка лицензии завершилась неуспешно (без ошибки)")
		return fmt.Errorf("установка лицензии завершилась неуспешно")
	}

	log.Info("Установка лицензии успешно завершена")
	return nil
}

func (s *serverActionsServiceImpl) AddAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	server, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	company, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return fmt.Errorf("ошибка получения компании: %w", err)
	}
	if company == nil {
		return fmt.Errorf("компания с ID %s не найдена: %w", companyID, gorm.ErrRecordNotFound)
	}

	err = s.db.Model(server).Association("AdditionalOwners").Append(company)
	if err != nil {
		s.logger.Error("Не удалось добавить дополнительного владельца", "error", err)
		return fmt.Errorf("ошибка добавления связи в БД: %w", err)
	}

	s.logger.Info("Дополнительный владелец успешно добавлен к серверу", "server_id", serverID, "company_id", companyID)
	return nil
}

func (s *serverActionsServiceImpl) RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	server, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}

	company, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return fmt.Errorf("ошибка получения компании: %w", err)
	}
	if company == nil {
		return fmt.Errorf("компания с ID %s не найдена: %w", companyID, gorm.ErrRecordNotFound)
	}

	err = s.db.Model(server).Association("AdditionalOwners").Delete(company)
	if err != nil {
		s.logger.Error("Не удалось удалить дополнительного владельца", "error", err)
		return fmt.Errorf("ошибка удаления связи из БД: %w", err)
	}

	s.logger.Info("Дополнительный владелец успешно удален с сервера", "server_id", serverID, "company_id", companyID)
	return nil
}
