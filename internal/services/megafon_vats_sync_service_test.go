package services

import (
	"context"
	"etalon-server/internal/domain/telephony"
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
	users []megafonvats.User
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
	if err = db.AutoMigrate(&user.User{}, &user.Role{}, &user.Integration{}, &telephony.ProviderEmployee{}); err != nil {
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
		userRepo,
	).(*megafonVATSSyncService)
	if !ok {
		t.Fatal("не удалось привести sync service Мегафон ВАТС к concrete type")
	}

	return service, telephonyRepo, userRepo
}
