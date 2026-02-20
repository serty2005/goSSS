package repositories

import (
	"context"
	"testing"

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

	if err := db.AutoMigrate(&tickets.Ticket{}); err != nil {
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
