package services

import (
	"cmp"
	"context"
	"errors"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/logger"
	"sort"
	"strings"
	"time"

	"etalon-server/pkg/eventbus"
)

var ErrTelephonyForbidden = errors.New("недостаточно прав для работы с телефонией")

const telephonyHeaderMissedWindow = 12 * time.Hour

type TelephonyCallFilter struct {
	EmployeeUserID    *uint
	ClientPhone       string
	Statuses          []string
	GroupNames        []string
	StartedFrom       *time.Time
	StartedTo         *time.Time
	OnlyMissed        bool
	OnlyWithoutTicket bool
	Limit             int
	Offset            int
}

type TelephonyPendingContextView struct {
	PendingContext *telephony.PendingContext
	Contact        *telephony.Contact
	Call           *telephony.Call
}

type TelephonyContactCompanyView struct {
	CompanyID     string
	Title         string
	ParentTitle   string
	LastSeenAt    time.Time
	ActiveContact *bool
}

type TelephonyCallView struct {
	Call          telephony.Call
	Contact       *telephony.Contact
	EmployeeName  string
	EmployeeState string
	TicketID      *string
}

type TelephonyLineEmployeeView struct {
	UserID       *uint
	Login        string
	Name         string
	Status       string
	Provider     string
	ProviderExt  *string
	ProviderLine *string
}

type TelephonyLineView struct {
	Color           string
	OnLineCount     int
	MissedOpenCount int
	Employees       []TelephonyLineEmployeeView
}

type TelephonyService interface {
	GetPendingContextForUser(ctx context.Context, userID uint) (*TelephonyPendingContextView, error)
	BindPendingContextToTicket(ctx context.Context, pendingContextID string, ticketID string, contactName string, actorID uint, roles []string) error
	BindCallToTicket(ctx context.Context, callID string, ticketID string, contactName string, actorID uint, roles []string) error
	ListContactCompanies(ctx context.Context, contactID uint) ([]TelephonyContactCompanyView, error)
	ListCalls(ctx context.Context, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error)
	ListUserCalls(ctx context.Context, userID uint, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error)
	GetLineView(ctx context.Context) (*TelephonyLineView, error)
	PublishLineUpdate(ctx context.Context)
}

type telephonyService struct {
	log            logger.LoggerInterface
	telephonyRepo  telephony.Repository
	ticketRepo     tickets.TicketRepository
	companyRepo    company.Repository
	userRepo       user.Repository
	eventBus       eventbus.EventBus
	historySync    MegafonVATSSyncService
	bitrixContacts BitrixContactService
}

func NewTelephonyService(
	log logger.LoggerInterface,
	telephonyRepo telephony.Repository,
	ticketRepo tickets.TicketRepository,
	companyRepo company.Repository,
	userRepo user.Repository,
	eventBus eventbus.EventBus,
	historySync MegafonVATSSyncService,
	bitrixContacts BitrixContactService,
) TelephonyService {
	return &telephonyService{
		log:            log,
		telephonyRepo:  telephonyRepo,
		ticketRepo:     ticketRepo,
		companyRepo:    companyRepo,
		userRepo:       userRepo,
		eventBus:       eventBus,
		historySync:    historySync,
		bitrixContacts: bitrixContacts,
	}
}

func (s *telephonyService) GetPendingContextForUser(ctx context.Context, userID uint) (*TelephonyPendingContextView, error) {
	if s == nil || s.telephonyRepo == nil || userID == 0 {
		return nil, nil
	}

	now := time.Now()
	pending, err := s.telephonyRepo.GetActivePendingContextByUserID(ctx, userID, now)
	if err != nil || pending == nil {
		return nil, err
	}
	if !pending.ExpiresAt.After(now) {
		reason := "истёк срок действия контекста"
		_ = s.telephonyRepo.UpdatePendingContext(ctx, pending.ID, telephony.PendingContextStatusExpired, nil, &reason)
		return nil, nil
	}

	contact, err := s.ensurePendingContact(ctx, pending.ClientPhone, "")
	if err != nil {
		return nil, err
	}
	call, err := s.telephonyRepo.GetCallByExternalID(ctx, telephony.ProviderMegafonVATS, pending.ExternalCallID)
	if err != nil {
		return nil, err
	}

	return &TelephonyPendingContextView{
		PendingContext: pending,
		Contact:        contact,
		Call:           call,
	}, nil
}

func (s *telephonyService) BindPendingContextToTicket(ctx context.Context, pendingContextID string, ticketID string, contactName string, actorID uint, roles []string) error {
	if s == nil || s.telephonyRepo == nil || s.ticketRepo == nil {
		return nil
	}

	pending, err := s.telephonyRepo.GetPendingContextByID(ctx, pendingContextID)
	if err != nil {
		return err
	}
	if pending == nil {
		return telephonyErrNotFound("pending_context")
	}
	if !canAccessPendingContext(pending, actorID, roles) {
		return ErrTelephonyForbidden
	}
	if pending.Status != telephony.PendingContextStatusNew {
		return nil
	}
	if !pending.ExpiresAt.After(time.Now()) {
		reason := "истёк срок действия контекста"
		return s.telephonyRepo.UpdatePendingContext(ctx, pending.ID, telephony.PendingContextStatusExpired, nil, &reason)
	}

	ticket, _, err := s.bindTicketContactByPhone(ctx, ticketID, pending.ClientPhone, contactName)
	if err != nil {
		return err
	}

	call, err := s.telephonyRepo.GetCallByExternalID(ctx, telephony.ProviderMegafonVATS, pending.ExternalCallID)
	if err != nil {
		return err
	}
	if call != nil {
		if err = s.telephonyRepo.UpsertCallTicketLink(ctx, &telephony.CallTicketLink{
			TelephonyCallID: call.ID,
			TicketID:        ticket.ID,
		}); err != nil {
			return err
		}
	}

	reason := "тикет привязан оператором"
	return s.telephonyRepo.UpdatePendingContext(ctx, pending.ID, telephony.PendingContextStatusBound, &ticket.ID, &reason)
}

func (s *telephonyService) BindCallToTicket(ctx context.Context, callID string, ticketID string, contactName string, actorID uint, roles []string) error {
	if s == nil || s.telephonyRepo == nil || s.ticketRepo == nil {
		return nil
	}

	call, err := s.telephonyRepo.GetCallByID(ctx, strings.TrimSpace(callID))
	if err != nil {
		return err
	}
	if call == nil {
		return telephonyErrNotFound("call")
	}
	if !canAccessTelephonyCall(call, actorID, roles) {
		return ErrTelephonyForbidden
	}

	ticket, _, err := s.bindTicketContactByPhone(ctx, ticketID, safeTelephonyString(call.ClientPhone), contactName)
	if err != nil {
		return err
	}
	if err = s.telephonyRepo.UpsertCallTicketLink(ctx, &telephony.CallTicketLink{
		TelephonyCallID: call.ID,
		TicketID:        ticket.ID,
	}); err != nil {
		return err
	}

	pending, err := s.telephonyRepo.GetPendingContextByExternalCallID(ctx, call.ExternalCallID)
	if err != nil {
		return err
	}
	if pending == nil || strings.TrimSpace(pending.Status) == telephony.PendingContextStatusDismissed {
		return nil
	}

	reason := "тикет привязан оператором"
	return s.telephonyRepo.UpdatePendingContext(ctx, pending.ID, telephony.PendingContextStatusBound, &ticket.ID, &reason)
}

func (s *telephonyService) bindTicketContactByPhone(ctx context.Context, ticketID string, normalizedPhone string, contactName string) (*tickets.Ticket, *telephony.Contact, error) {
	ticket, err := s.ticketRepo.GetByID(ctx, strings.TrimSpace(ticketID))
	if err != nil {
		return nil, nil, err
	}
	if ticket == nil {
		return nil, nil, telephonyErrNotFound("ticket")
	}

	phone := strings.TrimSpace(normalizedPhone)
	if phone == "" {
		return ticket, nil, nil
	}

	contact, err := s.ensurePendingContact(ctx, phone, contactName)
	if err != nil {
		return nil, nil, err
	}
	if contact == nil {
		return ticket, nil, nil
	}

	switch {
	case ticket.ContactID == nil:
		ticket.ContactID = &contact.ID
	}
	if ticket.ContactID != nil && *ticket.ContactID == contact.ID {
		if err = s.ticketRepo.Update(ctx, ticket); err != nil {
			return nil, nil, err
		}
	}
	if strings.TrimSpace(ticket.CompanyID) != "" {
		if err = s.telephonyRepo.UpsertContactCompanyLink(ctx, contact.ID, ticket.CompanyID, time.Now()); err != nil {
			return nil, nil, err
		}
	}
	return ticket, contact, nil
}

func (s *telephonyService) ensurePendingContact(ctx context.Context, normalizedPhone string, contactName string) (*telephony.Contact, error) {
	if s == nil || s.telephonyRepo == nil {
		return nil, nil
	}

	preferredName := strings.TrimSpace(contactName)
	existing, err := s.telephonyRepo.GetContactByPhone(ctx, normalizedPhone)
	if err != nil {
		return nil, err
	}
	if preferredName == "" && existing != nil && existing.Name != nil {
		preferredName = strings.TrimSpace(*existing.Name)
	}

	contact, err := mergeTelephonyContactWithBitrix(ctx, s.telephonyRepo, normalizedPhone, normalizedPhone, preferredName, nil)
	if err != nil {
		return nil, err
	}
	if contact == nil {
		contact, err = s.telephonyRepo.EnsureContact(ctx, normalizedPhone, normalizedPhone)
		if err != nil {
			return nil, err
		}
	}

	if s.bitrixContacts == nil || contact == nil {
		return contact, nil
	}

	result, err := s.bitrixContacts.EnsureContactByPhone(ctx, BitrixEnsureContactInput{
		NormalizedPhone: contact.PhoneNormalized,
		DisplayPhone:    contact.PhoneDisplay,
		Name:            preferredName,
	})
	if err != nil {
		if s.log != nil {
			s.log.Warn("Телефония: не удалось синхронизировать контакт с Bitrix24", "phone", normalizedPhone, "error", err)
		}
		return contact, nil
	}

	updated, err := mergeTelephonyContactWithBitrix(ctx, s.telephonyRepo, contact.PhoneNormalized, contact.PhoneDisplay, preferredName, result)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		return updated, nil
	}
	return contact, nil
}

func (s *telephonyService) ListContactCompanies(ctx context.Context, contactID uint) ([]TelephonyContactCompanyView, error) {
	if s == nil || s.telephonyRepo == nil || s.companyRepo == nil || contactID == 0 {
		return []TelephonyContactCompanyView{}, nil
	}

	links, err := s.telephonyRepo.ListContactCompanyLinks(ctx, contactID)
	if err != nil || len(links) == 0 {
		return []TelephonyContactCompanyView{}, err
	}

	companyIDs := make([]string, 0, len(links))
	for _, item := range links {
		if strings.TrimSpace(item.CompanyID) == "" {
			continue
		}
		companyIDs = append(companyIDs, item.CompanyID)
	}
	companies, err := s.companyRepo.GetByIDs(ctx, companyIDs)
	if err != nil {
		return nil, err
	}
	companiesByID := make(map[string]company.Company, len(companies))
	for i := range companies {
		companiesByID[companies[i].ID] = companies[i]
	}

	items := make([]TelephonyContactCompanyView, 0, len(links))
	for _, link := range links {
		companyItem, ok := companiesByID[link.CompanyID]
		if !ok {
			continue
		}
		items = append(items, TelephonyContactCompanyView{
			CompanyID:     companyItem.ID,
			Title:         cmp.Or(strings.TrimSpace(safeTelephonyString(companyItem.Title)), strings.TrimSpace(safeTelephonyString(companyItem.AdditionalName)), companyItem.ID),
			ParentTitle:   strings.TrimSpace(safeTelephonyString(companyItem.ParentTitle)),
			LastSeenAt:    link.LastSeenAt,
			ActiveContact: companyItem.ActiveContract,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].LastSeenAt.Equal(items[j].LastSeenAt) {
			return items[i].CompanyID < items[j].CompanyID
		}
		return items[i].LastSeenAt.After(items[j].LastSeenAt)
	})
	return items, nil
}

func (s *telephonyService) ListCalls(ctx context.Context, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error) {
	if !hasUserRole(roles, user.RoleAdmin) {
		return nil, 0, ErrTelephonyForbidden
	}
	return s.listCalls(ctx, nil, filter)
}

func (s *telephonyService) ListUserCalls(ctx context.Context, userID uint, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error) {
	if userID == 0 {
		return []TelephonyCallView{}, 0, nil
	}
	if actorID != userID && !hasUserRole(roles, user.RoleAdmin) {
		return nil, 0, ErrTelephonyForbidden
	}
	return s.listCalls(ctx, &userID, filter)
}

func (s *telephonyService) GetLineView(ctx context.Context) (*TelephonyLineView, error) {
	return buildTelephonyLineView(ctx, s.telephonyRepo, s.userRepo)
}

func (s *telephonyService) PublishLineUpdate(ctx context.Context) {
	publishTelephonyLineUpdate(ctx, s.log, s.eventBus, s.telephonyRepo, s.userRepo)
}

func (s *telephonyService) listCalls(ctx context.Context, userID *uint, filter TelephonyCallFilter) ([]TelephonyCallView, int64, error) {
	if s == nil || s.telephonyRepo == nil {
		return []TelephonyCallView{}, 0, nil
	}

	filter = normalizeTelephonyCallFilter(filter, time.Now())
	effectiveEmployeeUserID := filter.EmployeeUserID
	if userID != nil {
		effectiveEmployeeUserID = userID
	}
	s.syncMegafonHistoryForCalls(ctx, effectiveEmployeeUserID, filter)

	items, total, err := s.telephonyRepo.ListCalls(ctx, telephony.CallListFilter{
		Provider:          telephony.ProviderMegafonVATS,
		EmployeeUserID:    effectiveEmployeeUserID,
		ClientPhone:       strings.TrimSpace(filter.ClientPhone),
		Statuses:          filter.Statuses,
		GroupNames:        filter.GroupNames,
		StartedFrom:       filter.StartedFrom,
		StartedTo:         filter.StartedTo,
		OnlyMissed:        filter.OnlyMissed,
		OnlyWithoutTicket: filter.OnlyWithoutTicket,
		Limit:             filter.Limit,
		Offset:            filter.Offset,
	})
	if err != nil || len(items) == 0 {
		return []TelephonyCallView{}, total, err
	}

	callIDs := make([]string, 0, len(items))
	for _, item := range items {
		callIDs = append(callIDs, item.ID)
	}
	links, err := s.telephonyRepo.ListCallTicketLinks(ctx, callIDs)
	if err != nil {
		return nil, 0, err
	}
	linkByCallID := make(map[string]string, len(links))
	for i := range links {
		linkByCallID[links[i].TelephonyCallID] = links[i].TicketID
	}

	employees, err := loadMegafonIntegratedEmployees(ctx, s.telephonyRepo, s.userRepo)
	if err != nil {
		return nil, 0, err
	}
	employeesByLogin := make(map[string]TelephonyLineEmployeeView, len(employees))
	employeesByUserID := make(map[uint]TelephonyLineEmployeeView, len(employees))
	for _, item := range employees {
		employeesByLogin[item.Login] = item
		if item.UserID != nil {
			employeesByUserID[*item.UserID] = item
		}
	}

	contactsByPhone := make(map[string]*telephony.Contact, len(items))
	for _, item := range items {
		phone := strings.TrimSpace(safeTelephonyString(item.ClientPhone))
		if phone == "" {
			continue
		}
		if _, ok := contactsByPhone[phone]; ok {
			continue
		}
		contact, contactErr := s.telephonyRepo.GetContactByPhone(ctx, phone)
		if contactErr != nil {
			return nil, 0, contactErr
		}
		contactsByPhone[phone] = contact
	}

	result := make([]TelephonyCallView, 0, len(items))
	for _, item := range items {
		view := TelephonyCallView{Call: item}
		if item.EmployeeUserID != nil {
			if employee, ok := employeesByUserID[*item.EmployeeUserID]; ok {
				view.EmployeeName = employee.Name
				view.EmployeeState = employee.Status
			}
		}
		if view.EmployeeName == "" && item.EmployeeLogin != nil {
			if employee, ok := employeesByLogin[strings.TrimSpace(*item.EmployeeLogin)]; ok {
				view.EmployeeName = employee.Name
				view.EmployeeState = employee.Status
			}
		}
		if ticketID, ok := linkByCallID[item.ID]; ok {
			view.TicketID = &ticketID
		}
		if phone := strings.TrimSpace(safeTelephonyString(item.ClientPhone)); phone != "" {
			view.Contact = contactsByPhone[phone]
		}
		result = append(result, view)
	}
	return result, total, nil
}

func normalizeTelephonyCallFilter(filter TelephonyCallFilter, now time.Time) TelephonyCallFilter {
	startedFrom, startedTo := normalizeTelephonyDateRange(filter.StartedFrom, filter.StartedTo, now)
	filter.StartedFrom = &startedFrom
	filter.StartedTo = &startedTo
	return filter
}

func normalizeTelephonyDateRange(startedFrom *time.Time, startedTo *time.Time, now time.Time) (time.Time, time.Time) {
	switch {
	case startedFrom == nil && startedTo == nil:
		return now.Add(-24 * time.Hour), now
	case startedFrom == nil:
		return beginningOfDay(*startedTo), *startedTo
	case startedTo == nil:
		return *startedFrom, endOfDay(*startedFrom)
	default:
		if startedTo.Before(*startedFrom) {
			return *startedTo, *startedFrom
		}
		return *startedFrom, *startedTo
	}
}

func beginningOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func endOfDay(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 23, 59, 59, 0, value.Location())
}

func (s *telephonyService) syncMegafonHistoryForCalls(ctx context.Context, employeeUserID *uint, filter TelephonyCallFilter) {
	if s == nil || s.telephonyRepo == nil || s.historySync == nil || !s.historySync.IsEnabled() {
		return
	}
	if filter.StartedFrom == nil || filter.StartedTo == nil {
		return
	}

	var employeeLogin *string
	if employeeUserID != nil {
		login, err := s.resolveMegafonLogin(ctx, *employeeUserID)
		if err != nil {
			if s.log != nil {
				s.log.Warn("Телефония: не удалось определить логин сотрудника для backfill истории Мегафон", "user_id", *employeeUserID, "error", err)
			}
			return
		}
		login = strings.TrimSpace(login)
		if login == "" {
			return
		}
		employeeLogin = &login
	}

	covered, err := s.telephonyRepo.IsCallHistoryRangeCovered(
		ctx,
		telephony.ProviderMegafonVATS,
		employeeLogin,
		*filter.StartedFrom,
		*filter.StartedTo,
	)
	if err != nil {
		if s.log != nil {
			s.log.Warn("Телефония: не удалось проверить локальное покрытие истории звонков", "error", err)
		}
		return
	}
	if covered {
		return
	}

	historyFilter := MegafonVATSHistorySyncFilter{
		StartedFrom: filter.StartedFrom,
		StartedTo:   filter.StartedTo,
	}
	if employeeLogin != nil {
		historyFilter.EmployeeLogin = *employeeLogin
	}

	if _, err := s.historySync.SyncHistoryByFilter(ctx, historyFilter); err != nil && s.log != nil {
		s.log.Warn("Телефония: не удалось подтянуть историю звонков Мегафон по фильтру", "error", err)
	}
}

func (s *telephonyService) resolveMegafonLogin(ctx context.Context, userID uint) (string, error) {
	if s == nil || s.userRepo == nil || userID == 0 {
		return "", nil
	}

	item, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || item == nil {
		return "", err
	}
	for i := range item.Integrations {
		integration := item.Integrations[i]
		if !integration.IsEnabled {
			continue
		}
		if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
			continue
		}
		return strings.TrimSpace(integration.ExternalID), nil
	}
	return "", nil
}

func publishTelephonyLineUpdate(
	ctx context.Context,
	log logger.LoggerInterface,
	bus eventbus.EventBus,
	telephonyRepo telephony.Repository,
	userRepo user.Repository,
) {
	if bus == nil || telephonyRepo == nil || userRepo == nil {
		return
	}
	lineView, err := buildTelephonyLineView(ctx, telephonyRepo, userRepo)
	if err != nil {
		if log != nil {
			log.Warn("Телефония: не удалось опубликовать обновление линии", "error", err)
		}
		return
	}
	payload := events.TelephonyLineUpdatedPayload{
		Color:           lineView.Color,
		OnLineCount:     lineView.OnLineCount,
		MissedOpenCount: lineView.MissedOpenCount,
		Employees:       make([]events.TelephonyLineEmployeePayload, 0, len(lineView.Employees)),
		OccurredAt:      time.Now(),
	}
	for _, item := range lineView.Employees {
		payload.Employees = append(payload.Employees, events.TelephonyLineEmployeePayload{
			UserID:       item.UserID,
			Login:        item.Login,
			Name:         item.Name,
			Status:       item.Status,
			Provider:     item.Provider,
			ProviderExt:  item.ProviderExt,
			ProviderLine: item.ProviderLine,
		})
	}
	bus.Publish(eventbus.Event{
		Type:    events.TelephonyLineUpdated,
		Payload: payload,
	})
}

func buildTelephonyLineView(ctx context.Context, telephonyRepo telephony.Repository, userRepo user.Repository) (*TelephonyLineView, error) {
	employees, err := loadMegafonIntegratedEmployees(ctx, telephonyRepo, userRepo)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	missedStartedFrom := now.Add(-telephonyHeaderMissedWindow)

	activeCalls, _, err := telephonyRepo.ListCalls(ctx, telephony.CallListFilter{
		Provider: telephony.ProviderMegafonVATS,
		Statuses: []string{"incoming", "accepted", "outgoing", "transferred"},
		Limit:    500,
	})
	if err != nil {
		return nil, err
	}

	inCallByLogin := make(map[string]struct{}, len(activeCalls))
	inCallByUserID := make(map[uint]struct{}, len(activeCalls))
	for _, item := range activeCalls {
		if item.CompletedAt != nil {
			continue
		}
		if item.EmployeeLogin != nil && strings.TrimSpace(*item.EmployeeLogin) != "" {
			inCallByLogin[strings.TrimSpace(*item.EmployeeLogin)] = struct{}{}
		}
		if item.EmployeeUserID != nil && *item.EmployeeUserID > 0 {
			inCallByUserID[*item.EmployeeUserID] = struct{}{}
		}
	}

	missedCalls, _, err := telephonyRepo.ListCalls(ctx, telephony.CallListFilter{
		Provider:    telephony.ProviderMegafonVATS,
		OnlyMissed:  true,
		StartedFrom: &missedStartedFrom,
		StartedTo:   &now,
		Limit:       500,
	})
	if err != nil {
		return nil, err
	}

	missedOpenCount := 0
	for _, item := range missedCalls {
		if isMegafonOpenMissedCall(item) {
			missedOpenCount++
		}
	}

	onLineCount := 0
	hasInCall := false
	for i := range employees {
		employee := &employees[i]
		if employee.UserID != nil {
			if _, ok := inCallByUserID[*employee.UserID]; ok {
				employee.Status = "in_call"
			}
		}
		if employee.Status != "in_call" {
			if _, ok := inCallByLogin[employee.Login]; ok {
				employee.Status = "in_call"
			}
		}
		if employee.Status == "in_call" {
			hasInCall = true
		}
		if employee.Status == "online" || employee.Status == "in_call" {
			onLineCount++
		}
	}

	color := "red"
	switch {
	case missedOpenCount > 0:
		color = "blue"
	case hasInCall:
		color = "yellow"
	case onLineCount > 0:
		color = "green"
	}

	return &TelephonyLineView{
		Color:           color,
		OnLineCount:     onLineCount,
		MissedOpenCount: missedOpenCount,
		Employees:       employees,
	}, nil
}

func loadMegafonIntegratedEmployees(
	ctx context.Context,
	telephonyRepo telephony.Repository,
	userRepo user.Repository,
) ([]TelephonyLineEmployeeView, error) {
	if telephonyRepo == nil || userRepo == nil {
		return []TelephonyLineEmployeeView{}, nil
	}
	providerEmployees, err := telephonyRepo.ListProviderEmployees(ctx, telephony.ProviderMegafonVATS)
	if err != nil {
		return nil, err
	}
	users, err := userRepo.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	usersByLogin := make(map[string]user.User, len(users))
	for i := range users {
		for j := range users[i].Integrations {
			integration := users[i].Integrations[j]
			if !integration.IsEnabled {
				continue
			}
			if strings.TrimSpace(strings.ToLower(integration.IntegrationType)) != user.ExternalTypeMegafon {
				continue
			}
			login := strings.TrimSpace(integration.ExternalID)
			if login == "" {
				continue
			}
			usersByLogin[login] = users[i]
		}
	}

	items := make([]TelephonyLineEmployeeView, 0, len(providerEmployees))
	for i := range providerEmployees {
		login := strings.TrimSpace(providerEmployees[i].EmployeeLogin)
		localUser, ok := usersByLogin[login]
		if !ok {
			continue
		}
		status := "offline"
		if strings.EqualFold(strings.TrimSpace(safeTelephonyString(providerEmployees[i].Status)), "online") {
			status = "online"
		}
		item := TelephonyLineEmployeeView{
			Login:        login,
			Name:         cmp.Or(strings.TrimSpace(providerEmployees[i].EmployeeName), strings.TrimSpace(localUser.FullName), login),
			Status:       status,
			Provider:     providerEmployees[i].Provider,
			ProviderExt:  providerEmployees[i].Ext,
			ProviderLine: providerEmployees[i].Telnum,
		}
		item.UserID = &localUser.ID
		items = append(items, item)
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Status == items[j].Status {
			return items[i].Name < items[j].Name
		}
		return telephonyLineStatusRank(items[i].Status) > telephonyLineStatusRank(items[j].Status)
	})
	return items, nil
}

func canAccessPendingContext(item *telephony.PendingContext, actorID uint, roles []string) bool {
	if item == nil {
		return false
	}
	return actorID == item.EmployeeUserID || hasUserRole(roles, user.RoleAdmin)
}

func canAccessTelephonyCall(item *telephony.Call, actorID uint, roles []string) bool {
	if item == nil {
		return false
	}
	if hasUserRole(roles, user.RoleAdmin) {
		return true
	}
	return item.EmployeeUserID != nil && actorID == *item.EmployeeUserID
}

func isMegafonOpenMissedCall(item telephony.Call) bool {
	switch strings.TrimSpace(safeTelephonyString(item.MissedStatus)) {
	case "3", "4":
		return true
	}
	return false
}

func telephonyLineStatusRank(status string) int {
	switch strings.TrimSpace(status) {
	case "in_call":
		return 3
	case "online":
		return 2
	default:
		return 1
	}
}

func safeTelephonyString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func telephonyErrNotFound(entity string) error {
	return errors.New(entity + " не найден")
}
