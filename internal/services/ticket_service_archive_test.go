package services

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestChangeStatus_ReturnFromArchiveClearsArchiveFlagWithoutStatusChange(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_service_return_from_archive?mode=memory&cache=shared"), &gorm.Config{})
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
		&tickets.TicketComment{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	userRepo := repositories.NewUserRepo(db)
	companyRepo := repositories.NewCompanyRepo(db)
	contractRepo := repositories.NewContractRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)

	actor := &user.User{
		Username:     "archive_return_actor",
		PasswordHash: "hash",
		FullName:     "Оператор",
	}
	if err := userRepo.Create(ctx, actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	title := "Компания для возврата из архива"
	activeContract := false
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	archivedAt := time.Now().Add(-48 * time.Hour)
	ticket := &tickets.Ticket{
		Subject:         "Архивный тикет",
		Status:          tickets.StatusInProgress,
		CompanyID:       comp.ID,
		ReporterID:      &actor.ID,
		SyncWithBitrix:  false,
		IsArchived:      true,
		ArchivedAt:      &archivedAt,
		BitrixDealTitle: "",
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	svc := NewTicketService(
		nil,
		ticketRepo,
		userRepo,
		companyRepo,
		contractRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	updated, err := svc.ChangeStatus(ctx, ticket.ID, tickets.StatusInProgress, "", "", actor.ID)
	if err != nil {
		t.Fatalf("ChangeStatus вернул ошибку: %v", err)
	}
	if updated.IsArchived {
		t.Fatalf("ожидали снятие флага архива у возвращённого тикета")
	}
	if updated.ArchivedAt != nil {
		t.Fatalf("ожидали очистку archived_at у возвращённого тикета, получили %v", updated.ArchivedAt)
	}

	stored, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать тикет: %v", err)
	}
	if stored.IsArchived {
		t.Fatalf("после сохранения тикет остался архивным")
	}
	if stored.ArchivedAt != nil {
		t.Fatalf("после сохранения archived_at не очистился: %v", stored.ArchivedAt)
	}
}
