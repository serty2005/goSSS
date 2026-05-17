package contract

import (
	"context"
	"encoding/json"
	"testing"

	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/company"
	contractdom "etalon-server/internal/domain/contract"
	dbinfra "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestService_SyncDailySnapshots_NormalizesContractTypeFromBitrixPoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&company.Company{}, &contractdom.Contract{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)

	activeContract := true
	title := "Компания для контракта"
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	svc := NewService(
		logger.New("", "test", "error", true),
		dbinfra.NewGormTransactor(db),
		contractRepo,
		companyRepo,
		nil,
		nil,
		nil,
		nil,
	)

	if err := svc.SyncDailySnapshots(ctx, []contractdom.DailyCompanyContractSnapshot{
		{
			CompanyID:    comp.ID,
			ContractType: "Да",
			Active:       true,
		},
	}); err != nil {
		t.Fatalf("SyncDailySnapshots завершился ошибкой: %v", err)
	}

	item, err := contractRepo.GetByID(ctx, mailManagedContractID(comp.ID))
	if err != nil {
		t.Fatalf("не удалось прочитать созданный контракт: %v", err)
	}
	if item == nil || item.State == nil || *item.State != "active" {
		t.Fatalf("ожидали активный контракт, получили %+v", item)
	}

	var services []string
	if err := json.Unmarshal(item.Services, &services); err != nil {
		t.Fatalf("не удалось распарсить services: %v", err)
	}
	if len(services) != 1 || services[0] != "TS Standart" {
		t.Fatalf("ожидали канонический тип TS Standart, получили %#v", services)
	}
}

func TestService_SyncDailySnapshots_RestoresSoftDeletedMailContract(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:TestServiceSyncDailySnapshotsRestoresSoftDeletedMailContract?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&company.Company{}, &contractdom.Contract{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := t.Context()
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)

	activeContract := true
	title := "Компания с удаленным почтовым контрактом"
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	contractID := mailManagedContractID(comp.ID)
	inactiveState := "inactive"
	if err := contractRepo.Create(ctx, &contractdom.Contract{
		Base: common.Base{
			ID:            contractID,
			LastUpdatedBy: contractMailSyncUpdatedBy,
		},
		State: &inactiveState,
	}); err != nil {
		t.Fatalf("не удалось создать исходный контракт: %v", err)
	}
	if _, err := contractRepo.Delete(ctx, contractID); err != nil {
		t.Fatalf("не удалось удалить исходный контракт: %v", err)
	}

	svc := NewService(
		logger.New("", "test", "error", true),
		dbinfra.NewGormTransactor(db),
		contractRepo,
		companyRepo,
		nil,
		nil,
		nil,
		nil,
	)

	if err := svc.SyncDailySnapshots(ctx, []contractdom.DailyCompanyContractSnapshot{
		{
			CompanyID:    comp.ID,
			ContractType: "Да",
			Active:       true,
		},
	}); err != nil {
		t.Fatalf("SyncDailySnapshots завершился ошибкой: %v", err)
	}

	item, err := contractRepo.GetByID(ctx, contractID)
	if err != nil {
		t.Fatalf("не удалось прочитать восстановленный контракт: %v", err)
	}
	if item == nil || item.State == nil || *item.State != "active" {
		t.Fatalf("ожидали восстановленный активный контракт, получили %+v", item)
	}
}

func TestService_SyncDailySnapshots_SkipsSoftDeletedCompany(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:TestServiceSyncDailySnapshotsSkipsSoftDeletedCompany?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&company.Company{}, &contractdom.Contract{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := t.Context()
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)

	title := "Удаленная компания с почтовым mapping"
	comp := &company.Company{Title: &title}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	contractID := mailManagedContractID(comp.ID)
	inactiveState := "inactive"
	if err := contractRepo.Create(ctx, &contractdom.Contract{
		Base: common.Base{
			ID:            contractID,
			LastUpdatedBy: contractMailSyncUpdatedBy,
		},
		State: &inactiveState,
	}); err != nil {
		t.Fatalf("не удалось создать исходный контракт: %v", err)
	}
	if _, err := contractRepo.Delete(ctx, contractID); err != nil {
		t.Fatalf("не удалось удалить исходный контракт: %v", err)
	}
	if _, err := companyRepo.Delete(ctx, comp.ID); err != nil {
		t.Fatalf("не удалось удалить компанию: %v", err)
	}

	svc := NewService(
		logger.New("", "test", "error", true),
		dbinfra.NewGormTransactor(db),
		contractRepo,
		companyRepo,
		nil,
		nil,
		nil,
		nil,
	)

	if err := svc.SyncDailySnapshots(ctx, []contractdom.DailyCompanyContractSnapshot{
		{
			CompanyID:    comp.ID,
			ContractType: "TS Standart",
			Active:       true,
		},
	}); err != nil {
		t.Fatalf("SyncDailySnapshots завершился ошибкой: %v", err)
	}

	item, err := contractRepo.GetByIDUnscoped(ctx, contractID)
	if err != nil {
		t.Fatalf("не удалось прочитать контракт: %v", err)
	}
	if item == nil || !item.DeletedAt.Valid {
		t.Fatalf("ожидали, что контракт удаленной компании останется удаленным, получили %+v", item)
	}
}
