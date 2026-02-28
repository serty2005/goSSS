package services

import (
	"context"
	"errors"
	"etalon-server/internal/contextkeys"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/models"
	domainrepos "etalon-server/internal/domain/repositories"
	"etalon-server/internal/domain/server"
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
	ErrRateLimitExceeded   = errors.New("слишком много запросов на опрос сервера")
	ErrCloudPollingSkipped = errors.New("для cloud-адресов опрос не поддерживается")
)

const (
	rateLimitCount     = 3
	rateLimitWindow    = 2 * time.Minute
	licenseWaitTimeout = 90 * time.Second
	licensePollStep    = 3 * time.Second
)

type InstallLicenseResult struct {
	ServerID      string    `json:"server_id"`
	Status        string    `json:"status"`
	ServerName    string    `json:"server_name"`
	ServerVersion string    `json:"server_version"`
	ServerEdition string    `json:"server_edition"`
	CRMid         string    `json:"crm_id"`
	LastPolledAt  time.Time `json:"last_polled_at"`
}

type ServerActionsService interface {
	PollSingleServer(ctx context.Context, serverID string) error
	InstallLicense(ctx context.Context, serverID, login, password, fallbackPassword, uniqueID string) (*InstallLicenseResult, error)
	AddAdditionalOwner(ctx context.Context, serverID, companyID string) error
	RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error
}

type serverActionsServiceImpl struct {
	logger        logger.LoggerInterface
	bus           eventbus.EventBus
	serverRepo    server.Repository
	companyRepo   company.Repository
	ownerHistory  domainrepos.OwnerHistoryRepo
	iikoClient    iiko.IikoClient
	rateLimiter   *sync.Mutex
	requestStamps map[string][]time.Time
}

func NewServerActionsService(cfg *config.Config, logger logger.LoggerInterface, bus eventbus.EventBus, serverRepo server.Repository, companyRepo company.Repository, ownerHistory domainrepos.OwnerHistoryRepo, iikoClient iiko.IikoClient) ServerActionsService {
	_ = cfg
	return &serverActionsServiceImpl{
		logger:        logger,
		bus:           bus,
		serverRepo:    serverRepo,
		companyRepo:   companyRepo,
		ownerHistory:  ownerHistory,
		iikoClient:    iikoClient,
		rateLimiter:   &sync.Mutex{},
		requestStamps: make(map[string][]time.Time),
	}
}

func (s *serverActionsServiceImpl) PollSingleServer(ctx context.Context, serverID string) error {
	if !s.checkRateLimit(serverID) {
		return ErrRateLimitExceeded
	}

	serverModel, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера из БД: %w", err)
	}
	if serverModel == nil {
		return gorm.ErrRecordNotFound
	}
	if isCloudPollingAddress(serverModel.IP) {
		return ErrCloudPollingSkipped
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

func isCloudPollingAddress(ip *string) bool {
	if ip == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(*ip))
	if value == "" {
		return false
	}
	return strings.Contains(value, "iikoweb") || strings.Contains(value, "syrve.app")
}

// InstallLicense выполняет установку лицензии на iiko-сервер.
func (s *serverActionsServiceImpl) InstallLicense(ctx context.Context, serverID, login, password, fallbackPassword, uniqueID string) (*InstallLicenseResult, error) {
	serverModel, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return nil, fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if serverModel == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if serverModel.IP == nil || *serverModel.IP == "" {
		return nil, fmt.Errorf("у сервера отсутствует IP-адрес для подключения")
	}
	if strings.TrimSpace(login) == "" {
		return nil, fmt.Errorf("логин обязателен")
	}
	if strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("пароль обязателен")
	}
	if strings.TrimSpace(uniqueID) == "" {
		return nil, fmt.Errorf("UID обязателен")
	}
	status := strings.ToLower(strings.TrimSpace(serverModel.Status))
	if status != "active" && status != "license" {
		return nil, fmt.Errorf("установка лицензии доступна только для серверов со статусом active или license")
	}

	serverURL := fmt.Sprintf("http://%s", *serverModel.IP)
	if strings.Contains(*serverModel.IP, ":443") {
		serverURL = fmt.Sprintf("https://%s", strings.Split(*serverModel.IP, ":")[0])
	}

	log := s.logger.With("server_id", serverID, "server_url", serverURL, "uid", uniqueID)
	log.Info("Запуск установки лицензии")

	success, err := s.iikoClient.InstallLicense(ctx, serverURL, strings.TrimSpace(login), strings.TrimSpace(password), strings.TrimSpace(fallbackPassword), strings.TrimSpace(uniqueID))
	if err != nil {
		log.Error("Ошибка при установке лицензии", "error", err)
		return nil, fmt.Errorf("не удалось установить лицензию: %w", err)
	}
	if !success {
		log.Warn("Установка лицензии завершилась неуспешно (без ошибки)")
		return nil, fmt.Errorf("установка лицензии завершилась неуспешно")
	}

	result, err := s.waitForServerActivation(ctx, serverID, serverURL, strings.TrimSpace(login), strings.TrimSpace(password), strings.TrimSpace(fallbackPassword))
	if err != nil {
		return nil, err
	}

	s.writeInstallLicenseHistory(ctx, serverID, stringPtrValue(serverModel.OwnerID), strings.TrimSpace(uniqueID), result)
	log.Info("Установка лицензии успешно завершена")
	return result, nil
}

func (s *serverActionsServiceImpl) waitForServerActivation(ctx context.Context, serverID, serverURL, login, password, fallbackPassword string) (*InstallLicenseResult, error) {
	deadline := time.Now().Add(licenseWaitTimeout)
	for {
		info, err := s.iikoClient.GetServerMonitoringInfo(ctx, serverURL)
		if err == nil && info != nil {
			crmID := ""
			if value, crmErr := s.iikoClient.GetCRMid(ctx, serverURL, login, password, fallbackPassword); crmErr == nil {
				crmID = strings.TrimSpace(value)
			}
			status := mapServerStateToStatus(info.ServerState)
			now := time.Now()
			updates := map[string]interface{}{
				"server_name":     strings.TrimSpace(info.ServerName),
				"server_edition":  strings.TrimSpace(info.Edition),
				"server_version":  strings.TrimSpace(info.Version),
				"status":          status,
				"last_polled_at":  now,
				"last_updated_by": "license_install",
			}
			if crmID != "" {
				updates["crm_id"] = crmID
			}
			if _, updateErr := s.serverRepo.Update(ctx, nil, serverID, updates); updateErr != nil {
				return nil, fmt.Errorf("не удалось обновить сервер после установки лицензии: %w", updateErr)
			}
			if status == "active" {
				return &InstallLicenseResult{
					ServerID:      serverID,
					Status:        status,
					ServerName:    strings.TrimSpace(info.ServerName),
					ServerVersion: strings.TrimSpace(info.Version),
					ServerEdition: strings.TrimSpace(info.Edition),
					CRMid:         crmID,
					LastPolledAt:  now,
				}, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("сервер не подтвердил успешный запуск после установки лицензии за %s", licenseWaitTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(licensePollStep):
		}
	}
}

func (s *serverActionsServiceImpl) writeInstallLicenseHistory(ctx context.Context, serverID, ownerID, uniqueID string, result *InstallLicenseResult) {
	if s.ownerHistory == nil {
		return
	}
	comment := fmt.Sprintf(
		"Установлена лицензия сервера (UID: %s, статус: %s, версия: %s, редакция: %s, CRM ID: %s)",
		uniqueID,
		defaultString(result.Status, "-"),
		defaultString(result.ServerVersion, "-"),
		defaultString(result.ServerEdition, "-"),
		defaultString(result.CRMid, "-"),
	)
	event := &models.OwnerChangeHistory{
		EntityType:      "Server",
		EntityID:        serverID,
		ToOwnerID:       strings.TrimSpace(ownerID),
		ChangeSource:    models.OwnerChangeSourceManualUpdate,
		ChangedByUserID: stringPtrOrNil(contextUserID(ctx)),
		Comment:         &comment,
	}
	if err := s.ownerHistory.Create(ctx, event); err != nil {
		s.logger.Warn("Не удалось записать событие установки лицензии в историю сервера", "server_id", serverID, "error", err)
	}
}

func mapServerStateToStatus(state string) string {
	switch strings.TrimSpace(state) {
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

func contextUserID(ctx context.Context) string {
	value := ctx.Value(contextkeys.UserIDContextKey)
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func stringPtrOrNil(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func (s *serverActionsServiceImpl) AddAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	serverModel, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if serverModel == nil {
		return gorm.ErrRecordNotFound
	}

	companyEntity, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return fmt.Errorf("ошибка получения компании: %w", err)
	}
	if companyEntity == nil {
		return fmt.Errorf("компания с ID %s не найдена: %w", companyID, gorm.ErrRecordNotFound)
	}

	if err := s.serverRepo.AddAdditionalOwner(ctx, serverID, companyID); err != nil {
		s.logger.Error("Не удалось добавить дополнительного владельца", "error", err)
		return fmt.Errorf("ошибка добавления связи в БД: %w", err)
	}

	s.logger.Info("Дополнительный владелец успешно добавлен к серверу", "server_id", serverID, "company_id", companyID)
	return nil
}

func (s *serverActionsServiceImpl) RemoveAdditionalOwner(ctx context.Context, serverID, companyID string) error {
	serverModel, err := s.serverRepo.GetByID(ctx, serverID)
	if err != nil {
		return fmt.Errorf("ошибка получения сервера: %w", err)
	}
	if serverModel == nil {
		return gorm.ErrRecordNotFound
	}

	companyEntity, err := s.companyRepo.GetByID(ctx, companyID)
	if err != nil {
		return fmt.Errorf("ошибка получения компании: %w", err)
	}
	if companyEntity == nil {
		return fmt.Errorf("компания с ID %s не найдена: %w", companyID, gorm.ErrRecordNotFound)
	}

	if err := s.serverRepo.RemoveAdditionalOwner(ctx, serverID, companyID); err != nil {
		s.logger.Error("Не удалось удалить дополнительного владельца", "error", err)
		return fmt.Errorf("ошибка удаления связи из БД: %w", err)
	}

	s.logger.Info("Дополнительный владелец успешно удален с сервера", "server_id", serverID, "company_id", companyID)
	return nil
}
