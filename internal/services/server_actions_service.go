package services

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/repositories"
	"etalon-server/pkg/eventbus"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// ServerActionsService определяет интерфейс для ручных действий над серверами.
type ServerActionsService interface {
	PollSingleServer(ctx context.Context, serverUUID string) error
	InstallLicense(ctx context.Context, serverUUID, uniqueID string) error
}

type serverActionsServiceImpl struct {
	logger        *zap.Logger
	bus           eventbus.EventBus
	serverRepo    repositories.ServerRepo
	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

// NewServerActionsService создает новый экземпляр сервиса.
func NewServerActionsService(logger *zap.Logger, bus eventbus.EventBus, serverRepo repositories.ServerRepo) ServerActionsService {
	return &serverActionsServiceImpl{
		logger:        logger,
		bus:           bus,
		serverRepo:    serverRepo,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

// PollSingleServer запускает асинхронную задачу опроса через событие, с проверкой rate limit.
func (s *serverActionsServiceImpl) PollSingleServer(ctx context.Context, serverUUID string) error {
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

	s.logger.Info("Получен ручной запрос на опрос сервера. Публикация события...", zap.String("uuid", serverUUID))

	s.bus.Publish(eventbus.Event{
		Type: events.ServerPollingRequested,
		Payload: events.ServerPollingRequestedPayload{
			ServerUUID: serverUUID,
		},
	})

	return nil
}

// checkRateLimit проверяет, можно ли выполнить запрос для данного serverUUID.
func (s *serverActionsServiceImpl) checkRateLimit(serverUUID string) bool {
	s.rateLimiter.Lock()
	defer s.rateLimiter.Unlock()
	now := time.Now()
	limitWindowStart := now.Add(-rateLimitWindow)
	stamps := s.requestStamps[serverUUID]
	recentStamps := make([]time.Time, 0, len(stamps))
	for _, stamp := range stamps {
		if stamp.After(limitWindowStart) {
			recentStamps = append(recentStamps, stamp)
		}
	}
	if len(recentStamps) >= rateLimitCount {
		s.logger.Warn("Превышен лимит запросов на опрос для сервера", zap.String("uuid", serverUUID))
		s.requestStamps[serverUUID] = recentStamps
		return false
	}
	recentStamps = append(recentStamps, now)
	s.requestStamps[serverUUID] = recentStamps
	return true
}

// InstallLicense - метод-заглушка для ручного запуска установки лицензии.
func (s *serverActionsServiceImpl) InstallLicense(ctx context.Context, serverUUID, uniqueID string) error {
	server, err := s.serverRepo.GetByUUID(ctx, serverUUID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}
	s.logger.Info("ЗАГЛУШКА: Запущена установка лицензии",
		zap.String("server_uuid", serverUUID),
		zap.String("unique_id", uniqueID),
	)
	// В будущем здесь тоже может быть публикация события, например, `license.installation.requested`
	return nil
}