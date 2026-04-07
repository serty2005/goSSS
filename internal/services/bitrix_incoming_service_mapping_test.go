package services

import (
	"context"
	"testing"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBitrixIncomingService_ResolveMappedCompanyIDByPoint_IgnoresTemporaryServicePoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&company.Company{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	companyRepo := repositories.NewCompanyRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	title := "Компания тестовой точки"
	comp := &company.Company{Title: &title}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	pointID := int64(1501)
	if err := db.Create(&bitrix.ServicePoint{B24ElementID: pointID, Name: bitrixTemporaryServicePointName}).Error; err != nil {
		t.Fatalf("не удалось создать тестовую точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: pointID,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	svc := &bitrixIncomingService{repo: bitrixRepo}
	mappedCompanyID, err := svc.resolveMappedCompanyIDByPoint(ctx, pointID)
	if err != nil {
		t.Fatalf("resolveMappedCompanyIDByPoint завершился ошибкой: %v", err)
	}
	if mappedCompanyID != "" {
		t.Fatalf("ожидали отсутствие локальной компании для тестовой точки, получили %q", mappedCompanyID)
	}
}

func TestBitrixIncomingService_CreateTicketFromDeal_IgnoresConfiguredTestCompany(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&company.Company{},
		&tickets.Ticket{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	companyRepo := repositories.NewCompanyRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	title := "Обычная компания"
	comp := &company.Company{Title: &title}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	pointID := int64(1601)
	if err := db.Create(&bitrix.ServicePoint{B24ElementID: pointID, Name: "Обычная точка"}).Error; err != nil {
		t.Fatalf("не удалось создать точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: pointID,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	svc := &bitrixIncomingService{
		cfg:        &config.Config{BitrixTestCompanyIDs: []int64{321}},
		log:        logger.New("", "test", "error", true),
		ticketRepo: ticketRepo,
		repo:       bitrixRepo,
	}

	deal := &b24.Deal{
		ID:      7001,
		Title:   "Тестовая сделка",
		StageID: "C17:NEW",
		Raw: map[string]interface{}{
			"UF_CRM_1766060620": "Описание из Bitrix24",
			"UF_CRM_1766062398": pointID,
			"COMPANY_ID":        int64(321),
		},
	}

	item, created, err := svc.createTicketFromDeal(ctx, deal, "")
	if err != nil {
		t.Fatalf("createTicketFromDeal завершился ошибкой: %v", err)
	}
	if !created {
		t.Fatal("ожидали создание нового тикета из сделки")
	}
	if item.CompanyID != "" {
		t.Fatalf("ожидали пустую локальную компанию для тестовой Bitrix-компании, получили %q", item.CompanyID)
	}
	if item.BitrixServicePointID == nil || *item.BitrixServicePointID != pointID {
		t.Fatalf("ожидали сохранённую точку %d, получили %v", pointID, item.BitrixServicePointID)
	}
}
