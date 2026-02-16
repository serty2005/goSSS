package services

import (
	"context"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/logger"
	"strings"
	"time"

	"gorm.io/datatypes"
)

type TicketHistoryWriteRequest struct {
	TicketID string
	UserID   *uint
	Action   string
	Field    string
	Source   string
	OldValue string
	NewValue string
	Meta     map[string]interface{}
}

type TicketHistoryWriter interface {
	Write(ctx context.Context, req TicketHistoryWriteRequest)
}

type ticketHistoryRepo interface {
	AddHistory(ctx context.Context, history *tickets.TicketHistory) error
}

type ticketHistoryWriterImpl struct {
	ticketRepo ticketHistoryRepo
	logger     logger.LoggerInterface
}

func NewTicketHistoryWriter(ticketRepo ticketHistoryRepo, log logger.LoggerInterface) TicketHistoryWriter {
	return &ticketHistoryWriterImpl{
		ticketRepo: ticketRepo,
		logger:     log,
	}
}

func (w *ticketHistoryWriterImpl) Write(ctx context.Context, req TicketHistoryWriteRequest) {
	ticketID := strings.TrimSpace(req.TicketID)
	if ticketID == "" {
		return
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = tickets.HistoryActionFieldChanged
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = tickets.HistorySourceSystem
	}

	var meta datatypes.JSONMap
	if len(req.Meta) > 0 {
		meta = datatypes.JSONMap(req.Meta)
	}

	h := &tickets.TicketHistory{
		TicketID:  ticketID,
		UserID:    req.UserID,
		Action:    action,
		Field:     strings.TrimSpace(req.Field),
		Source:    source,
		OldValue:  req.OldValue,
		NewValue:  req.NewValue,
		Meta:      meta,
		CreatedAt: time.Now(),
	}
	if err := w.ticketRepo.AddHistory(ctx, h); err != nil {
		w.logger.Error("Не удалось записать историю тикета", "ticket_id", ticketID, "action", action, "source", source, "error", err)
	}
}
