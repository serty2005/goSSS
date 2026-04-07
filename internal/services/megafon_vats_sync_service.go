package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	megafonvats "etalon-server/internal/infra/plugins/megafonvats"
	"strconv"
	"strings"
	"time"

	"etalon-server/pkg/eventbus"
	"github.com/google/uuid"
)

type megafonVATSClient interface {
	IsConfigured() bool
	ListUsers(ctx context.Context, withStatus bool) ([]megafonvats.User, error)
	GetUser(ctx context.Context, login string, withStatus bool) (*megafonvats.User, error)
	ListHistory(ctx context.Context, filter megafonvats.HistoryFilter) ([]megafonvats.HistoryRecord, error)
}

type MegafonVATSSyncService interface {
	IsEnabled() bool
	Start(ctx context.Context)
	RefreshEmployees(ctx context.Context) (int, error)
	SyncHistory(ctx context.Context) (int, error)
	SyncHistoryByFilter(ctx context.Context, filter MegafonVATSHistorySyncFilter) (int, error)
	ListCachedEmployees(ctx context.Context) ([]telephony.ProviderEmployee, error)
	SearchEmployeesByName(ctx context.Context, firstName, lastName, fullName string) ([]telephony.ProviderEmployee, error)
	GetEmployee(ctx context.Context, login string) (*telephony.ProviderEmployee, error)
}

type MegafonVATSHistorySyncFilter struct {
	StartedFrom   *time.Time
	StartedTo     *time.Time
	EmployeeLogin string
	ClientPhone   string
	Groups        []string
}

type megafonVATSSyncService struct {
	cfg              *config.Config
	log              logger.LoggerInterface
	client           megafonVATSClient
	repo             telephony.Repository
	ticketRepo       tickets.TicketRepository
	userRepo         user.Repository
	eventBus         eventbus.EventBus
	recordingService MegafonVATSRecordingService
}

func NewMegafonVATSSyncService(
	cfg *config.Config,
	log logger.LoggerInterface,
	client megafonVATSClient,
	repo telephony.Repository,
	ticketRepo tickets.TicketRepository,
	userRepo user.Repository,
	eventBus eventbus.EventBus,
	recordingService MegafonVATSRecordingService,
) MegafonVATSSyncService {
	return &megafonVATSSyncService{
		cfg:              cfg,
		log:              log,
		client:           client,
		repo:             repo,
		ticketRepo:       ticketRepo,
		userRepo:         userRepo,
		eventBus:         eventBus,
		recordingService: recordingService,
	}
}

func (s *megafonVATSSyncService) IsEnabled() bool {
	return s != nil && s.cfg != nil && s.cfg.EnableMegafonVATS && s.client != nil && s.client.IsConfigured() && s.repo != nil
}

func (s *megafonVATSSyncService) Start(ctx context.Context) {
	if !s.IsEnabled() {
		if s != nil && s.log != nil {
			s.log.Warn(
				"Мегафон ВАТС: фоновая синхронизация отключена",
				"feature_enabled", s.cfg != nil && s.cfg.EnableMegafonVATS,
				"client_configured", s.client != nil && s.client.IsConfigured(),
				"repo_ready", s.repo != nil,
			)
		}
		return
	}

	s.refreshAllSafe(ctx)

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
			s.refreshAllSafe(ctx)
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
	publishTelephonyLineUpdate(ctx, s.log, s.eventBus, s.repo, s.userRepo)

	return len(items), nil
}

func (s *megafonVATSSyncService) SyncHistory(ctx context.Context) (int, error) {
	return s.SyncHistoryByFilter(ctx, MegafonVATSHistorySyncFilter{})
}

func (s *megafonVATSSyncService) SyncHistoryByFilter(ctx context.Context, filter MegafonVATSHistorySyncFilter) (int, error) {
	if !s.IsEnabled() {
		return 0, nil
	}

	startedFrom, startedTo := normalizeTelephonyDateRange(filter.StartedFrom, filter.StartedTo, time.Now())
	historyFilter := megafonvats.HistoryFilter{
		Start:         formatMegafonHistoryDateTime(startedFrom),
		End:           formatMegafonHistoryDateTime(startedTo),
		User:          strings.TrimSpace(filter.EmployeeLogin),
		Client:        normalizeMegafonPhone(filter.ClientPhone),
		Groups:        sanitizeMegafonGroups(filter.Groups),
		ProcessMissed: true,
	}

	items, err := s.client.ListHistory(ctx, historyFilter)
	if err != nil {
		return 0, err
	}

	synced := 0
	for i := range items {
		applied, applyErr := s.syncHistoryRecord(ctx, &items[i])
		if applyErr != nil {
			return synced, applyErr
		}
		if applied {
			synced++
		}
	}
	if shouldMarkMegafonHistoryCoverage(filter) {
		if err := s.repo.MarkCallHistoryRangeCovered(
			ctx,
			telephony.ProviderMegafonVATS,
			normalizeMegafonEmployeeLoginPointer(filter.EmployeeLogin),
			startedFrom,
			startedTo,
			time.Now(),
		); err != nil && s.log != nil {
			s.log.Warn("Мегафон ВАТС: не удалось отметить покрытие локальной истории звонков", "error", err)
		}
	}
	publishTelephonyLineUpdate(ctx, s.log, s.eventBus, s.repo, s.userRepo)

	return synced, nil
}

func formatMegafonHistoryDateTime(value time.Time) string {
	return value.UTC().Format("20060102T150405Z")
}

func sanitizeMegafonGroups(groups []string) []string {
	if len(groups) == 0 {
		return nil
	}

	items := make([]string, 0, len(groups))
	for _, group := range groups {
		if trimmed := strings.TrimSpace(group); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

func shouldMarkMegafonHistoryCoverage(filter MegafonVATSHistorySyncFilter) bool {
	return strings.TrimSpace(filter.ClientPhone) == "" && len(sanitizeMegafonGroups(filter.Groups)) == 0
}

func normalizeMegafonEmployeeLoginPointer(login string) *string {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil
	}
	return &login
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

func (s *megafonVATSSyncService) refreshAllSafe(ctx context.Context) {
	s.refreshEmployeesSafe(ctx)
	s.syncHistorySafe(ctx)
	s.cleanupRecordingsSafe(ctx)
}

func (s *megafonVATSSyncService) refreshEmployeesSafe(ctx context.Context) {
	count, err := s.RefreshEmployees(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Error("Мегафон ВАТС: не удалось обновить сотрудников", "error", err)
		}
		return
	}
	if s.log != nil {
		s.log.Info("Мегафон ВАТС: обновлен справочник сотрудников", "count", count)
	}
}

func (s *megafonVATSSyncService) syncHistorySafe(ctx context.Context) {
	count, err := s.SyncHistory(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Error("Мегафон ВАТС: не удалось подтянуть историю звонков", "error", err)
		}
		return
	}
	if s.log != nil {
		s.log.Info("Мегафон ВАТС: синхронизирована история звонков", "count", count)
	}
}

func (s *megafonVATSSyncService) cleanupRecordingsSafe(ctx context.Context) {
	if s.recordingService == nil {
		return
	}
	count, err := s.recordingService.CleanupExpiredRecordings(ctx)
	if err != nil {
		if s.log != nil {
			s.log.Warn("Мегафон ВАТС: не удалось очистить локальные записи звонков", "error", err)
		}
		return
	}
	if count > 0 && s.log != nil {
		s.log.Info("Мегафон ВАТС: очищены просроченные локальные записи звонков", "count", count)
	}
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
		if !s.refreshMegafonUserIntegrations(ctx, &users[i], employeesByLogin) {
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

func (s *megafonVATSSyncService) refreshMegafonUserIntegrations(
	ctx context.Context,
	u *user.User,
	employeesByLogin map[string]telephony.ProviderEmployee,
) bool {
	if u == nil {
		return false
	}

	changed := false
	for i := range u.Integrations {
		integration := &u.Integrations[i]
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
			continue
		}

		login := strings.TrimSpace(integration.ExternalID)
		if !integration.IsEnabled {
			s.syncMegafonCallEmployeeUser(ctx, login, nil)
			if integration.IsVerified || integration.IsLocked || integration.VerifiedName != "" {
				integration.IsVerified = false
				integration.IsLocked = false
				integration.VerifiedName = ""
				changed = true
			}
			continue
		}

		employee, exists := employeesByLogin[login]
		if !exists {
			s.syncMegafonCallEmployeeUser(ctx, login, nil)
			if integration.IsVerified || integration.IsLocked || integration.VerifiedName != "" {
				integration.IsVerified = false
				integration.IsLocked = false
				integration.VerifiedName = ""
				changed = true
			}
			continue
		}

		s.syncMegafonCallEmployeeUser(ctx, login, &u.ID)
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

func (s *megafonVATSSyncService) syncMegafonCallEmployeeUser(ctx context.Context, login string, userID *uint) {
	if s == nil || s.repo == nil {
		return
	}
	if err := s.repo.SyncCallEmployeeUser(ctx, telephony.ProviderMegafonVATS, login, userID); err != nil && s.log != nil {
		s.log.Warn("Мегафон ВАТС: не удалось синхронизировать историю звонков с сотрудником", "login", strings.TrimSpace(login), "error", err)
	}
}

func (s *megafonVATSSyncService) syncHistoryRecord(ctx context.Context, item *megafonvats.HistoryRecord) (bool, error) {
	if item == nil {
		return false, nil
	}

	callID := strings.TrimSpace(item.UID)
	if callID == "" {
		if s.log != nil {
			s.log.Warn("Мегафон ВАТС: запись истории без uid пропущена")
		}
		return false, nil
	}

	payload := buildMegafonHistoryPayload(item)
	phone := normalizeMegafonPhone(payload.Phone)
	if phone == "" || !looksLikeExternalPhone(phone) {
		return false, nil
	}

	call, err := s.repo.GetCallByAnyExternalID(ctx, telephony.ProviderMegafonVATS, callID)
	if err != nil {
		return false, err
	}
	if call == nil {
		call = &telephony.Call{
			ID:             uuid.NewString(),
			Provider:       telephony.ProviderMegafonVATS,
			ExternalCallID: callID,
		}
	}

	rawJSON, err := json.Marshal(item)
	if err != nil {
		return false, err
	}

	call.RawSnapshot = string(rawJSON)
	call.UpdatedAt = time.Now()
	call.LastEventType = stringPtr(telephony.IncomingEventCommandHistory)

	if direction := resolveMegafonDirection(telephony.IncomingEventCommandHistory, payload); direction != "" {
		call.Direction = direction
	}
	call.ClientPhone = &phone
	if vatNumber := normalizeMegafonPhone(payload.Diversion); vatNumber != "" {
		call.VATNumber = &vatNumber
	}
	if employeeLogin := strings.TrimSpace(payload.User); employeeLogin != "" {
		call.EmployeeLogin = &employeeLogin
	}
	if groupName := strings.TrimSpace(payload.GroupRealName); groupName != "" {
		call.GroupName = &groupName
	}
	if missedStatus := strings.TrimSpace(payload.MissedStatus); missedStatus != "" {
		call.MissedStatus = &missedStatus
	}
	if recordingURL := strings.TrimSpace(payload.Link); recordingURL != "" {
		call.RecordingURL = &recordingURL
		call.HasRecording = true
	}

	if employeeUserID, ok, resolveErr := s.resolveMegafonUserID(ctx, payload.User); resolveErr != nil {
		return false, resolveErr
	} else if ok {
		call.EmployeeUserID = &employeeUserID
	}

	applyMegafonHistorySnapshot(call, payload)
	if err = s.repo.UpsertCall(ctx, call); err != nil {
		return false, err
	}
	contact, err := s.repo.EnsureContact(ctx, phone, phone)
	if err != nil {
		return false, err
	}
	if _, err = autoBindTelephonyCallToActiveTicket(ctx, s.repo, s.ticketRepo, call, contact); err != nil {
		return false, err
	}
	if s.recordingService != nil {
		if err = s.recordingService.SyncCallRecording(ctx, call.ID); err != nil && s.log != nil {
			s.log.Warn("Мегафон ВАТС: не удалось синхронизировать запись звонка", "call_id", call.ID, "external_call_id", callID, "error", err)
		}
	}

	return true, nil
}

func buildMegafonHistoryPayload(item *megafonvats.HistoryRecord) megafonVATSPayload {
	payload := megafonVATSPayload{
		HistoryType:   strings.TrimSpace(item.Type),
		CallID:        strings.TrimSpace(item.UID),
		Phone:         strings.TrimSpace(item.Client),
		User:          strings.TrimSpace(item.User),
		Diversion:     strings.TrimSpace(item.Diversion),
		GroupRealName: firstMegafonValue(item.GroupName, item.TelnumName),
		Status:        strings.TrimSpace(item.Status),
		Start:         strings.TrimSpace(item.Start),
		Link:          strings.TrimSpace(item.Record),
		WaitSeconds:   item.Wait,
	}
	if item != nil {
		payload.DurationSeconds = item.Duration
		if item.MissedStatus != nil {
			payload.MissedStatus = strconv.Itoa(*item.MissedStatus)
		}
	}
	return payload
}

func firstMegafonValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (s *megafonVATSSyncService) resolveMegafonUserID(ctx context.Context, login string) (uint, bool, error) {
	login = strings.TrimSpace(login)
	if login == "" || s.userRepo == nil {
		return 0, false, nil
	}

	users, err := s.userRepo.GetAll(ctx)
	if err != nil {
		return 0, false, err
	}
	for i := range users {
		for j := range users[i].Integrations {
			integration := users[i].Integrations[j]
			if !integration.IsEnabled {
				continue
			}
			if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
				continue
			}
			if strings.TrimSpace(integration.ExternalID) != login {
				continue
			}
			return users[i].ID, true, nil
		}
	}
	return 0, false, nil
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
