package services

import (
	"context"
	"testing"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/repositories"
	api "etalon-server/internal/transport/http/dtos"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestCreateInternal_AutoSetBitrixServicePointFromMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&contract.Contract{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	author := &user.User{Username: "author", PasswordHash: "hash", FullName: "Автор"}
	assignee := &user.User{Username: "assignee", PasswordHash: "hash", FullName: "Исполнитель"}
	if err := userRepo.Create(context.Background(), author); err != nil {
		t.Fatalf("не удалось создать автора: %v", err)
	}
	if err := userRepo.Create(context.Background(), assignee); err != nil {
		t.Fatalf("не удалось создать исполнителя: %v", err)
	}

	title := "Компания автосопоставления"
	activeContract := false
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(context.Background(), comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 501, Name: "B24 Точка 501"}).Error; err != nil {
		t.Fatalf("не удалось создать точку B24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(context.Background(), &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: 501,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	svc := NewTicketService(
		nil,
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", EnableBitrixGateway: true},
		nil,
		nil,
		nil,
		bitrixRepo,
		nil,
	)

	dto := api.TicketCreateInternalDTO{
		Subject:         "Тест автосопоставления",
		Description:     "Описание",
		Type:            tickets.TypeIncident,
		CompanyID:       comp.ID,
		AssigneeID:      &assignee.ID,
		BitrixDealTitle: "Сделка для теста",
	}

	item, err := svc.CreateInternal(context.Background(), dto, author.ID)
	if err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	if item.BitrixServicePointID == nil || *item.BitrixServicePointID != 501 {
		t.Fatalf("ожидали автосопоставленную точку 501, получили %v", item.BitrixServicePointID)
	}
}

func TestChangeCompany_SavesBitrixCompanyServicePointMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&contract.Contract{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	actor := &user.User{Username: "actor_change_company", PasswordHash: "hash", FullName: "Оператор"}
	if err := userRepo.Create(context.Background(), actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	activeContract := false
	title1 := "Компания 1"
	title2 := "Компания 2"
	company1 := &company.Company{Title: &title1, ActiveContract: &activeContract}
	company2 := &company.Company{Title: &title2, ActiveContract: &activeContract}
	if err := companyRepo.Create(context.Background(), company1); err != nil {
		t.Fatalf("не удалось создать company1: %v", err)
	}
	if err := companyRepo.Create(context.Background(), company2); err != nil {
		t.Fatalf("не удалось создать company2: %v", err)
	}

	pointID := int64(777)
	ticket := &tickets.Ticket{
		Subject:              "Тикет из Б24",
		Description:          "Описание",
		Status:               tickets.StatusNew,
		Priority:             tickets.PriorityMedium,
		Type:                 tickets.TypeIncident,
		CompanyID:            company1.ID,
		ReporterName:         "Bitrix24",
		SyncWithBitrix:       true,
		BitrixDealTitle:      "Сделка",
		BitrixServicePointID: &pointID,
	}
	if err := ticketRepo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	svc := NewTicketService(
		nil,
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", EnableBitrixGateway: true},
		nil,
		nil,
		nil,
		bitrixRepo,
		nil,
	)

	if _, err := svc.ChangeCompany(context.Background(), ticket.ID, company2.ID, actor.ID); err != nil {
		t.Fatalf("ChangeCompany вернул ошибку: %v", err)
	}

	mapping, err := bitrixRepo.GetCompanyServicePointMappingByPointID(context.Background(), pointID)
	if err != nil {
		t.Fatalf("не удалось получить mapping по точке: %v", err)
	}
	if mapping == nil || mapping.CompanyID != company2.ID {
		t.Fatalf("ожидали mapping точки %d на company2, получили %+v", pointID, mapping)
	}
}

func TestUpdateBitrixFields_SavesBitrixCompanyServicePointMapping(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&contract.Contract{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
		&bitrix.ServicePoint{},
		&bitrix.CompanyServicePointMapping{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	actor := &user.User{Username: "actor_update_b24", PasswordHash: "hash", FullName: "Оператор"}
	if err := userRepo.Create(context.Background(), actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	activeContract := false
	title := "Компания для UpdateBitrixFields"
	comp := &company.Company{Title: &title, ActiveContract: &activeContract}
	if err := companyRepo.Create(context.Background(), comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:         "Тикет",
		Description:     "Описание",
		Status:          tickets.StatusNew,
		Priority:        tickets.PriorityMedium,
		Type:            tickets.TypeIncident,
		CompanyID:       comp.ID,
		ReporterName:    "Bitrix24",
		SyncWithBitrix:  false,
		BitrixDealTitle: "Сделка",
	}
	if err := ticketRepo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	svc := NewTicketService(
		nil,
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		&config.Config{CommonContractID: "common-contract", EnableBitrixGateway: true},
		nil,
		nil,
		nil,
		bitrixRepo,
		nil,
	)

	pointID := int64(888)
	if _, err := svc.UpdateBitrixFields(context.Background(), ticket.ID, &pointID, "Новый заголовок сделки", actor.ID); err != nil {
		t.Fatalf("UpdateBitrixFields вернул ошибку: %v", err)
	}

	mapping, err := bitrixRepo.GetCompanyServicePointMappingByCompanyID(context.Background(), comp.ID)
	if err != nil {
		t.Fatalf("не удалось получить mapping по компании: %v", err)
	}
	if mapping == nil || mapping.BitrixServicePointID != pointID {
		t.Fatalf("ожидали mapping компании %s на точку %d, получили %+v", comp.ID, pointID, mapping)
	}
}
