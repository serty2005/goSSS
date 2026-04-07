package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	infraRepos "etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type telephonyServiceTestEnv struct {
	service       TelephonyService
	telephonyRepo telephony.Repository
	ticketRepo    tickets.TicketRepository
	userRepo      user.Repository
	historySync   *fakeTelephonyHistorySyncService
}

type fakeTelephonyHistorySyncService struct {
	enabled    bool
	lastFilter MegafonVATSHistorySyncFilter
	callCount  int
}

func (f *fakeTelephonyHistorySyncService) IsEnabled() bool {
	return f != nil && f.enabled
}

func (f *fakeTelephonyHistorySyncService) Start(_ context.Context) {}

func (f *fakeTelephonyHistorySyncService) RefreshEmployees(_ context.Context) (int, error) {
	return 0, nil
}

func (f *fakeTelephonyHistorySyncService) SyncHistory(ctx context.Context) (int, error) {
	return f.SyncHistoryByFilter(ctx, MegafonVATSHistorySyncFilter{})
}

func (f *fakeTelephonyHistorySyncService) SyncHistoryByFilter(_ context.Context, filter MegafonVATSHistorySyncFilter) (int, error) {
	f.lastFilter = filter
	f.callCount++
	return 0, nil
}

func (f *fakeTelephonyHistorySyncService) ListCachedEmployees(_ context.Context) ([]telephony.ProviderEmployee, error) {
	return []telephony.ProviderEmployee{}, nil
}

func (f *fakeTelephonyHistorySyncService) SearchEmployeesByName(_ context.Context, _, _, _ string) ([]telephony.ProviderEmployee, error) {
	return []telephony.ProviderEmployee{}, nil
}

func (f *fakeTelephonyHistorySyncService) GetEmployee(_ context.Context, _ string) (*telephony.ProviderEmployee, error) {
	return nil, nil
}

func TestTelephonyServiceGetLineViewColorPriority(t *testing.T) {
	ctx := context.Background()
	env := newTelephonyServiceTestEnv(t)
	operator := createMegafonTestUser(t, ctx, env.userRepo, "alice", "Алиса")

	statusOnline := "online"
	require.NoError(t, env.telephonyRepo.ReplaceProviderEmployees(ctx, telephony.ProviderMegafonVATS, []telephony.ProviderEmployee{
		{
			EmployeeLogin: "alice",
			EmployeeName:  "Алиса",
			Status:        &statusOnline,
			LastSeenAt:    time.Now(),
		},
	}))

	lineView, err := env.service.GetLineView(ctx)
	require.NoError(t, err)
	require.NotNil(t, lineView)
	require.Equal(t, "green", lineView.Color)
	require.Equal(t, 1, lineView.OnLineCount)

	now := time.Now()
	clientPhone := "79990001122"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-active",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-active",
		Direction:      "incoming",
		Status:         "accepted",
		ClientPhone:    &clientPhone,
		EmployeeUserID: &operator.ID,
		StartedAt:      &now,
	}))

	lineView, err = env.service.GetLineView(ctx)
	require.NoError(t, err)
	require.NotNil(t, lineView)
	require.Equal(t, "yellow", lineView.Color)

	missedStatus := "3"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-missed",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-missed",
		Direction:      "incoming",
		Status:         "missed",
		MissedStatus:   &missedStatus,
		ClientPhone:    &clientPhone,
		StartedAt:      &now,
	}))

	lineView, err = env.service.GetLineView(ctx)
	require.NoError(t, err)
	require.NotNil(t, lineView)
	require.Equal(t, "blue", lineView.Color)
	require.Equal(t, 1, lineView.MissedOpenCount)
}

func TestTelephonyServiceGetLineViewSkipsEmployeesWithoutActiveMegafonIntegration(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)

	statusOnline := "online"
	require.NoError(t, env.telephonyRepo.ReplaceProviderEmployees(ctx, telephony.ProviderMegafonVATS, []telephony.ProviderEmployee{
		{
			Provider:      telephony.ProviderMegafonVATS,
			EmployeeLogin: "boris_gorbunov",
			EmployeeName:  "Борис Горбунов",
			Status:        &statusOnline,
			LastSeenAt:    time.Now(),
		},
	}))

	lineView, err := env.service.GetLineView(ctx)
	require.NoError(t, err)
	require.NotNil(t, lineView)
	require.Equal(t, "red", lineView.Color)
	require.Equal(t, 0, lineView.OnLineCount)
	require.Len(t, lineView.Employees, 0)
}

func TestTelephonyServiceBindPendingContextToTicket(t *testing.T) {
	ctx := context.Background()
	env := newTelephonyServiceTestEnv(t)
	operator := createMegafonTestUser(t, ctx, env.userRepo, "bob", "Боб")

	ticket := &tickets.Ticket{
		Subject:     "Проблема с кассой",
		Description: "Клиент сообщил об ошибке на кассе",
		Status:      tickets.StatusNew,
		Type:        tickets.TypeIncident,
		CompanyID:   "company-1",
	}
	require.NoError(t, env.ticketRepo.Create(ctx, ticket))

	clientPhone := "79995554433"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-local-1",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-external-1",
		Direction:      "incoming",
		Status:         "accepted",
		ClientPhone:    &clientPhone,
		EmployeeUserID: &operator.ID,
	}))
	require.NoError(t, env.telephonyRepo.UpsertPendingContext(ctx, &telephony.PendingContext{
		ID:             "pending-1",
		EmployeeUserID: operator.ID,
		ExternalCallID: "call-external-1",
		ClientPhone:    clientPhone,
		Status:         telephony.PendingContextStatusNew,
		ExpiresAt:      time.Now().Add(time.Hour),
	}))

	require.NoError(t, env.service.BindPendingContextToTicket(ctx, "pending-1", ticket.ID, operator.ID, nil))

	updatedTicket, err := env.ticketRepo.GetByID(ctx, ticket.ID)
	require.NoError(t, err)
	require.NotNil(t, updatedTicket)
	require.NotNil(t, updatedTicket.ContactID)

	contact, err := env.telephonyRepo.GetContactByID(ctx, *updatedTicket.ContactID)
	require.NoError(t, err)
	require.NotNil(t, contact)
	require.Equal(t, clientPhone, contact.PhoneNormalized)
	require.Equal(t, clientPhone, contact.PhoneDisplay)

	callLinks, err := env.telephonyRepo.ListCallTicketLinks(ctx, []string{"call-local-1"})
	require.NoError(t, err)
	require.Len(t, callLinks, 1)
	require.Equal(t, ticket.ID, callLinks[0].TicketID)

	companyLinks, err := env.telephonyRepo.ListContactCompanyLinks(ctx, contact.ID)
	require.NoError(t, err)
	require.Len(t, companyLinks, 1)
	require.Equal(t, ticket.CompanyID, companyLinks[0].CompanyID)

	pendingContext, err := env.telephonyRepo.GetPendingContextByID(ctx, "pending-1")
	require.NoError(t, err)
	require.NotNil(t, pendingContext)
	require.Equal(t, telephony.PendingContextStatusBound, pendingContext.Status)
	require.NotNil(t, pendingContext.LinkedTicketID)
	require.Equal(t, ticket.ID, *pendingContext.LinkedTicketID)
}

func TestTelephonyServiceListCallsAdminSeesAllCallsByDefault(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)
	firstUser := createMegafonTestUser(t, ctx, env.userRepo, "alice", "Алиса")
	secondUser := createMegafonTestUser(t, ctx, env.userRepo, "boris", "Борис")

	now := time.Now()
	firstPhone := "79990000001"
	secondPhone := "79990000002"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-admin-all-1",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-admin-all-1",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &firstPhone,
		EmployeeUserID: &firstUser.ID,
		StartedAt:      &now,
	}))
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-admin-all-2",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-admin-all-2",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &secondPhone,
		EmployeeUserID: &secondUser.ID,
		StartedAt:      &now,
	}))

	items, total, err := env.service.ListCalls(ctx, TelephonyCallFilter{}, 999, []string{user.RoleAdmin})
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, items, 2)
}

func TestTelephonyServiceListCallsAdminFiltersByEmployeeUserID(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)
	firstUser := createMegafonTestUser(t, ctx, env.userRepo, "alice", "Алиса")
	secondUser := createMegafonTestUser(t, ctx, env.userRepo, "boris", "Борис")

	now := time.Now()
	firstPhone := "79990000011"
	secondPhone := "79990000012"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-admin-filter-1",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-admin-filter-1",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &firstPhone,
		EmployeeUserID: &firstUser.ID,
		StartedAt:      &now,
	}))
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-admin-filter-2",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-admin-filter-2",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &secondPhone,
		EmployeeUserID: &secondUser.ID,
		StartedAt:      &now,
	}))

	items, total, err := env.service.ListCalls(ctx, TelephonyCallFilter{
		EmployeeUserID: &firstUser.ID,
	}, 999, []string{user.RoleAdmin})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.NotNil(t, items[0].Call.EmployeeUserID)
	require.Equal(t, firstUser.ID, *items[0].Call.EmployeeUserID)
}

func TestTelephonyServiceListCallsAppliesDefaultTwentyFourHourRange(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)
	operator := createMegafonTestUser(t, ctx, env.userRepo, "alice", "Алиса")

	oldStartedAt := time.Now().Add(-48 * time.Hour)
	recentStartedAt := time.Now().Add(-12 * time.Hour)
	oldPhone := "79990000101"
	recentPhone := "79990000102"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-old",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-old",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &oldPhone,
		EmployeeUserID: &operator.ID,
		StartedAt:      &oldStartedAt,
	}))
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-recent",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-recent",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &recentPhone,
		EmployeeUserID: &operator.ID,
		StartedAt:      &recentStartedAt,
	}))

	items, total, err := env.service.ListCalls(ctx, TelephonyCallFilter{}, 999, []string{user.RoleAdmin})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, "call-recent", items[0].Call.ExternalCallID)
}

func TestTelephonyServiceListCallsOnlyMissedReturnsOnlyMissedCalls(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)

	now := time.Now()
	missedStatus := "3"
	missedPhone := "79990000991"
	completedPhone := "79990000992"
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-missed-only",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-missed-only",
		Direction:      "incoming",
		Status:         "missed",
		MissedStatus:   &missedStatus,
		ClientPhone:    &missedPhone,
		StartedAt:      &now,
	}))
	require.NoError(t, env.telephonyRepo.UpsertCall(ctx, &telephony.Call{
		ID:             "call-completed-only",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-completed-only",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &completedPhone,
		StartedAt:      &now,
	}))

	items, total, err := env.service.ListCalls(ctx, TelephonyCallFilter{OnlyMissed: true}, 999, []string{user.RoleAdmin})
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, "call-missed-only", items[0].Call.ExternalCallID)
}

func TestTelephonyServiceListUserCallsSyncsHistoryByRequestedDateRange(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)
	operator := createMegafonTestUser(t, ctx, env.userRepo, "alice", "Алиса")

	startedFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	startedTo := time.Date(2026, 4, 7, 23, 59, 59, 0, time.UTC)

	_, _, err := env.service.ListUserCalls(ctx, operator.ID, TelephonyCallFilter{
		StartedFrom: &startedFrom,
		StartedTo:   &startedTo,
		ClientPhone: "+7 (999) 000-00-55",
		GroupNames:  []string{"Техподдержка"},
	}, operator.ID, []string{user.RoleSupportSpecialist})
	require.NoError(t, err)
	require.Equal(t, 1, env.historySync.callCount)
	require.Equal(t, "alice", env.historySync.lastFilter.EmployeeLogin)
	require.NotNil(t, env.historySync.lastFilter.StartedFrom)
	require.NotNil(t, env.historySync.lastFilter.StartedTo)
	require.Equal(t, startedFrom, *env.historySync.lastFilter.StartedFrom)
	require.Equal(t, startedTo, *env.historySync.lastFilter.StartedTo)
	require.Equal(t, "", env.historySync.lastFilter.ClientPhone)
	require.Nil(t, env.historySync.lastFilter.Groups)
}

func TestTelephonyServiceListCallsSkipsHistorySyncWhenRangeAlreadyCovered(t *testing.T) {
	ctx := t.Context()
	env := newTelephonyServiceTestEnv(t)

	startedFrom := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	startedTo := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	require.NoError(t, env.telephonyRepo.MarkCallHistoryRangeCovered(
		ctx,
		telephony.ProviderMegafonVATS,
		nil,
		startedFrom,
		startedTo,
		time.Now(),
	))

	_, _, err := env.service.ListCalls(ctx, TelephonyCallFilter{
		StartedFrom: &startedFrom,
		StartedTo:   &startedTo,
	}, 999, []string{user.RoleAdmin})
	require.NoError(t, err)
	require.Equal(t, 0, env.historySync.callCount)
}

func newTelephonyServiceTestEnv(t *testing.T) *telephonyServiceTestEnv {
	t.Helper()

	dsn := fmt.Sprintf("file:telephony-service-%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&company.Company{},
		&user.Role{},
		&user.User{},
		&user.Integration{},
		&tickets.Ticket{},
		&telephony.ProviderEmployee{},
		&telephony.Call{},
		&telephony.CallHistorySyncWindow{},
		&telephony.CallTicketLink{},
		&telephony.PendingContext{},
		&telephony.Contact{},
		&telephony.ContactCompanyLink{},
	))

	telephonyRepo := infraRepos.NewTelephonyRepo(db)
	ticketRepo := infraRepos.NewTicketRepo(db)
	userRepo := infraRepos.NewUserRepo(db)
	historySync := &fakeTelephonyHistorySyncService{enabled: true}

	return &telephonyServiceTestEnv{
		service:       NewTelephonyService(nil, telephonyRepo, ticketRepo, nil, userRepo, nil, historySync),
		telephonyRepo: telephonyRepo,
		ticketRepo:    ticketRepo,
		userRepo:      userRepo,
		historySync:   historySync,
	}
}

func createMegafonTestUser(t *testing.T, ctx context.Context, repo user.Repository, login string, fullName string) *user.User {
	t.Helper()

	u := &user.User{
		Username:     login,
		PasswordHash: "test",
		FullName:     fullName,
		FirstName:    fullName,
		Position:     user.RoleSupportSpecialist,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
	}
	require.NoError(t, repo.Create(ctx, u))
	require.NoError(t, repo.ReplaceIntegrations(ctx, u.ID, []user.Integration{
		{
			IntegrationType: user.ExternalTypeMegafon,
			ExternalID:      login,
			IsEnabled:       true,
		},
	}))

	created, err := repo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, created)
	return created
}
