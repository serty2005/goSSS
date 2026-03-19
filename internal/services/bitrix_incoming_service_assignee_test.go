package services

import (
	"context"
	"testing"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBitrixIncomingService_ApplyDealSnapshot_AssigneeResolvedByExternalID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&tickets.Ticket{},
		&bitrix.UserMap{},
		&bitrix.UserCache{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	userRepo := repositories.NewUserRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)

	externalType := user.ExternalTypeBitrix24
	externalID := "495"
	assignee := &user.User{
		Username:     "bitrix_assignee",
		PasswordHash: "hash",
		FirstName:    "Юрий",
		LastName:     "Ерёменко",
		FullName:     "Юрий Ерёменко",
		ExternalType: &externalType,
		ExternalID:   &externalID,
	}
	if err := userRepo.Create(ctx, assignee); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}
	oldAssignee := &user.User{
		Username:     "old_assignee",
		PasswordHash: "hash",
		FirstName:    "Старый",
		LastName:     "Исполнитель",
		FullName:     "Старый Исполнитель",
	}
	if err := userRepo.Create(ctx, oldAssignee); err != nil {
		t.Fatalf("не удалось создать старого исполнителя: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:        "Тестовый тикет",
		Status:         tickets.StatusInProgress,
		SyncWithBitrix: true,
		AssigneeID:     &oldAssignee.ID,
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}
	loadedTicket, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось загрузить тикет перед проверкой: %v", err)
	}
	if loadedTicket == nil || loadedTicket.Assignee == nil || loadedTicket.Assignee.ID != oldAssignee.ID {
		t.Fatalf("ожидался тикет с предзагруженной старой ассоциацией исполнителя")
	}

	svc := &bitrixIncomingService{
		log:        logger.New("", "test", "error", true),
		ticketRepo: ticketRepo,
		userRepo:   userRepo,
		repo:       bitrixRepo,
		history:    nil,
	}

	assignedBy := int64(495)
	deal := &b24.Deal{
		ID:         5081,
		StageID:    "C17:PREPARATION",
		Title:      "Тестовая сделка",
		AssignedBy: &assignedBy,
		Raw: map[string]interface{}{
			"UF_CRM_1766060620": "Описание",
		},
	}

	if err := svc.applyDealSnapshotToTicket(ctx, loadedTicket, deal); err != nil {
		t.Fatalf("applyDealSnapshotToTicket завершился ошибкой: %v", err)
	}

	stored, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать тикет: %v", err)
	}
	if stored == nil || stored.AssigneeID == nil {
		t.Fatalf("ожидался назначенный исполнитель, получено: %+v", stored)
	}
	if *stored.AssigneeID != assignee.ID {
		t.Fatalf("ожидался assignee_id=%d, получено assignee_id=%d", assignee.ID, *stored.AssigneeID)
	}

	mapping, err := bitrixRepo.GetUserMapByB24ID(ctx, assignedBy)
	if err != nil {
		t.Fatalf("не удалось получить user_map: %v", err)
	}
	if mapping == nil || mapping.EtalonUserID != assignee.ID {
		t.Fatalf("ожидался user_map для b24_user_id=%d и etalon_user_id=%d", assignedBy, assignee.ID)
	}
}

func TestBitrixIncomingService_ApplyDealSnapshot_StoresBitrixDealTitle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&tickets.Ticket{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	ticketRepo := repositories.NewTicketRepo(db)

	ticket := &tickets.Ticket{
		Subject:         "Исходный тикет",
		Description:     "Описание",
		Status:          tickets.StatusNew,
		SyncWithBitrix:  true,
		BitrixDealTitle: "",
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	loadedTicket, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось загрузить тикет перед проверкой: %v", err)
	}

	svc := &bitrixIncomingService{
		log:        logger.New("", "test", "error", true),
		ticketRepo: ticketRepo,
		history:    nil,
	}

	deal := &b24.Deal{
		ID:      6001,
		StageID: "C17:NEW",
		Title:   "Автоматический заголовок из Bitrix24",
		Raw: map[string]interface{}{
			"UF_CRM_1766060620": "Описание из Bitrix24",
		},
	}

	if err := svc.applyDealSnapshotToTicket(ctx, loadedTicket, deal); err != nil {
		t.Fatalf("applyDealSnapshotToTicket завершился ошибкой: %v", err)
	}

	stored, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать тикет: %v", err)
	}
	if stored == nil {
		t.Fatalf("ожидался сохранённый тикет")
	}
	if stored.BitrixDealTitle != "Автоматический заголовок из Bitrix24" {
		t.Fatalf("ожидали сохранённый заголовок Bitrix24, получили %q", stored.BitrixDealTitle)
	}
}
