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
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РѕС‚РєСЂС‹С‚СЊ in-memory Р‘Р”: %v", err)
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
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ РїРѕРґРіРѕС‚РѕРІРёС‚СЊ СЃС…РµРјСѓ: %v", err)
	}

	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	author := &user.User{Username: "author", PasswordHash: "hash", FullName: "РђРІС‚РѕСЂ"}
	assignee := &user.User{Username: "assignee", PasswordHash: "hash", FullName: "РСЃРїРѕР»РЅРёС‚РµР»СЊ"}
	if err := userRepo.Create(context.Background(), author); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ Р°РІС‚РѕСЂР°: %v", err)
	}
	if err := userRepo.Create(context.Background(), assignee); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ РёСЃРїРѕР»РЅРёС‚РµР»СЏ: %v", err)
	}

	title := "РљРѕРјРїР°РЅРёСЏ Р°РІС‚РѕСЃРѕРїРѕСЃС‚Р°РІР»РµРЅРёСЏ"
	activeContract := false
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(context.Background(), comp); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ РєРѕРјРїР°РЅРёСЋ: %v", err)
	}

	if err := db.Create(&bitrix.ServicePoint{B24ElementID: 501, Name: "B24 РўРѕС‡РєР° 501"}).Error; err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ С‚РѕС‡РєСѓ B24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(context.Background(), &bitrix.CompanyServicePointMapping{
		CompanyID:            comp.ID,
		BitrixServicePointID: 501,
	}); err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ mapping: %v", err)
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
		Subject:         "РўРµСЃС‚ Р°РІС‚РѕСЃРѕРїРѕСЃС‚Р°РІР»РµРЅРёСЏ",
		Description:     "РћРїРёСЃР°РЅРёРµ",
		Type:            tickets.TypeIncident,
		CompanyID:       comp.ID,
		AssigneeID:      &assignee.ID,
		BitrixDealTitle: "РЎРґРµР»РєР° РґР»СЏ С‚РµСЃС‚Р°",
	}

	item, err := svc.CreateInternal(context.Background(), dto, author.ID)
	if err != nil {
		t.Fatalf("РЅРµ СѓРґР°Р»РѕСЃСЊ СЃРѕР·РґР°С‚СЊ С‚РёРєРµС‚: %v", err)
	}
	if item.BitrixServicePointID == nil || *item.BitrixServicePointID != 501 {
		t.Fatalf("РѕР¶РёРґР°Р»Рё Р°РІС‚РѕСЃРѕРїРѕСЃС‚Р°РІР»РµРЅРЅСѓСЋ С‚РѕС‡РєСѓ 501, РїРѕР»СѓС‡РёР»Рё %v", item.BitrixServicePointID)
	}
}
