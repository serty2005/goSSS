package services

import (
	"context"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	megafonvats "etalon-server/internal/infra/plugins/megafonvats"
	infraRepos "etalon-server/internal/infra/repositories"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type fakeMegafonVATSDirectoryClient struct {
	users             []megafonvats.User
	history           []megafonvats.HistoryRecord
	lastHistoryFilter megafonvats.HistoryFilter
}

func (f *fakeMegafonVATSDirectoryClient) IsConfigured() bool {
	return true
}

func (f *fakeMegafonVATSDirectoryClient) ListUsers(_ context.Context, _ bool) ([]megafonvats.User, error) {
	return f.users, nil
}

func (f *fakeMegafonVATSDirectoryClient) GetUser(_ context.Context, login string, _ bool) (*megafonvats.User, error) {
	for i := range f.users {
		if f.users[i].Login == login {
			return &f.users[i], nil
		}
	}
	return nil, nil
}

func (f *fakeMegafonVATSDirectoryClient) ListHistory(_ context.Context, filter megafonvats.HistoryFilter) ([]megafonvats.HistoryRecord, error) {
	f.lastHistoryFilter = filter
	return f.history, nil
}

func TestMegafonVATSSyncService_RefreshEmployeesStoresDirectoryAndVerifiesIntegrations(t *testing.T) {
	service, telephonyRepo, userRepo := newMegafonVATSSyncTestEnv(t, []megafonvats.User{
		{
			Login:    "admin",
			Name:     "Иван Иванов",
			Ext:      "701",
			Telnum:   "74950000001",
			Status:   "online",
			Position: "Специалист",
		},
		{
			Login:  "user1",
			Name:   "Петр Петров",
			Ext:    "702",
			Status: "offline",
		},
	})

	u := &user.User{
		Username:     "ivanov",
		PasswordHash: "hash",
		FullName:     "Иван Иванов",
		FirstName:    "Иван",
		LastName:     "Иванов",
		Position:     user.RoleSupportSpecialist,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
		Integrations: []user.Integration{
			{
				IntegrationType: user.ExternalTypeMegafon,
				ExternalID:      "admin",
				IsEnabled:       true,
			},
		},
	}
	if err := userRepo.Create(context.Background(), u); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	count, err := service.RefreshEmployees(context.Background())
	if err != nil {
		t.Fatalf("RefreshEmployees вернул ошибку: %v", err)
	}
	if count != 2 {
		t.Fatalf("ожидали 2 сотрудников после синхронизации, получили %d", count)
	}

	items, err := telephonyRepo.ListProviderEmployees(context.Background(), telephony.ProviderMegafonVATS)
	if err != nil {
		t.Fatalf("не удалось получить кэш сотрудников: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ожидали 2 записи в кэше сотрудников, получили %d", len(items))
	}
	if items[0].Provider != telephony.ProviderMegafonVATS {
		t.Fatalf("ожидали provider=%q, получили %q", telephony.ProviderMegafonVATS, items[0].Provider)
	}

	updatedUser, err := userRepo.GetByID(context.Background(), u.ID)
	if err != nil {
		t.Fatalf("не удалось получить пользователя после синхронизации: %v", err)
	}
	if updatedUser == nil || len(updatedUser.Integrations) != 1 {
		t.Fatalf("ожидали одну интеграцию пользователя, получили %+v", updatedUser)
	}
	integration := updatedUser.Integrations[0]
	if !integration.IsVerified {
		t.Fatal("ожидали подтвержденную интеграцию Мегафон ВАТС")
	}
	if !integration.IsLocked {
		t.Fatal("ожидали заблокированную интеграцию после успешной верификации")
	}
	if integration.VerifiedName != "Иван Иванов" {
		t.Fatalf("ожидали VerifiedName=Иван Иванов, получили %q", integration.VerifiedName)
	}
}

func TestMegafonVATSSyncService_RefreshEmployeesBackfillsEmployeeUserIDForStoredCalls(t *testing.T) {
	service, telephonyRepo, userRepo := newMegafonVATSSyncTestEnv(t, []megafonvats.User{
		{
			Login:  "admin",
			Name:   "Иван Иванов",
			Status: "online",
		},
	})

	u := &user.User{
		Username:     "ivanov",
		PasswordHash: "hash",
		FullName:     "Иван Иванов",
		FirstName:    "Иван",
		LastName:     "Иванов",
		Position:     user.RoleSupportSpecialist,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
		Integrations: []user.Integration{
			{
				IntegrationType: user.ExternalTypeMegafon,
				ExternalID:      "admin",
				IsEnabled:       true,
			},
		},
	}
	if err := userRepo.Create(context.Background(), u); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	login := "admin"
	clientPhone := "79990001122"
	if err := telephonyRepo.UpsertCall(context.Background(), &telephony.Call{
		ID:             "call-backfill-1",
		Provider:       telephony.ProviderMegafonVATS,
		ExternalCallID: "call-backfill-1",
		Direction:      "incoming",
		Status:         "completed",
		ClientPhone:    &clientPhone,
		EmployeeLogin:  &login,
	}); err != nil {
		t.Fatalf("не удалось сохранить звонок без employee_user_id: %v", err)
	}

	if _, err := service.RefreshEmployees(context.Background()); err != nil {
		t.Fatalf("RefreshEmployees вернул ошибку: %v", err)
	}

	call, err := telephonyRepo.GetCallByExternalID(context.Background(), telephony.ProviderMegafonVATS, "call-backfill-1")
	if err != nil {
		t.Fatalf("не удалось получить звонок: %v", err)
	}
	if call == nil || call.EmployeeUserID == nil || *call.EmployeeUserID != u.ID {
		t.Fatalf("ожидали employee_user_id=%d после backfill, получили %+v", u.ID, call)
	}
}

func TestMegafonVATSSyncService_SearchEmployeesByNameFindsExactMatch(t *testing.T) {
	service, _, _ := newMegafonVATSSyncTestEnv(t, []megafonvats.User{
		{Login: "admin", Name: "Иван Иванов", Status: "online"},
		{Login: "petrov", Name: "Петр Петров", Status: "offline"},
	})

	if _, err := service.RefreshEmployees(context.Background()); err != nil {
		t.Fatalf("RefreshEmployees вернул ошибку: %v", err)
	}

	items, err := service.SearchEmployeesByName(context.Background(), "Иван", "Иванов", "Иван Иванов")
	if err != nil {
		t.Fatalf("SearchEmployeesByName вернул ошибку: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ожидали один найденный логин, получили %d", len(items))
	}
	if items[0].EmployeeLogin != "admin" {
		t.Fatalf("ожидали логин admin, получили %q", items[0].EmployeeLogin)
	}
}

func TestMegafonVATSSyncService_SyncHistoryCreatesCallWithoutWebhook(t *testing.T) {
	waitSeconds := 15
	durationSeconds := 120
	missedStatus := 2

	service, telephonyRepo, userRepo := newMegafonVATSSyncTestEnv(t, []megafonvats.User{
		{Login: "admin", Name: "Иван Иванов", Status: "online"},
	})
	service.client = &fakeMegafonVATSDirectoryClient{
		users: []megafonvats.User{
			{Login: "admin", Name: "Иван Иванов", Status: "online"},
		},
		history: []megafonvats.HistoryRecord{
			{
				UID:          "call-100",
				Type:         "in",
				Status:       "success",
				Client:       "+7 (999) 000-11-22",
				User:         "admin",
				GroupName:    "Техподдержка",
				Diversion:    "8 (495) 000-00-01",
				Start:        "2026-04-05T12:00:00Z",
				Wait:         &waitSeconds,
				Duration:     &durationSeconds,
				Record:       "https://example.com/record.mp3",
				MissedStatus: &missedStatus,
			},
		},
	}

	u := &user.User{
		Username:     "ivanov",
		PasswordHash: "hash",
		FullName:     "Иван Иванов",
		FirstName:    "Иван",
		LastName:     "Иванов",
		Position:     user.RoleSupportSpecialist,
		ScheduleType: user.ScheduleFiveTwo,
		IsActive:     true,
		Integrations: []user.Integration{
			{
				IntegrationType: user.ExternalTypeMegafon,
				ExternalID:      "admin",
				IsEnabled:       true,
			},
		},
	}
	if err := userRepo.Create(context.Background(), u); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	count, err := service.SyncHistory(context.Background())
	if err != nil {
		t.Fatalf("SyncHistory вернул ошибку: %v", err)
	}
	if count != 1 {
		t.Fatalf("ожидали 1 обработанный звонок, получили %d", count)
	}

	call, err := telephonyRepo.GetCallByExternalID(context.Background(), telephony.ProviderMegafonVATS, "call-100")
	if err != nil {
		t.Fatalf("не удалось получить звонок: %v", err)
	}
	if call == nil {
		t.Fatal("ожидали созданный звонок из history/json")
	}
	if call.EmployeeUserID == nil || *call.EmployeeUserID != u.ID {
		t.Fatalf("ожидали employee_user_id=%d, получили %+v", u.ID, call.EmployeeUserID)
	}
	if call.ClientPhone == nil || *call.ClientPhone != "79990001122" {
		t.Fatalf("ожидали нормализованный номер клиента 79990001122, получили %+v", call.ClientPhone)
	}
	if call.VATNumber == nil || *call.VATNumber != "74950000001" {
		t.Fatalf("ожидали нормализованный номер ВАТС 74950000001, получили %+v", call.VATNumber)
	}
	if call.Status != "success" {
		t.Fatalf("ожидали status=success, получили %q", call.Status)
	}
	if call.MissedStatus == nil || *call.MissedStatus != "2" {
		t.Fatalf("ожидали missed_status=2, получили %+v", call.MissedStatus)
	}
	if call.RecordingURL == nil || *call.RecordingURL != "https://example.com/record.mp3" {
		t.Fatalf("ожидали запись разговора, получили %+v", call.RecordingURL)
	}
	if call.AnsweredAt == nil || call.AnsweredAt.UTC().Format(time.RFC3339) != "2026-04-05T12:00:15Z" {
		t.Fatalf("ожидали answered_at=2026-04-05T12:00:15Z, получили %+v", call.AnsweredAt)
	}
	if call.CompletedAt == nil || call.CompletedAt.UTC().Format(time.RFC3339) != "2026-04-05T12:02:15Z" {
		t.Fatalf("ожидали completed_at=2026-04-05T12:02:15Z, получили %+v", call.CompletedAt)
	}
}

func TestMegafonVATSSyncService_SyncHistoryUpdatesMissedCallState(t *testing.T) {
	waitSeconds := 13
	durationSeconds := 0
	missedStatus := 3

	service, telephonyRepo, _ := newMegafonVATSSyncTestEnv(t, nil)
	service.client = &fakeMegafonVATSDirectoryClient{
		history: []megafonvats.HistoryRecord{
			{
				UID:          "call-missed",
				Type:         "in",
				Status:       "missed",
				Client:       "79990002233",
				GroupName:    "Техподдержка",
				Diversion:    "74950000002",
				Start:        "2026-04-05T13:00:00Z",
				Wait:         &waitSeconds,
				Duration:     &durationSeconds,
				Record:       "",
				MissedStatus: &missedStatus,
			},
		},
	}

	count, err := service.SyncHistory(context.Background())
	if err != nil {
		t.Fatalf("SyncHistory вернул ошибку: %v", err)
	}
	if count != 1 {
		t.Fatalf("ожидали 1 обработанный звонок, получили %d", count)
	}

	call, err := telephonyRepo.GetCallByExternalID(context.Background(), telephony.ProviderMegafonVATS, "call-missed")
	if err != nil {
		t.Fatalf("не удалось получить звонок: %v", err)
	}
	if call == nil {
		t.Fatal("ожидали созданный пропущенный звонок")
	}
	if call.AnsweredAt != nil {
		t.Fatalf("не ожидали answered_at у пропущенного звонка, получили %+v", call.AnsweredAt)
	}
	if call.CompletedAt == nil || call.CompletedAt.UTC().Format(time.RFC3339) != "2026-04-05T13:00:13Z" {
		t.Fatalf("ожидали completed_at=2026-04-05T13:00:13Z, получили %+v", call.CompletedAt)
	}
	if call.MissedStatus == nil || *call.MissedStatus != "3" {
		t.Fatalf("ожидали missed_status=3, получили %+v", call.MissedStatus)
	}
}

func TestMegafonVATSSyncService_SyncHistoryUsesLastTwentyFourHoursByDefault(t *testing.T) {
	service, _, _ := newMegafonVATSSyncTestEnv(t, nil)
	client := &fakeMegafonVATSDirectoryClient{}
	service.client = client

	if _, err := service.SyncHistory(context.Background()); err != nil {
		t.Fatalf("SyncHistory вернул ошибку: %v", err)
	}

	if client.lastHistoryFilter.Period != "" {
		t.Fatalf("не ожидали period в дефолтной синхронизации, получили %q", client.lastHistoryFilter.Period)
	}
	if client.lastHistoryFilter.Start == "" || client.lastHistoryFilter.End == "" {
		t.Fatalf("ожидали заполненные start/end, получили %+v", client.lastHistoryFilter)
	}
	if !client.lastHistoryFilter.ProcessMissed {
		t.Fatal("ожидали processMissed=true")
	}

	startedFrom, err := time.Parse("20060102T150405Z", client.lastHistoryFilter.Start)
	if err != nil {
		t.Fatalf("не удалось распарсить start: %v", err)
	}
	startedTo, err := time.Parse("20060102T150405Z", client.lastHistoryFilter.End)
	if err != nil {
		t.Fatalf("не удалось распарсить end: %v", err)
	}
	diff := startedTo.Sub(startedFrom)
	if diff < (23*time.Hour) || diff > (25*time.Hour) {
		t.Fatalf("ожидали диапазон около 24 часов, получили %v", diff)
	}
}

func TestMegafonVATSSyncService_SyncHistoryMarksCoveredRangeInLocalDB(t *testing.T) {
	service, telephonyRepo, _ := newMegafonVATSSyncTestEnv(t, nil)
	client := &fakeMegafonVATSDirectoryClient{}
	service.client = client

	startedFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	startedTo := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
	_, err := service.SyncHistoryByFilter(context.Background(), MegafonVATSHistorySyncFilter{
		StartedFrom:   &startedFrom,
		StartedTo:     &startedTo,
		EmployeeLogin: "admin",
	})
	if err != nil {
		t.Fatalf("SyncHistoryByFilter вернул ошибку: %v", err)
	}

	covered, err := telephonyRepo.IsCallHistoryRangeCovered(
		context.Background(),
		telephony.ProviderMegafonVATS,
		normalizeMegafonEmployeeLoginPointer("admin"),
		startedFrom,
		startedTo,
	)
	if err != nil {
		t.Fatalf("не удалось проверить покрытие истории звонков: %v", err)
	}
	if !covered {
		t.Fatal("ожидали покрытый диапазон истории звонков после синхронизации")
	}
}

func TestMegafonVATSSyncService_SyncHistoryByFilterUsesExplicitRange(t *testing.T) {
	service, _, _ := newMegafonVATSSyncTestEnv(t, nil)
	client := &fakeMegafonVATSDirectoryClient{}
	service.client = client

	startedFrom := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	startedTo := time.Date(2026, 4, 7, 23, 59, 59, 0, time.UTC)
	_, err := service.SyncHistoryByFilter(context.Background(), MegafonVATSHistorySyncFilter{
		StartedFrom:   &startedFrom,
		StartedTo:     &startedTo,
		EmployeeLogin: "admin",
		ClientPhone:   "+7 (999) 000-11-22",
		Groups:        []string{"Техподдержка", "  "},
	})
	if err != nil {
		t.Fatalf("SyncHistoryByFilter вернул ошибку: %v", err)
	}

	if client.lastHistoryFilter.Start != "20260401T000000Z" {
		t.Fatalf("ожидали start=20260401T000000Z, получили %q", client.lastHistoryFilter.Start)
	}
	if client.lastHistoryFilter.End != "20260407T235959Z" {
		t.Fatalf("ожидали end=20260407T235959Z, получили %q", client.lastHistoryFilter.End)
	}
	if client.lastHistoryFilter.User != "admin" {
		t.Fatalf("ожидали user=admin, получили %q", client.lastHistoryFilter.User)
	}
	if client.lastHistoryFilter.Client != "79990001122" {
		t.Fatalf("ожидали client=79990001122, получили %q", client.lastHistoryFilter.Client)
	}
	if len(client.lastHistoryFilter.Groups) != 1 || client.lastHistoryFilter.Groups[0] != "Техподдержка" {
		t.Fatalf("ожидали groups=[Техподдержка], получили %+v", client.lastHistoryFilter.Groups)
	}
}

func newMegafonVATSSyncTestEnv(
	t *testing.T,
	users []megafonvats.User,
) (*megafonVATSSyncService, telephony.Repository, user.Repository) {
	t.Helper()

	dbName := "file:megafon-vats-sync-test-" + time.Now().Format("20060102150405.000000000") + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dbName), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err = db.AutoMigrate(
		&company.Company{},
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&tickets.Ticket{},
		&telephony.ProviderEmployee{},
		&telephony.Call{},
		&telephony.CallTicketLink{},
		&telephony.CallHistorySyncWindow{},
		&telephony.CallEvent{},
		&telephony.CallArtifact{},
		&telephony.Contact{},
		&telephony.ContactCompanyLink{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему БД: %v", err)
	}

	telephonyRepo := infraRepos.NewTelephonyRepo(db)
	userRepo := infraRepos.NewUserRepo(db)
	service, ok := NewMegafonVATSSyncService(
		&config.Config{
			EnableMegafonVATS:       true,
			MegafonVATSSyncInterval: 5 * time.Minute,
		},
		logger.New("", "test", "error", true),
		&fakeMegafonVATSDirectoryClient{users: users},
		telephonyRepo,
		infraRepos.NewTicketRepo(db),
		userRepo,
		nil,
		nil,
	).(*megafonVATSSyncService)
	if !ok {
		t.Fatal("не удалось привести sync service Мегафон ВАТС к concrete type")
	}

	return service, telephonyRepo, userRepo
}
