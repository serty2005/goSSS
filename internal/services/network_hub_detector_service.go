package services

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/domain/server"
	domainServices "etalon-server/internal/domain/services"
	"etalon-server/internal/domain/workstation"
	"etalon-server/internal/infra/logger"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

// networkHubCacheEntry представляет запись в кэше результатов.
type networkHubCacheEntry struct {
	isHub    bool
	cachedAt time.Time
}

// NetworkHubDetectorService реализует логику определения network-hub компаний.
type NetworkHubDetectorService struct {
	logger      logger.LoggerInterface
	db          *gorm.DB
	companyRepo company.Repository

	// Кэш результатов
	cache    map[string]networkHubCacheEntry
	cacheMu  sync.RWMutex
	cacheTTL time.Duration
}

// NewNetworkHubDetectorService создаёт новый экземпляр детектора.
func NewNetworkHubDetectorService(
	logger logger.LoggerInterface,
	db *gorm.DB,
	companyRepo company.Repository,
) *NetworkHubDetectorService {
	return &NetworkHubDetectorService{
		logger:      logger,
		db:          db,
		companyRepo: companyRepo,
		cache:       make(map[string]networkHubCacheEntry),
		cacheTTL:    5 * time.Minute,
	}
}

// IsNetworkHub проверяет, является ли компания network-hub.
func (s *NetworkHubDetectorService) IsNetworkHub(ctx context.Context, companyID string) (bool, error) {
	companyID = strings.TrimSpace(companyID)
	if companyID == "" {
		return false, nil
	}

	// Проверка кэша
	s.cacheMu.RLock()
	if cached, ok := s.cache[companyID]; ok && time.Since(cached.cachedAt) < s.cacheTTL {
		s.cacheMu.RUnlock()
		return cached.isHub, nil
	}
	s.cacheMu.RUnlock()

	// Проверка признаков
	isHub, err := s.checkHubIndicators(ctx, companyID)
	if err != nil {
		return false, err
	}

	// Сохранение в кэш
	s.cacheMu.Lock()
	s.cache[companyID] = networkHubCacheEntry{
		isHub:    isHub,
		cachedAt: time.Now(),
	}
	s.cacheMu.Unlock()

	return isHub, nil
}

// IsNetworkHubServer проверяет, является ли сервер network-hub.
func (s *NetworkHubDetectorService) IsNetworkHubServer(ctx context.Context, srv *server.Server) (bool, error) {
	if srv == nil || srv.OwnerID == nil || strings.TrimSpace(*srv.OwnerID) == "" {
		return false, nil
	}

	// Сначала проверяем owner_mode компании
	owner, err := s.companyRepo.GetByID(ctx, strings.TrimSpace(*srv.OwnerID))
	if err != nil {
		return false, fmt.Errorf("ошибка получения владельца сервера: %w", err)
	}

	// Ручной режим имеет приоритет
	if strings.TrimSpace(owner.OwnerMode) == models.CompanyOwnerModeNetworkHub {
		return true, nil
	}

	// Автоматическое определение по признакам
	return s.IsNetworkHub(ctx, *srv.OwnerID)
}

// checkHubIndicators проверяет признаки network-hub компании.
func (s *NetworkHubDetectorService) checkHubIndicators(ctx context.Context, companyID string) (bool, error) {
	// Признак 1: У компании есть дочерние компании
	var childCount int64
	if err := s.db.WithContext(ctx).
		Model(&company.Company{}).
		Where("parent_id = ?", companyID).
		Count(&childCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта дочерних компаний: %w", err)
	}
	if childCount == 0 {
		s.logger.Debug("NetworkHubDetector: у компании нет дочерних компаний — не network_hub",
			"company_id", companyID,
		)
		return false, nil
	}

	// Признак 2: У дочерних компаний нет серверов
	var childServerCount int64
	if err := s.db.WithContext(ctx).
		Table("servers").
		Joins("JOIN companies ON companies.id = servers.owner_id").
		Where("companies.parent_id = ?", companyID).
		Count(&childServerCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта серверов дочерних компаний: %w", err)
	}
	if childServerCount > 0 {
		s.logger.Debug("NetworkHubDetector: дочерние компании имеют серверы — не network_hub",
			"company_id", companyID,
			"child_server_count", childServerCount,
		)
		return false, nil
	}

	// Признак 3: У компании есть хотя бы один сервер
	var srvCount int64
	if err := s.db.WithContext(ctx).
		Model(&server.Server{}).
		Where("owner_id = ?", companyID).
		Count(&srvCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта серверов: %w", err)
	}
	if srvCount == 0 {
		s.logger.Debug("NetworkHubDetector: у компании нет серверов — не network_hub",
			"company_id", companyID,
		)
		return false, nil
	}

	// Признак 4: У дочерних компаний есть оборудование (РС или ФР)
	var childWSCount int64
	if err := s.db.WithContext(ctx).
		Table("workstations").
		Joins("JOIN companies ON companies.id = workstations.owner_id").
		Where("companies.parent_id = ?", companyID).
		Count(&childWSCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта РС дочерних компаний: %w", err)
	}
	var childFRCount int64
	if err := s.db.WithContext(ctx).
		Table("fiscal_registers fr").
		Joins("JOIN companies ON companies.id = fr.owner_id").
		Where("companies.parent_id = ?", companyID).
		Count(&childFRCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта ФР дочерних компаний: %w", err)
	}
	if childWSCount == 0 && childFRCount == 0 {
		s.logger.Debug("NetworkHubDetector: у дочерних компаний нет оборудования — не network_hub",
			"company_id", companyID,
		)
		return false, nil
	}

	// Доп. метрика: РС родителя больше не является блокирующим признаком.
	var wsCount int64
	if err := s.db.WithContext(ctx).
		Model(&workstation.Workstation{}).
		Where("owner_id = ?", companyID).
		Count(&wsCount).Error; err != nil {
		return false, fmt.Errorf("ошибка подсчёта РС: %w", err)
	}
	s.logger.Debug("NetworkHubDetector: проверка завершена",
		"company_id", companyID,
		"is_hub", true,
		"child_count", childCount,
		"server_count", srvCount,
		"ws_count", wsCount,
		"child_server_count", childServerCount,
		"child_ws_count", childWSCount,
		"child_fr_count", childFRCount,
	)

	return true, nil
}

// ClearCache очищает кэш результатов.
func (s *NetworkHubDetectorService) ClearCache() {
	s.cacheMu.Lock()
	s.cache = make(map[string]networkHubCacheEntry)
	s.cacheMu.Unlock()
}

// Убеждаемся, что NetworkHubDetectorService реализует интерфейс NetworkHubDetector
var _ domainServices.NetworkHubDetector = (*NetworkHubDetectorService)(nil)
