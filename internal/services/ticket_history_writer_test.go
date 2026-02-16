package services

import (
	"context"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/logger"
	"testing"

	"github.com/stretchr/testify/require"
)

type ticketHistoryRepoStub struct {
	items []*tickets.TicketHistory
}

func (s *ticketHistoryRepoStub) AddHistory(_ context.Context, history *tickets.TicketHistory) error {
	s.items = append(s.items, history)
	return nil
}

type loggerStub struct{}

func (loggerStub) Debug(_ string, _ ...any) {}
func (loggerStub) Info(_ string, _ ...any)  {}
func (loggerStub) Warn(_ string, _ ...any)  {}
func (loggerStub) Error(_ string, _ ...any) {}
func (loggerStub) Fatal(_ string, _ ...any) {}
func (loggerStub) With(_ ...any) logger.LoggerInterface {
	return loggerStub{}
}

func TestTicketHistoryWriter_Write(t *testing.T) {
	repo := &ticketHistoryRepoStub{}
	writer := NewTicketHistoryWriter(repo, loggerStub{})
	actorID := uint(15)

	writer.Write(context.Background(), TicketHistoryWriteRequest{
		TicketID: "T-1",
		UserID:   &actorID,
		Action:   tickets.HistoryActionCommentUpdated,
		Field:    tickets.HistoryFieldComment,
		Source:   tickets.HistorySourceBitrix,
		OldValue: "было",
		NewValue: "стало",
		Meta: map[string]interface{}{
			"bitrix_comment_id": int64(77),
		},
	})

	require.Len(t, repo.items, 1)
	item := repo.items[0]
	require.Equal(t, "T-1", item.TicketID)
	require.Equal(t, tickets.HistoryActionCommentUpdated, item.Action)
	require.Equal(t, tickets.HistorySourceBitrix, item.Source)
	require.Equal(t, "было", item.OldValue)
	require.Equal(t, "стало", item.NewValue)
	require.NotNil(t, item.Meta)
	require.EqualValues(t, 77, item.Meta["bitrix_comment_id"])
}
