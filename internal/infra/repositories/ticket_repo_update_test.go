package repositories

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTicketRepoUpdate_DoesNotOverrideAssigneeByLoadedAssociation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}

	if err := db.AutoMigrate(&user.User{}, &user.Role{}, &user.Integration{}, &company.Company{}, &tickets.Ticket{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	userRepo := NewUserRepo(db)
	ticketRepo := NewTicketRepo(db)

	oldAssignee := &user.User{Username: "old_assignee", PasswordHash: "hash", FullName: "Старый исполнитель"}
	newAssignee := &user.User{Username: "new_assignee", PasswordHash: "hash", FullName: "Новый исполнитель"}
	if err := userRepo.Create(ctx, oldAssignee); err != nil {
		t.Fatalf("не удалось создать старого исполнителя: %v", err)
	}
	if err := userRepo.Create(ctx, newAssignee); err != nil {
		t.Fatalf("не удалось создать нового исполнителя: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:        "Тест",
		Status:         tickets.StatusInProgress,
		SyncWithBitrix: true,
		AssigneeID:     &oldAssignee.ID,
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	loaded, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить тикет: %v", err)
	}
	if loaded == nil || loaded.Assignee == nil || loaded.Assignee.ID != oldAssignee.ID {
		t.Fatalf("ожидалась загруженная старая ассоциация исполнителя")
	}

	loaded.AssigneeID = &newAssignee.ID
	if err := ticketRepo.Update(ctx, loaded); err != nil {
		t.Fatalf("не удалось обновить тикет: %v", err)
	}

	stored, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать тикет: %v", err)
	}
	if stored == nil || stored.AssigneeID == nil {
		t.Fatalf("после обновления assignee_id отсутствует")
	}
	if *stored.AssigneeID != newAssignee.ID {
		t.Fatalf("ожидался assignee_id=%d, получен assignee_id=%d", newAssignee.ID, *stored.AssigneeID)
	}
}

func TestTicketRepoCreate_PersistsSyncWithBitrixFalse(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}

	if err := db.AutoMigrate(&company.Company{}, &tickets.Ticket{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	ticketRepo := NewTicketRepo(db)

	ticket := &tickets.Ticket{
		Subject:        "Тест sync_with_bitrix=false",
		Status:         tickets.StatusNew,
		SyncWithBitrix: false,
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	stored, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать тикет: %v", err)
	}
	if stored == nil {
		t.Fatalf("тикет не найден после создания")
	}
	if stored.SyncWithBitrix {
		t.Fatalf("ожидался sync_with_bitrix=false, получен true")
	}
}

func TestTicketRepoFind_SanitizesSortBy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_repo_find_sanitizes_sort_by?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}

	if err := db.AutoMigrate(&company.Company{}, &tickets.Ticket{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	ticketRepo := NewTicketRepo(db)

	older := &tickets.Ticket{Subject: "Старый тикет", Status: tickets.StatusNew, SyncWithBitrix: true}
	newer := &tickets.Ticket{Subject: "Новый тикет", Status: tickets.StatusNew, SyncWithBitrix: true}
	if err := ticketRepo.Create(ctx, older); err != nil {
		t.Fatalf("не удалось создать старый тикет: %v", err)
	}
	if err := ticketRepo.Create(ctx, newer); err != nil {
		t.Fatalf("не удалось создать новый тикет: %v", err)
	}

	if err := db.Model(&tickets.Ticket{}).Where("id = ?", older.ID).Update("created_at", older.CreatedAt.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("не удалось обновить created_at старого тикета: %v", err)
	}
	if err := db.Model(&tickets.Ticket{}).Where("id = ?", newer.ID).Update("created_at", newer.CreatedAt).Error; err != nil {
		t.Fatalf("не удалось обновить created_at нового тикета: %v", err)
	}

	items, err := ticketRepo.Find(ctx, tickets.TicketFilter{
		SortBy: "created_at desc; select 1",
	})
	if err != nil {
		t.Fatalf("не ожидали ошибку при санитизированной сортировке: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("ожидали 2 тикета, получили %d", len(items))
	}
	if items[0].ID != newer.ID {
		t.Fatalf("ожидали, что первым вернется более новый тикет, получили %s", items[0].ID)
	}
}

func TestTicketRepoUpsertTicketContactKeepsManualPrimary(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_repo_contacts_manual_primary?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	if err := db.AutoMigrate(&tickets.Ticket{}, &tickets.TicketContact{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	ticketRepo := NewTicketRepo(db)
	ticket := &tickets.Ticket{Subject: "Контакты", Status: tickets.StatusNew, SyncWithBitrix: true}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	manual, err := ticketRepo.UpsertTicketContact(ctx, tickets.TicketContactUpsertInput{
		TicketID:     ticket.ID,
		ContactType:  tickets.ManagerTransferContactTelegram,
		Value:        "@client",
		DisplayValue: "@client",
		IsPrimary:    true,
		Source:       tickets.TicketContactSourceManual,
	})
	if err != nil {
		t.Fatalf("не удалось сохранить ручной контакт: %v", err)
	}
	if manual == nil || !manual.IsPrimary || manual.PrimaryMode != tickets.TicketContactPrimaryModeManual {
		t.Fatalf("ручной контакт должен стать главным: %+v", manual)
	}

	if _, err := ticketRepo.UpsertTicketContact(ctx, tickets.TicketContactUpsertInput{
		TicketID:     ticket.ID,
		ContactType:  tickets.ManagerTransferContactPhone,
		Value:        "79990000000",
		DisplayValue: "79990000000",
		Source:       tickets.TicketContactSourceLinkedCall,
	}); err != nil {
		t.Fatalf("не удалось сохранить авто-контакт: %v", err)
	}

	contacts, err := ticketRepo.ListTicketContacts(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить контакты: %v", err)
	}
	if len(contacts) != 2 {
		t.Fatalf("ожидали два контакта, получили %d", len(contacts))
	}
	if contacts[0].ID != manual.ID || !contacts[0].IsPrimary || contacts[0].PrimaryMode != tickets.TicketContactPrimaryModeManual {
		t.Fatalf("ручной главный контакт не должен перебиваться автоподхватом: %+v", contacts)
	}
}
