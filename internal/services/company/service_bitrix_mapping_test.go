package company

import (
	"context"
	"testing"

	"etalon-server/internal/domain/bitrix"
	domainCompany "etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/models"
	dbpkg "etalon-server/internal/infra/db"
	"etalon-server/internal/infra/logger"
	"etalon-server/internal/infra/repositories"
	contractsvc "etalon-server/internal/services/contract"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestUpdateBitrixMapping_AssignReassignAndClear(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&domainCompany.Company{},
		&contract.Contract{},
		&models.CompanyContract{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	companyRepo := repositories.NewCompanyRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	ctx := context.Background()

	title1 := "Компания 1"
	title2 := "Компания 2"
	company1 := &domainCompany.Company{Title: &title1}
	company2 := &domainCompany.Company{Title: &title2}
	if err := companyRepo.Create(ctx, company1); err != nil {
		t.Fatalf("не удалось создать company1: %v", err)
	}
	if err := companyRepo.Create(ctx, company2); err != nil {
		t.Fatalf("не удалось создать company2: %v", err)
	}

	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 101, Name: "Точка 101"}).Error; err != nil {
		t.Fatalf("не удалось создать точку 101: %v", err)
	}
	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 102, Name: "Точка 102"}).Error; err != nil {
		t.Fatalf("не удалось создать точку 102: %v", err)
	}

	svc := &serviceImpl{
		tm:          dbpkg.NewGormTransactor(db),
		companyRepo: companyRepo,
		bitrixRepo:  bitrixRepo,
	}

	point101 := int64(101)
	if err := svc.UpdateBitrixMapping(ctx, &company1.ID, &point101); err != nil {
		t.Fatalf("не удалось назначить mapping company1->101: %v", err)
	}

	item, err := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company1.ID)
	if err != nil || item == nil || item.BitrixServicePointID != 101 {
		t.Fatalf("ожидали mapping company1->101, получили item=%v err=%v", item, err)
	}

	if err := svc.UpdateBitrixMapping(ctx, &company2.ID, &point101); err != nil {
		t.Fatalf("не удалось назначить ту же точку company2: %v", err)
	}

	oldItem, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company1.ID)
	if oldItem == nil || oldItem.BitrixServicePointID != 101 {
		t.Fatalf("ожидали, что mapping company1->101 сохранится, получили %v", oldItem)
	}
	newItem, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company2.ID)
	if newItem == nil || newItem.BitrixServicePointID != 101 {
		t.Fatalf("ожидали mapping company2->101, получили %v", newItem)
	}

	if err := svc.UpdateBitrixMapping(ctx, &company2.ID, nil); err != nil {
		t.Fatalf("не удалось очистить mapping по company2: %v", err)
	}
	clearedByCompany, _ := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, company2.ID)
	if clearedByCompany != nil {
		t.Fatalf("ожидали, что mapping company2 будет очищен")
	}

	point102 := int64(102)
	if err := svc.UpdateBitrixMapping(ctx, &company1.ID, &point102); err != nil {
		t.Fatalf("не удалось назначить mapping company1->102: %v", err)
	}
	if err := svc.UpdateBitrixMapping(ctx, nil, &point102); err != nil {
		t.Fatalf("не удалось очистить mapping по точке 102: %v", err)
	}

	clearedByPoint, _ := bitrixRepo.GetCompanyServicePointMappingByPointID(ctx, 102)
	if clearedByPoint != nil {
		t.Fatalf("ожидали, что mapping по точке 102 будет очищен")
	}
}

func TestSyncBitrixContract_UpdatesCompanyContractFromMappedPoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&domainCompany.Company{},
		&contract.Contract{},
		&models.CompanyContract{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)
	transactor := dbpkg.NewGormTransactor(db)

	title := "Компания с ручной синхронизацией"
	activeContract := true
	comp := &domainCompany.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	oneCCode := "RU-100"
	contractType := "TS Cloud"
	contractOn := true
	if err := db.Create(&bitrix.ServicePoint{
		B24ElementID: 601,
		Name:         "Точка 601",
		OneCCode:     &oneCCode,
		ContractType: &contractType,
		ContractOn:   &contractOn,
	}).Error; err != nil {
		t.Fatalf("не удалось создать точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: 601,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	domainContractSvc := contractsvc.NewService(
		logger.New("", "test", "error", true),
		transactor,
		contractRepo,
		companyRepo,
		nil,
		nil,
		nil,
		nil,
	)
	svc := &serviceImpl{
		tm:          transactor,
		companyRepo: companyRepo,
		bitrixRepo:  bitrixRepo,
		contractSvc: domainContractSvc,
	}

	if err := svc.SyncBitrixContract(ctx, comp.ID); err != nil {
		t.Fatalf("SyncBitrixContract вернул ошибку: %v", err)
	}

	refreshed, err := companyRepo.GetByID(ctx, comp.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать компанию: %v", err)
	}
	if refreshed.ContractID == nil || *refreshed.ContractID == "" {
		t.Fatalf("ожидали, что у компании появится локальный контракт")
	}
	if refreshed.ContractType == nil || *refreshed.ContractType != "TS Cloud" {
		t.Fatalf("ожидали тип контракта TS Cloud, получили %v", refreshed.ContractType)
	}
}
