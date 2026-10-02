package repositories

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/infra/testdb"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openBitrixCommentSendTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testdb.PostgresDSN() != "" {
		return testdb.OpenPostgres(t, &bitrix.CommentLink{}, &bitrix.CommentSendState{})
	}
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть БД: %v", err)
	}
	if err := db.AutoMigrate(&bitrix.CommentLink{}, &bitrix.CommentSendState{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	return db
}

func newCommentSendState(commentID string) *bitrix.CommentSendState {
	return &bitrix.CommentSendState{
		EtalonCommentID: commentID,
		TicketID:        "ticket-1",
		B24DealID:       100,
		Fingerprint:     "fp-" + commentID,
	}
}

func TestClaimCommentSend_OnlyOneConcurrentCallerWins(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))

	const callers = 12
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), 3)
			if err != nil {
				t.Errorf("ClaimCommentSend завершился ошибкой: %v", err)
				return
			}
			if claimed {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()

	if wins.Load() != 1 {
		t.Fatalf("ожидался ровно один владелец отправки, получено %d", wins.Load())
	}
}

func TestClaimCommentSend_AmbiguousIsNeverReclaimed(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))

	if _, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), 3); err != nil || !claimed {
		t.Fatalf("первое резервирование должно удаться: claimed=%v err=%v", claimed, err)
	}
	if err := repo.MarkCommentSendAmbiguous(ctx, "c-1", "таймаут"); err != nil {
		t.Fatalf("не удалось отметить неоднозначный результат: %v", err)
	}

	current, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), 3)
	if err != nil {
		t.Fatalf("ClaimCommentSend завершился ошибкой: %v", err)
	}
	if claimed {
		t.Fatalf("повторная отправка неподтверждённого комментария должна быть запрещена")
	}
	if current == nil || current.Status != bitrix.CommentSendStatusAmbiguous {
		t.Fatalf("ожидалось состояние ambiguous, получено %+v", current)
	}
}

func TestClaimCommentSend_RejectedRetriesUntilAttemptsLimit(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))
	const maxAttempts = 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		current, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), maxAttempts)
		if err != nil || !claimed {
			t.Fatalf("попытка %d должна быть разрешена: claimed=%v err=%v", attempt, claimed, err)
		}
		if current.Attempts != attempt {
			t.Fatalf("ожидалось попыток %d, получено %d", attempt, current.Attempts)
		}
		if err := repo.MarkCommentSendRejected(ctx, "c-1", "ошибка Bitrix24"); err != nil {
			t.Fatalf("не удалось отметить отказ: %v", err)
		}
	}

	current, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), maxAttempts)
	if err != nil {
		t.Fatalf("ClaimCommentSend завершился ошибкой: %v", err)
	}
	if claimed {
		t.Fatalf("после %d попыток новое резервирование должно быть запрещено", maxAttempts)
	}
	if current.Status != bitrix.CommentSendStatusRejected || current.Attempts != maxAttempts {
		t.Fatalf("ожидалось rejected с %d попытками, получено %+v", maxAttempts, current)
	}
}

func TestMarkCommentSend_AppliesOnlyFromSending(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))

	if _, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), 3); err != nil || !claimed {
		t.Fatalf("резервирование не удалось: claimed=%v err=%v", claimed, err)
	}
	if err := repo.MarkCommentSendAmbiguous(ctx, "c-1", "таймаут"); err != nil {
		t.Fatalf("MarkCommentSendAmbiguous: %v", err)
	}
	// Запоздавший отказ не должен снять запрет на повторную отправку.
	if err := repo.MarkCommentSendRejected(ctx, "c-1", "поздний отказ"); err != nil {
		t.Fatalf("MarkCommentSendRejected: %v", err)
	}
	state, err := repo.GetCommentSendState(ctx, "c-1")
	if err != nil || state == nil {
		t.Fatalf("состояние не найдено: %v", err)
	}
	if state.Status != bitrix.CommentSendStatusAmbiguous {
		t.Fatalf("ambiguous не должен перезаписываться отказом, получено %q", state.Status)
	}
}

func TestConfirmCommentSend_WritesLinkAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))

	if _, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState("c-1"), 3); err != nil || !claimed {
		t.Fatalf("резервирование не удалось: claimed=%v err=%v", claimed, err)
	}
	confirmed, err := repo.ConfirmCommentSend(ctx, "c-1", 777, true)
	if err != nil || !confirmed {
		t.Fatalf("подтверждение должно удаться: confirmed=%v err=%v", confirmed, err)
	}

	link, err := repo.GetCommentLinkByEtalonID(ctx, "c-1")
	if err != nil || link == nil || link.B24CommentID != 777 || link.Direction != "etalon_to_b24" {
		t.Fatalf("связь записана неверно: %+v err=%v", link, err)
	}

	again, err := repo.ConfirmCommentSend(ctx, "c-1", 777, false)
	if err != nil || !again {
		t.Fatalf("повторное подтверждение тем же ID должно быть безопасным: confirmed=%v err=%v", again, err)
	}
	other, err := repo.ConfirmCommentSend(ctx, "c-1", 778, false)
	if err != nil {
		t.Fatalf("подтверждение другим ID не должно падать: %v", err)
	}
	if other {
		t.Fatalf("подтверждение другим ID должно быть отклонено")
	}
	link, _ = repo.GetCommentLinkByEtalonID(ctx, "c-1")
	if link.B24CommentID != 777 {
		t.Fatalf("связь не должна перезаписываться, получено %d", link.B24CommentID)
	}
}

func TestListCommentSendsForReconcile_RespectsGraceAndFinalize(t *testing.T) {
	ctx := context.Background()
	db := openBitrixCommentSendTestDB(t)
	repo := NewBitrixRepo(db)

	for _, id := range []string{"fresh", "old-ambiguous", "confirmed-pending"} {
		if _, claimed, err := repo.ClaimCommentSend(ctx, newCommentSendState(id), 3); err != nil || !claimed {
			t.Fatalf("резервирование %s не удалось: claimed=%v err=%v", id, claimed, err)
		}
	}
	if err := repo.MarkCommentSendAmbiguous(ctx, "old-ambiguous", "таймаут"); err != nil {
		t.Fatalf("MarkCommentSendAmbiguous: %v", err)
	}
	if _, err := repo.ConfirmCommentSend(ctx, "confirmed-pending", 5, true); err != nil {
		t.Fatalf("ConfirmCommentSend: %v", err)
	}

	past := time.Now().Add(-10 * time.Minute)
	if err := db.Model(&bitrix.CommentSendState{}).Where("etalon_comment_id IN ?", []string{"old-ambiguous", "confirmed-pending"}).
		Updates(map[string]any{"last_attempt_at": past, "updated_at": past}).Error; err != nil {
		t.Fatalf("не удалось состарить записи: %v", err)
	}

	items, err := repo.ListCommentSendsForReconcile(ctx, time.Now().Add(-2*time.Minute), time.Now().Add(-15*time.Second), 10)
	if err != nil {
		t.Fatalf("ListCommentSendsForReconcile: %v", err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.EtalonCommentID] = true
	}
	if got["fresh"] {
		t.Fatalf("свежая отправка не должна попадать в сверку до окончания ожидания")
	}
	if !got["old-ambiguous"] || !got["confirmed-pending"] {
		t.Fatalf("ожидались old-ambiguous и confirmed-pending, получено %v", got)
	}
}

func TestInsertCommentLinkIfAbsent_ReservesOnce(t *testing.T) {
	ctx := context.Background()
	repo := NewBitrixRepo(openBitrixCommentSendTestDB(t))

	link := func() *bitrix.CommentLink {
		return &bitrix.CommentLink{EtalonCommentID: "b24-9", B24CommentID: 9, TicketID: "ticket-1", Direction: "b24_to_etalon"}
	}
	first, err := repo.InsertCommentLinkIfAbsent(ctx, link())
	if err != nil || !first {
		t.Fatalf("первая вставка должна удаться: inserted=%v err=%v", first, err)
	}
	second, err := repo.InsertCommentLinkIfAbsent(ctx, link())
	if err != nil {
		t.Fatalf("повторная вставка не должна падать: %v", err)
	}
	if second {
		t.Fatalf("повторная вставка должна сообщать, что связь уже существует")
	}
}
