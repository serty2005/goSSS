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

func TestTicketRepoArchiveStale_UsesClosedAtHistoryInsteadOfCreatedOrUpdatedAt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_repo_archive_stale?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}

	if err := db.AutoMigrate(
		&user.User{},
		&user.Role{},
		&user.Integration{},
		&company.Company{},
		&tickets.Ticket{},
		&tickets.TicketHistory{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	ticketRepo := NewTicketRepo(db)
	now := time.Now()

	shouldArchive := &tickets.Ticket{
		Subject:        "Закрыт давно, обновлён недавно",
		Status:         tickets.StatusClosed,
		SyncWithBitrix: true,
	}
	if err := ticketRepo.Create(ctx, shouldArchive); err != nil {
		t.Fatalf("не удалось создать тикет shouldArchive: %v", err)
	}
	if err := db.Model(&tickets.Ticket{}).
		Where("id = ?", shouldArchive.ID).
		Update("updated_at", now).Error; err != nil {
		t.Fatalf("не удалось обновить updated_at для shouldArchive: %v", err)
	}
	if err := ticketRepo.AddHistory(ctx, &tickets.TicketHistory{
		TicketID:  shouldArchive.ID,
		Action:    tickets.HistoryActionFieldChanged,
		Field:     tickets.HistoryFieldStatus,
		Source:    tickets.HistorySourceSystem,
		OldValue:  tickets.StatusResolved,
		NewValue:  tickets.StatusClosed,
		CreatedAt: now.Add(-15 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("не удалось записать историю закрытия для shouldArchive: %v", err)
	}

	shouldStayActive := &tickets.Ticket{
		Subject:        "Закрыт недавно, создан давно",
		Status:         tickets.StatusClosed,
		SyncWithBitrix: true,
	}
	if err := ticketRepo.Create(ctx, shouldStayActive); err != nil {
		t.Fatalf("не удалось создать тикет shouldStayActive: %v", err)
	}
	if err := db.Model(&tickets.Ticket{}).
		Where("id = ?", shouldStayActive.ID).
		Updates(map[string]any{
			"created_at": now.Add(-30 * 24 * time.Hour),
			"updated_at": now.Add(-30 * 24 * time.Hour),
		}).Error; err != nil {
		t.Fatalf("не удалось обновить даты для shouldStayActive: %v", err)
	}
	if err := ticketRepo.AddHistory(ctx, &tickets.TicketHistory{
		TicketID:  shouldStayActive.ID,
		Action:    tickets.HistoryActionFieldChanged,
		Field:     tickets.HistoryFieldStatus,
		Source:    tickets.HistorySourceSystem,
		OldValue:  tickets.StatusResolved,
		NewValue:  tickets.StatusClosed,
		CreatedAt: now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("не удалось записать историю закрытия для shouldStayActive: %v", err)
	}

	activeOldTicket := &tickets.Ticket{
		Subject:        "Старый активный тикет",
		Status:         tickets.StatusInProgress,
		SyncWithBitrix: true,
	}
	if err := ticketRepo.Create(ctx, activeOldTicket); err != nil {
		t.Fatalf("не удалось создать activeOldTicket: %v", err)
	}
	if err := db.Model(&tickets.Ticket{}).
		Where("id = ?", activeOldTicket.ID).
		Updates(map[string]any{
			"created_at": now.Add(-40 * 24 * time.Hour),
			"updated_at": now.Add(-40 * 24 * time.Hour),
		}).Error; err != nil {
		t.Fatalf("не удалось обновить даты для activeOldTicket: %v", err)
	}

	archivedCount, err := ticketRepo.ArchiveStale(ctx, 14*24*time.Hour)
	if err != nil {
		t.Fatalf("ArchiveStale вернул ошибку: %v", err)
	}
	if archivedCount != 1 {
		t.Fatalf("ожидали архивацию 1 тикета, получили %d", archivedCount)
	}

	archivedTicket, err := ticketRepo.GetByID(ctx, shouldArchive.ID)
	if err != nil {
		t.Fatalf("не удалось получить shouldArchive: %v", err)
	}
	if !archivedTicket.IsArchived {
		t.Fatalf("ожидали архивацию тикета shouldArchive")
	}

	recentlyClosedTicket, err := ticketRepo.GetByID(ctx, shouldStayActive.ID)
	if err != nil {
		t.Fatalf("не удалось получить shouldStayActive: %v", err)
	}
	if recentlyClosedTicket.IsArchived {
		t.Fatalf("тикет shouldStayActive не должен архивироваться по возрасту создания или updated_at")
	}

	stillActiveTicket, err := ticketRepo.GetByID(ctx, activeOldTicket.ID)
	if err != nil {
		t.Fatalf("не удалось получить activeOldTicket: %v", err)
	}
	if stillActiveTicket.IsArchived {
		t.Fatalf("активный тикет не должен попадать в архив")
	}
}
