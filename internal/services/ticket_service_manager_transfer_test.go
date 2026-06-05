package services

import (
	"context"
	"testing"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/contract"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestChangeStatus_ToManagerWithTelegramStoresTargetAndComment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_service_manager_transfer?mode=memory&cache=shared"), &gorm.Config{})
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
		Username:     "manager_transfer_actor",
		PasswordHash: "hash",
		FullName:     "Оператор",
	}
	if err := userRepo.Create(ctx, actor); err != nil {
		t.Fatalf("не удалось создать пользователя: %v", err)
	}

	title := "Компания для передачи менеджеру"
	activeContract := false
	comp := &company.Company{
		Title:          &title,
		ActiveContract: &activeContract,
	}
	if err := companyRepo.Create(ctx, comp); err != nil {
		t.Fatalf("не удалось создать компанию: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:        "Нужна консультация",
		Status:         tickets.StatusInProgress,
		CompanyID:      comp.ID,
		ReporterID:     &actor.ID,
		SyncWithBitrix: true,
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

	updated, err := svc.ChangeStatus(ctx, ticket.ID, tickets.StatusToManager, "", TicketStatusChangeOptions{
		ManagerTransferTarget: tickets.ManagerTransferTargetPaymentReview,
		ClientContactType:     tickets.ManagerTransferContactTelegram,
		ClientContactValue:    "t.me/client_login",
	}, actor.ID)
	if err != nil {
		t.Fatalf("ChangeStatus вернул ошибку: %v", err)
	}
	if updated.Status != tickets.StatusToManager {
		t.Fatalf("ожидали статус %q, получили %q", tickets.StatusToManager, updated.Status)
	}
	if updated.ManagerTransferTarget != tickets.ManagerTransferTargetPaymentReview {
		t.Fatalf("ожидали направление %q, получили %q", tickets.ManagerTransferTargetPaymentReview, updated.ManagerTransferTarget)
	}

	comments, err := ticketRepo.GetComments(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось получить комментарии: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("ожидали один комментарий с Telegram-контактом, получили %d", len(comments))
	}
	if comments[0].Text != "Контакт в телеграмм: @client_login" {
		t.Fatalf("неверный текст комментария: %q", comments[0].Text)
	}
	if comments[0].IsPrivate {
		t.Fatalf("Telegram-контакт должен быть публичным комментарием для синхронизации")
	}
}
