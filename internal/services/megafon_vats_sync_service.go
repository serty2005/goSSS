package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	megafonvats "etalon-server/internal/infra/plugins/megafonvats"
	"strings"
	"time"
)

type megafonVATSDirectoryClient interface {
	IsConfigured() bool
	ListUsers(ctx context.Context, withStatus bool) ([]megafonvats.User, error)
	GetUser(ctx context.Context, login string, withStatus bool) (*megafonvats.User, error)
}

type MegafonVATSSyncService interface {
	IsEnabled() bool
	Start(ctx context.Context)
	RefreshEmployees(ctx context.Context) (int, error)
	ListCachedEmployees(ctx context.Context) ([]telephony.ProviderEmployee, error)
	SearchEmployeesByName(ctx context.Context, firstName, lastName, fullName string) ([]telephony.ProviderEmployee, error)
	GetEmployee(ctx context.Context, login string) (*telephony.ProviderEmployee, error)
}

type megafonVATSSyncService struct {
	cfg      *config.Config
	log      logger.LoggerInterface
	client   megafonVATSDirectoryClient
	repo     telephony.Repository
	userRepo user.Repository
}

func NewMegafonVATSSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client megafonVATSDirectoryClient,
	repo telephony.Repository,
	userRepo user.Repository,
) MegafonVATSSyncService {
	return &megafonVATSSyncService{
		cfg:      cfg,
		log:      log,
		client:   client,
		repo:     repo,
		userRepo: userRepo,
	}
}

func (s *megafonVATSSyncService) IsEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.EnableMegafonVATS && s.client != nil && s.client.IsConfigured() && s.repo != nil
}

func (s *megafonVATSSyncService) Start(ctx context.Context) {
	if !s.IsEnabled() {
		if s != nil && s.log != nil {
			s.log.Warn(
				"Мегафон ВАТС: синхронизация сотрудников отключена",
				"feature_enabled", s.cfg != nil && s.cfg.EnableMegafonVATS,
				"client_configured", s.client != nil && s.client.IsConfigured(),
				"repo_ready", s.repo != nil,
			)
		}
		return
	}

	s.refreshEmployeesSafe(ctx)

	interval := s.cfg.MegafonVATSSyncInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshEmployeesSafe(ctx)
		}
	}
}

func (s *megafonVATSSyncService) RefreshEmployees(ctx context.Context) (int, error) {
	if !s.IsEnabled() {
		return 0, nil
	}

	users, err := s.client.ListUsers(ctx, true)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	items := make([]telephony.ProviderEmployee, 0, len(users))
	for i := range users {
		rawJSON, marshalErr := json.Marshal(users[i])
		if marshalErr != nil {
			return 0, marshalErr
		}
		items = append(items, telephony.ProviderEmployee{
			Provider:      telephony.ProviderMegafonVATS,
			EmployeeLogin: strings.TrimSpace(users[i].Login),
			EmployeeName:  strings.TrimSpace(users[i].Name),
			Ext:           normalizeMegafonOptionalString(&users[i].Ext),
			Telnum:        normalizeMegafonOptionalString(&users[i].Telnum),
			Status:        normalizeMegafonOptionalString(&users[i].Status),
			RawJSON:       string(rawJSON),
			LastSeenAt:    now,
		})
	}

	if err = s.repo.ReplaceProviderEmployees(ctx, telephony.ProviderMegafonVATS, items); err != nil {
		return 0, err
	}
	if err = s.refreshMegafonIntegrations(ctx, items); err != nil {
		return 0, err
	}

	return len(items), nil
}

func (s *megafonVATSSyncService) ListCachedEmployees(ctx context.Context) ([]telephony.ProviderEmployee, error) {
	if s == nil || s.repo == nil {
		return []telephony.ProviderEmployee{}, nil
	}
	return s.repo.ListProviderEmployees(ctx, telephony.ProviderMegafonVATS)
}

func (s *megafonVATSSyncService) SearchEmployeesByName(ctx context.Context, firstName, lastName, fullName string) ([]telephony.ProviderEmployee, error) {
	items, err := s.ListCachedEmployees(ctx)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return []telephony.ProviderEmployee{}, nil
	}

	userFirst := normalizeMegafonPersonToken(firstName)
	userLast := normalizeMegafonPersonToken(lastName)
	userFull := normalizeMegafonPersonToken(fullName)

	result := make([]telephony.ProviderEmployee, 0, 1)
	for i := range items {
		employeeName := normalizeMegafonPersonToken(items[i].EmployeeName)
		if userFirst != "" && userLast != "" && strings.Contains(employeeName, userFirst) && strings.Contains(employeeName, userLast) {
			result = append(result, items[i])
			continue
		}
		if userFull != "" && employeeName == userFull {
			result = append(result, items[i])
		}
	}
	return result, nil
}

func (s *megafonVATSSyncService) GetEmployee(ctx context.Context, login string) (*telephony.ProviderEmployee, error) {
	if s == nil || s.repo == nil {
		return nil, nil
	}
	return s.repo.GetProviderEmployee(ctx, telephony.ProviderMegafonVATS, login)
}

func (s *megafonVATSSyncService) refreshEmployeesSafe(ctx context.Context) {
	count, err := s.RefreshEmployees(ctx)
	if err != nil {
		s.log.Error("Мегафон ВАТС: не удалось обновить сотрудников", "error", err)
		return
	}
	s.log.Info("Мегафон ВАТС: обновлен справочник сотрудников", "count", count)
}

func (s *megafonVATSSyncService) refreshMegafonIntegrations(ctx context.Context, items []telephony.ProviderEmployee) error {
	if s.userRepo == nil {
		return nil
	}

	employeesByLogin := make(map[string]telephony.ProviderEmployee, len(items))
	for i := range items {
		login := strings.TrimSpace(items[i].EmployeeLogin)
		if login == "" {
			continue
		}
		employeesByLogin[login] = items[i]
	}

	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return err
	}
	for i := range users {
		if !s.refreshMegafonUserIntegrations(&users[i], employeesByLogin) {
			continue
		}
		if err = s.userRepo.ReplaceIntegrations(ctx, users[i].ID, users[i].Integrations); err != nil {
			return err
		}
		if err = s.userRepo.Update(ctx, &users[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *megafonVATSSyncService) refreshMegafonUserIntegrations(u *user.User, employeesByLogin map[string]telephony.ProviderEmployee) bool {
	if u == nil {
		return false
	}

	changed := false
	for i := range u.Integrations {
		integration := &u.Integrations[i]
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
			continue
		}

		if !integration.IsEnabled {
			if integration.IsVerified || integration.IsLocked || integration.VerifiedName != "" {
				integration.IsVerified = false
				integration.IsLocked = false
				integration.VerifiedName = ""
				changed = true
			}
			continue
		}

		employee, exists := employeesByLogin[strings.TrimSpace(integration.ExternalID)]
		if !exists {
			if integration.IsVerified || integration.IsLocked || integration.VerifiedName != "" {
				integration.IsVerified = false
				integration.IsLocked = false
				integration.VerifiedName = ""
				changed = true
			}
			continue
		}

		if !integration.IsVerified || !integration.IsLocked || integration.VerifiedName != employee.EmployeeName {
			integration.IsVerified = true
			integration.IsLocked = true
			integration.VerifiedName = employee.EmployeeName
			changed = true
		}
	}

	if changed {
		u.ExternalType, u.ExternalID = pickPrimaryEnabledIntegration(u.Integrations)
	}
	return changed
}

func normalizeMegafonPersonToken(value string) string {
	token := strings.ToLower(strings.TrimSpace(value))
	token = strings.ReplaceAll(token, "ё", "е")
	token = strings.Join(strings.Fields(token), " ")
	return token
}

func pickPrimaryEnabledIntegration(items []user.Integration) (*string, *string) {
	for i := range items {
		if !items[i].IsEnabled {
			continue
		}
		integrationType := strings.TrimSpace(strings.ToLower(items[i].IntegrationType))
		externalID := strings.TrimSpace(items[i].ExternalID)
		if integrationType == "" || externalID == "" {
			continue
		}
		return &integrationType, &externalID
	}
	return nil, nil
}

func normalizeMegafonOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
