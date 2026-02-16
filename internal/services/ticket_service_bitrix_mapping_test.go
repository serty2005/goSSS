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
		&config.Config{CommonContractID: "common-contract"},
		nil,
		nil,
		nil,
		bitrixRepo,
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
