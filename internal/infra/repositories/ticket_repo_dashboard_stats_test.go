package repositories

import (
	"context"
	"testing"
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/server"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestTicketRepoGetDashboardStats_MapsResolvedPeriods(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket_repo_dashboard_stats?mode=memory&cache=shared"), &gorm.Config{})
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
		&telephony.Call{},
		&server.Server{},
	); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	userRepo := NewUserRepo(db)
	ticketRepo := NewTicketRepo(db)

	assignee := &user.User{
		Username:     "dashboard_assignee",
		PasswordHash: "hash",
		FullName:     "Сотрудник дашборда",
	}
	if err := userRepo.Create(ctx, assignee); err != nil {
		t.Fatalf("не удалось создать исполнителя: %v", err)
	}

	ticketRecent := &tickets.Ticket{
		Subject:        "Недавно решённый тикет",
		Status:         tickets.StatusResolved,
		SyncWithBitrix: true,
		AssigneeID:     &assignee.ID,
	}
	if err := ticketRepo.Create(ctx, ticketRecent); err != nil {
		t.Fatalf("не удалось создать недавний тикет: %v", err)
	}

	ticketOld := &tickets.Ticket{
		Subject:        "Решённый тикет за пределами 7 дней",
		Status:         tickets.StatusClosed,
		SyncWithBitrix: true,
		AssigneeID:     &assignee.ID,
	}
	if err := ticketRepo.Create(ctx, ticketOld); err != nil {
		t.Fatalf("не удалось создать старый тикет: %v", err)
	}

	recentHistory := &tickets.TicketHistory{
		TicketID: ticketRecent.ID,
		UserID:   &assignee.ID,
		Action:   tickets.HistoryActionFieldChanged,
		Field:    tickets.HistoryFieldStatus,
		Source:   tickets.HistorySourceUI,
		OldValue: tickets.StatusInProgress,
		NewValue: tickets.StatusResolved,
	}
	if err := ticketRepo.AddHistory(ctx, recentHistory); err != nil {
		t.Fatalf("не удалось добавить недавнюю историю: %v", err)
	}

	oldHistory := &tickets.TicketHistory{
		TicketID: ticketOld.ID,
		UserID:   &assignee.ID,
		Action:   tickets.HistoryActionFieldChanged,
		Field:    tickets.HistoryFieldStatus,
		Source:   tickets.HistorySourceUI,
		OldValue: tickets.StatusInProgress,
		NewValue: tickets.StatusResolved,
	}
	if err := ticketRepo.AddHistory(ctx, oldHistory); err != nil {
		t.Fatalf("не удалось добавить старую историю: %v", err)
	}

	now := time.Now()
	recentResolvedAt := now.Add(-24 * time.Hour)
	oldResolvedAt := now.Add(-10 * 24 * time.Hour)
	if err := db.Model(&tickets.TicketHistory{}).
		Where("id = ?", recentHistory.ID).
		Update("created_at", recentResolvedAt).Error; err != nil {
		t.Fatalf("не удалось обновить дату недавней истории: %v", err)
	}
	if err := db.Model(&tickets.TicketHistory{}).
		Where("id = ?", oldHistory.ID).
		Update("created_at", oldResolvedAt).Error; err != nil {
		t.Fatalf("не удалось обновить дату старой истории: %v", err)
	}

	stats, err := ticketRepo.GetDashboardStats(ctx)
	if err != nil {
		t.Fatalf("не удалось получить статистику дашборда: %v", err)
	}
	if stats == nil {
		t.Fatal("ожидалась статистика дашборда")
	}
	if len(stats.ResolvedByAssignee) != 1 {
		t.Fatalf("ожидалась одна строка статистики по исполнителю, получено %d", len(stats.ResolvedByAssignee))
	}

	row := stats.ResolvedByAssignee[0]
	if row.UserID != assignee.ID {
		t.Fatalf("ожидался исполнитель %d, получен %d", assignee.ID, row.UserID)
	}
	if row.TodayCount != 0 {
		t.Fatalf("ожидали today_count=0, получили %d", row.TodayCount)
	}
	if row.Days7Count != 1 {
		t.Fatalf("ожидали days_7_count=1, получили %d", row.Days7Count)
	}
	if row.Days30Count != 2 {
		t.Fatalf("ожидали days_30_count=2, получили %d", row.Days30Count)
	}
}
