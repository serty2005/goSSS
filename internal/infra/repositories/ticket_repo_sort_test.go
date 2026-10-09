package repositories

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/domain/user"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// openTicketSortTestDB открывает реальный Postgres, если задан TEST_POSTGRES_DSN, иначе sqlite в памяти.
func openTicketSortTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	var (
		db  *gorm.DB
		err error
	)
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		db, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
		if err == nil {
			db.Exec("DROP SCHEMA IF EXISTS ticket_sort_test CASCADE")
			db.Exec("CREATE SCHEMA ticket_sort_test")
			db.Exec("SET search_path TO ticket_sort_test")
			t.Cleanup(func() { db.Exec("DROP SCHEMA IF EXISTS ticket_sort_test CASCADE") })
			sqlDB, _ := db.DB()
			sqlDB.SetMaxOpenConns(1)
		}
	} else {
		db, err = gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	}
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	if err := db.AutoMigrate(&user.User{}, &user.Role{}, &user.Integration{}, &company.Company{}, &tickets.Ticket{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	return db
}

func createSortTicket(t *testing.T, repo tickets.TicketRepository, subject, status string, assigneeID *uint, updatedAgo time.Duration) *tickets.Ticket {
	t.Helper()
	ticket := &tickets.Ticket{Subject: subject, Status: status, SyncWithBitrix: true, AssigneeID: assigneeID}
	if err := repo.Create(context.Background(), ticket); err != nil {
		t.Fatalf("не удалось создать тикет %q: %v", subject, err)
	}
	return ticket
}

func setTicketUpdatedAt(t *testing.T, db *gorm.DB, id string, value time.Time) {
	t.Helper()
	if err := db.Model(&tickets.Ticket{}).Where("id = ?", id).UpdateColumn("updated_at", value).Error; err != nil {
		t.Fatalf("не удалось обновить updated_at: %v", err)
	}
}

func subjectsOf(items []tickets.Ticket) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		result = append(result, item.Subject)
	}
	return result
}

func TestTicketRepoFind_SortByStatusGroupsByWorkflowOrder(t *testing.T) {
	db := openTicketSortTestDB(t)
	repo := NewTicketRepo(db)
	now := time.Now()

	closed := createSortTicket(t, repo, "закрыта", tickets.StatusClosed, nil, 0)
	legacyNew := createSortTicket(t, repo, "новая-legacy", "registered", nil, 0)
	progress := createSortTicket(t, repo, "в работе", tickets.StatusInProgress, nil, 0)
	newer := createSortTicket(t, repo, "новая-свежая", tickets.StatusNew, nil, 0)
	deferred := createSortTicket(t, repo, "отложена", tickets.StatusDeferred, nil, 0)
	unknown := createSortTicket(t, repo, "неизвестный", "waitClientAnswer", nil, 0)
	pending := createSortTicket(t, repo, "ожидание", tickets.StatusPending, nil, 0)
	setTicketUpdatedAt(t, db, closed.ID, now)
	setTicketUpdatedAt(t, db, legacyNew.ID, now.Add(-2*time.Hour))
	setTicketUpdatedAt(t, db, progress.ID, now)
	setTicketUpdatedAt(t, db, newer.ID, now.Add(-time.Hour))
	setTicketUpdatedAt(t, db, deferred.ID, now)
	setTicketUpdatedAt(t, db, unknown.ID, now)
	setTicketUpdatedAt(t, db, pending.ID, now)

	asc, err := repo.Find(context.Background(), tickets.TicketFilter{SortBy: "status:asc"})
	if err != nil {
		t.Fatalf("сортировка по статусу: %v", err)
	}
	wantAsc := []string{"новая-свежая", "новая-legacy", "в работе", "ожидание", "отложена", "закрыта", "неизвестный"}
	if got := subjectsOf(asc); fmt.Sprint(got) != fmt.Sprint(wantAsc) {
		t.Fatalf("порядок asc: получено %v, ожидалось %v", got, wantAsc)
	}

	desc, err := repo.Find(context.Background(), tickets.TicketFilter{SortBy: "status:desc"})
	if err != nil {
		t.Fatalf("обратная сортировка по статусу: %v", err)
	}
	wantDesc := []string{"неизвестный", "закрыта", "отложена", "ожидание", "в работе", "новая-свежая", "новая-legacy"}
	if got := subjectsOf(desc); fmt.Sprint(got) != fmt.Sprint(wantDesc) {
		t.Fatalf("порядок desc: получено %v, ожидалось %v", got, wantDesc)
	}
}

func TestTicketRepoFind_SortByStatusKeepsUnknownStatusesTogether(t *testing.T) {
	db := openTicketSortTestDB(t)
	repo := NewTicketRepo(db)
	now := time.Now()
	for i, status := range []string{"waitClientAnswer", "done", "waitClientAnswer", "done"} {
		ticket := createSortTicket(t, repo, fmt.Sprintf("%s-%d", status, i), status, nil, 0)
		setTicketUpdatedAt(t, db, ticket.ID, now.Add(-time.Duration(i)*time.Minute))
	}

	items, err := repo.Find(context.Background(), tickets.TicketFilter{SortBy: "status:asc"})
	if err != nil {
		t.Fatalf("сортировка по статусу: %v", err)
	}
	var runs []string
	for _, item := range items {
		if len(runs) == 0 || runs[len(runs)-1] != item.Status {
			runs = append(runs, item.Status)
		}
	}
	if len(runs) != 2 {
		t.Fatalf("неизвестные статусы должны идти сериями без перемешивания, получено %v", runs)
	}
}

func TestTicketRepoFind_SortByStatusPaginationHasNoOverlap(t *testing.T) {
	db := openTicketSortTestDB(t)
	repo := NewTicketRepo(db)
	same := time.Now()
	statuses := []string{tickets.StatusNew, tickets.StatusInProgress, tickets.StatusNew, tickets.StatusClosed, tickets.StatusInProgress, tickets.StatusNew}
	for i, status := range statuses {
		ticket := createSortTicket(t, repo, fmt.Sprintf("t%d", i), status, nil, 0)
		setTicketUpdatedAt(t, db, ticket.ID, same)
	}

	seen := map[string]bool{}
	for offset := 0; offset < len(statuses); offset += 2 {
		page, err := repo.Find(context.Background(), tickets.TicketFilter{SortBy: "status:asc", Limit: 2, Offset: offset})
		if err != nil {
			t.Fatalf("страница offset=%d: %v", offset, err)
		}
		for _, item := range page {
			if seen[item.ID] {
				t.Fatalf("тикет %s повторился на соседних страницах", item.Subject)
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != len(statuses) {
		t.Fatalf("страницы вернули %d уникальных тикетов из %d", len(seen), len(statuses))
	}
}

func TestTicketRepoFind_SortByAssigneeName(t *testing.T) {
	db := openTicketSortTestDB(t)
	userRepo := NewUserRepo(db)
	repo := NewTicketRepo(db)
	ctx := context.Background()

	zed := &user.User{Username: "zed", PasswordHash: "h", FullName: "Яковлев"}
	abe := &user.User{Username: "abe", PasswordHash: "h", FullName: "Абрамов"}
	for _, u := range []*user.User{zed, abe} {
		if err := userRepo.Create(ctx, u); err != nil {
			t.Fatalf("не удалось создать пользователя: %v", err)
		}
	}
	createSortTicket(t, repo, "без исполнителя", tickets.StatusNew, nil, 0)
	createSortTicket(t, repo, "яковлев", tickets.StatusNew, &zed.ID, 0)
	createSortTicket(t, repo, "абрамов", tickets.StatusNew, &abe.ID, 0)

	items, err := repo.Find(ctx, tickets.TicketFilter{SortBy: "assignee:asc"})
	if err != nil {
		t.Fatalf("сортировка по исполнителю: %v", err)
	}
	want := []string{"без исполнителя", "абрамов", "яковлев"}
	if got := subjectsOf(items); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("порядок: получено %v, ожидалось %v", got, want)
	}
}

func TestTicketRepoCountByStatus(t *testing.T) {
	db := openTicketSortTestDB(t)
	repo := NewTicketRepo(db)
	createSortTicket(t, repo, "a", tickets.StatusNew, nil, 0)
	createSortTicket(t, repo, "b", tickets.StatusNew, nil, 0)
	createSortTicket(t, repo, "c", "registered", nil, 0)
	createSortTicket(t, repo, "d", tickets.StatusClosed, nil, 0)

	counts, err := repo.CountByStatus(context.Background(), tickets.TicketFilter{})
	if err != nil {
		t.Fatalf("подсчет по статусам: %v", err)
	}
	if counts[tickets.StatusNew] != 2 || counts["registered"] != 1 || counts[tickets.StatusClosed] != 1 || len(counts) != 3 {
		t.Fatalf("неожиданные счетчики: %v", counts)
	}

	filtered, err := repo.CountByStatus(context.Background(), tickets.TicketFilter{Statuses: []string{tickets.StatusClosed}})
	if err != nil {
		t.Fatalf("подсчет с фильтром: %v", err)
	}
	if len(filtered) != 1 || filtered[tickets.StatusClosed] != 1 {
		t.Fatalf("неожиданные счетчики с фильтром: %v", filtered)
	}
}
