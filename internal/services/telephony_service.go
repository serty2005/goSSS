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

type TelephonyCallFilter struct {
	EmployeeUserID    *uint
	ClientPhone       string
	Statuses          []string
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
	BindPendingContextToTicket(ctx context.Context, pendingContextID string, ticketID string, actorID uint, roles []string) error
	ListContactCompanies(ctx context.Context, contactID uint) ([]TelephonyContactCompanyView, error)
	ListCalls(ctx context.Context, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error)
	ListUserCalls(ctx context.Context, userID uint, filter TelephonyCallFilter, actorID uint, roles []string) ([]TelephonyCallView, int64, error)
	GetLineView(ctx context.Context) (*TelephonyLineView, error)
	PublishLineUpdate(ctx context.Context)
}

type telephonyService struct {
	log           logger.LoggerInterface
	telephonyRepo telephony.Repository
	ticketRepo    tickets.TicketRepository
	companyRepo   company.Repository
	userRepo      user.Repository
	eventBus      eventbus.EventBus
}

func NewTelephonyService(
	log logger.LoggerInterface,
	telephonyRepo telephony.Repository,
	ticketRepo tickets.TicketRepository,
	companyRepo company.Repository,
	userRepo user.Repository,
	eventBus eventbus.EventBus,
) TelephonyService {
	return &telephonyService{
		log:           log,
		telephonyRepo: telephonyRepo,
		ticketRepo:    ticketRepo,
		companyRepo:   companyRepo,
		userRepo:      userRepo,
		eventBus:      eventBus,
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

	contact, err := s.telephonyRepo.GetContactByPhone(ctx, strings.TrimSpace(pending.ClientPhone))
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

func (s *telephonyService) BindPendingContextToTicket(ctx context.Context, pendingContextID string, ticketID string, actorID uint, roles []string) error {
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

	ticket, err := s.ticketRepo.GetByID(ctx, strings.TrimSpace(ticketID))
	if err != nil {
		return err
	}
	if ticket == nil {
		return telephonyErrNotFound("ticket")
	}

	contact, err := s.telephonyRepo.EnsureContact(ctx, pending.ClientPhone, pending.ClientPhone)
	if err != nil {
		return err
	}
	if contact != nil {
		switch {
		case ticket.ContactID == nil:
			ticket.ContactID = &contact.ID
		case *ticket.ContactID != contact.ID:
			return errors.New("тикет уже привязан к другому контакту")
		}
		if err = s.ticketRepo.Update(ctx, ticket); err != nil {
			return err
		}
		if strings.TrimSpace(ticket.CompanyID) != "" {
			if err = s.telephonyRepo.UpsertContactCompanyLink(ctx, contact.ID, ticket.CompanyID, time.Now()); err != nil {
				return err
			}
		}
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

	items, total, err := s.telephonyRepo.ListCalls(ctx, telephony.CallListFilter{
		Provider:          telephony.ProviderMegafonVATS,
		EmployeeUserID:    cmp.Or(userID, filter.EmployeeUserID),
		ClientPhone:       strings.TrimSpace(filter.ClientPhone),
		Statuses:          filter.Statuses,
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
		result = append(result, view)
	}
	return result, total, nil
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
		Provider:   telephony.ProviderMegafonVATS,
		OnlyMissed: true,
		Limit:      500,
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
		items = append(items, TelephonyLineEmployeeView{
			UserID:       &localUser.ID,
			Login:        login,
			Name:         cmp.Or(strings.TrimSpace(providerEmployees[i].EmployeeName), strings.TrimSpace(localUser.FullName), login),
			Status:       status,
			Provider:     providerEmployees[i].Provider,
			ProviderExt:  providerEmployees[i].Ext,
			ProviderLine: providerEmployees[i].Telnum,
		})
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
