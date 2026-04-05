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
		&telephony.CallTicketLink{},
		&telephony.PendingContext{},
		&telephony.Contact{},
		&telephony.ContactCompanyLink{},
	))

	telephonyRepo := infraRepos.NewTelephonyRepo(db)
	ticketRepo := infraRepos.NewTicketRepo(db)
	userRepo := infraRepos.NewUserRepo(db)

	return &telephonyServiceTestEnv{
		service:       NewTelephonyService(nil, telephonyRepo, ticketRepo, nil, userRepo, nil),
		telephonyRepo: telephonyRepo,
		ticketRepo:    ticketRepo,
		userRepo:      userRepo,
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
