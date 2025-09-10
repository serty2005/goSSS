// internal/services/server_actions_service.go
package services

import (
	"context"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/logger"
	"etalon-server/internal/repositories"
	"etalon-server/pkg/eventbus"
	"fmt"
	"sync"
	"time"

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

// ServerActionsService определяет интерфейс для ручных действий над серверами.
type ServerActionsService interface {
	PollSingleServer(ctx context.Context, serverID string) error
	InstallLicense(ctx context.Context, serverID, uniqueID string) error
	AddAdditionalOwner(ctx context.Context, serverID, companyID string) error
	RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error
}

type serverActionsServiceImpl struct {
	logger        logger.LoggerInterface
	bus           eventbus.EventBus
	serverRepo    repositories.ServerRepo
	companyRepo   repositories.CompanyRepo
	db            *gorm.DB
	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

// NewServerActionsService создает новый экземпляр сервиса.
func NewServerActionsService(logger logger.LoggerInterface, bus eventbus.EventBus, serverRepo repositories.ServerRepo, companyRepo repositories.CompanyRepo, db *gorm.DB) ServerActionsService {
	return &serverActionsServiceImpl{
		logger:        logger,
		bus:           bus,
		serverRepo:    serverRepo,
		companyRepo:   companyRepo,
		db:            db,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

// PollSingleServer запускает асинхронную задачу опроса через событие, с проверкой rate limit.
// Принимает внутренний ID сервера.
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
			ServerUUID: serverID, // Поле в событии называется UUID, но мы передаем внутренний ID
		},
	})

	return nil
}

// checkRateLimit проверяет, можно ли выполнить запрос для данного serverID.
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

// InstallLicense - метод-заглушка для ручного запуска установки лицензии.
// Принимает внутренний ID сервера.
func (s *serverActionsServiceImpl) InstallLicense(ctx context.Context, serverID, uniqueID string) error {
	server, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if server == nil {
		return gorm.ErrRecordNotFound
	}
	s.logger.Info("ЗАГЛУШКА: Запущена установка лицензии",
		"server_id", serverID,
		"unique_id", uniqueID,
	)
	return nil
}

// AddAdditionalOwner добавляет компанию в список дополнительных владельцев сервера.
// Принимает внутренние ID сервера и компании.
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

// RemoveAdditionalOwner удаляет компанию из списка дополнительных владельцев сервера.
// Принимает внутренние ID сервера и компании.
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
