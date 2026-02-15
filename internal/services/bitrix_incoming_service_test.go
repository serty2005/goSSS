package services

import (
	"context"
	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"net/url"
	"testing"
	"time"
)

type bitrixRepoMock struct {
	bitrix.Repository
	byHash       map[string]string
	insertCalls  int
	createdCount int
	queuedIDs    []string
	commentLinks map[int64]*bitrix.CommentLink
}

func (m *bitrixRepoMock) InsertIfNotExistsByHash(_ context.Context, event *bitrix.IncomingEvent) (bool, error) {
	m.insertCalls++
	if m.byHash == nil {
		m.byHash = make(map[string]string)
	}
	if _, exists := m.byHash[event.PayloadHash]; exists {
		return false, nil
	}
	m.byHash[event.PayloadHash] = event.ID
	m.createdCount++
	return true, nil
}

func (m *bitrixRepoMock) MarkQueued(_ context.Context, id string) error {
	m.queuedIDs = append(m.queuedIDs, id)
	return nil
}

func (m *bitrixRepoMock) GetCommentLinkByB24ID(_ context.Context, b24CommentID int64) (*bitrix.CommentLink, error) {
	if m.commentLinks == nil {
		return nil, nil
	}
	return m.commentLinks[b24CommentID], nil
}

type ticketRepoMock struct {
	tickets.TicketRepository
	deletedCommentIDs []string
}

func (m *ticketRepoMock) MarkCommentDeletedInBitrix(_ context.Context, commentID string, _ time.Time) error {
	m.deletedCommentIDs = append(m.deletedCommentIDs, commentID)
	return nil
}

func TestBitrixIncomingService_HandleWebhook_IdempotentByPayloadHash(t *testing.T) {
	repo := &bitrixRepoMock{}
	svc := &bitrixIncomingService{
		cfg: &config.Config{
			EnableBitrixGateway:   true,
			BitrixWebhookEnabled:  true,
			BitrixWebhookAppToken: "tok",
		},
		log:  logger.New("", "test", "error", true),
		repo: repo,
	}

	raw := []byte("event=ONCRMDEALADD&data%5BFIELDS%5D%5BID%5D=123&auth%5Bapplication_token%5D=tok")
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		t.Fatalf("ошибка подготовки payload: %v", err)
	}

	if err := svc.HandleWebhook(context.Background(), raw, form); err != nil {
		t.Fatalf("первая обработка завершилась ошибкой: %v", err)
	}
	if err := svc.HandleWebhook(context.Background(), raw, form); err != nil {
		t.Fatalf("повторная обработка завершилась ошибкой: %v", err)
	}

	if repo.insertCalls != 2 {
		t.Fatalf("ожидалось 2 попытки вставки, получено %d", repo.insertCalls)
	}
	if repo.createdCount != 1 {
		t.Fatalf("ожидалась 1 созданная запись события, получено %d", repo.createdCount)
	}
}

func TestBitrixIncomingService_CommentDeleteAlwaysSoftDeletes(t *testing.T) {
	repo := &bitrixRepoMock{
		commentLinks: map[int64]*bitrix.CommentLink{
			77: {EtalonCommentID: "c-77"},
		},
	}
	ticketRepo := &ticketRepoMock{}
	entityID := "77"
	svc := &bitrixIncomingService{
		cfg:        &config.Config{},
		log:        logger.New("", "test", "error", true),
		repo:       repo,
		ticketRepo: ticketRepo,
	}

	status, reason, err := svc.handleIncomingEvent(context.Background(), &bitrix.IncomingEvent{
		EventName: "ONCRMTIMELINECOMMENTDELETE",
		EntityID:  &entityID,
	})
	if err != nil {
		t.Fatalf("обработка comment delete завершилась ошибкой: %v", err)
	}
	if status != bitrix.IncomingEventStatusDone {
		t.Fatalf("ожидался статус %q, получен %q (reason=%q)", bitrix.IncomingEventStatusDone, status, reason)
	}
	if len(ticketRepo.deletedCommentIDs) != 1 || ticketRepo.deletedCommentIDs[0] != "c-77" {
		t.Fatalf("ожидался soft delete комментария c-77, получено: %#v", ticketRepo.deletedCommentIDs)
	}
}
