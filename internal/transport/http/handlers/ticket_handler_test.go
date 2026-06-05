package handlers

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/common"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/logger"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"mime/multipart"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type ticketHandlerServiceStub struct {
	details *tickets.TicketDetails
}

func (s *ticketHandlerServiceStub) List(context.Context, tickets.TicketFilter) ([]tickets.Ticket, int64, error) {
	return nil, 0, nil
}
func (s *ticketHandlerServiceStub) GetLastComments(context.Context, []string) (map[string]tickets.LastCommentInfo, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) GetCompanyFilters(context.Context, tickets.TicketFilter) ([]tickets.CompanyFilterItem, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) GetDashboardStats(context.Context) (*tickets.DashboardStats, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) GetDetails(context.Context, string) (*tickets.TicketDetails, error) {
	return s.details, nil
}
func (s *ticketHandlerServiceStub) GetConnectionCopyStats(context.Context, string) ([]tickets.ConnectionCopyStat, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) CreateInternal(context.Context, api.TicketCreateInternalDTO, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) CreateFromPyrus(context.Context, services.TicketCreateFromPyrusInput) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) ChangeStatus(context.Context, string, string, string, services.TicketStatusChangeOptions, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) AddComment(context.Context, string, string, bool, bool, uint) (*tickets.TicketComment, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) UpdateComment(context.Context, string, string, string, bool, uint, []string) (*tickets.TicketComment, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) DeleteComment(context.Context, string, string, uint, []string) error {
	return nil
}
func (s *ticketHandlerServiceStub) RecordConnectionCopy(context.Context, string, string, string, string, string, string, uint) error {
	return nil
}
func (s *ticketHandlerServiceStub) UpdateDescription(context.Context, string, string, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) RefreshCommentsFromServiceDesk(context.Context, string) (int, error) {
	return 0, nil
}
func (s *ticketHandlerServiceStub) UploadAttachments(context.Context, string, []*multipart.FileHeader) ([]tickets.Attachment, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) Assign(context.Context, string, *uint, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) ChangeCompany(context.Context, string, string, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) UpdateBitrixFields(context.Context, string, *int64, string, uint) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) UnlinkFromBitrix(context.Context, string, uint, []string) (*tickets.Ticket, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) Delete(context.Context, string, uint, []string) error {
	return nil
}
func (s *ticketHandlerServiceStub) AutoCloseResolvedTickets(context.Context, time.Duration) (int, error) {
	return 0, nil
}
func (s *ticketHandlerServiceStub) ProcessExpiredDeferred(context.Context, time.Time, int) ([]services.DeferredStatusActivation, error) {
	return nil, nil
}
func (s *ticketHandlerServiceStub) LinkToAsset(context.Context, string, string, string) error {
	return nil
}

type captureEventBus struct {
	events []eventbus.Event
}

func (b *captureEventBus) Publish(event eventbus.Event) {
	b.events = append(b.events, event)
}
func (b *captureEventBus) Subscribe(string, eventbus.EventHandler) {}
func (b *captureEventBus) SubscribeChannel(context.Context, int, ...string) <-chan eventbus.Event {
	return nil
}
func (b *captureEventBus) Start(context.Context, logger.LoggerInterface) {}
func (b *captureEventBus) GetDebugInfo() eventbus.DebugInfo              { return eventbus.DebugInfo{} }

func TestTicketHandler_ResolvePyrusTaskIDForTicketUsesTicketLink(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket-handler-pyrus-link?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&pyrus.TicketLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	pyrusRepo := infraRepos.NewPyrusRepo(db)
	if err := pyrusRepo.UpsertTicketLink(t.Context(), &pyrus.TicketLink{
		TicketID:    "ticket-1",
		PyrusTaskID: 345176232,
	}); err != nil {
		t.Fatalf("не удалось сохранить pyrus_ticket_link: %v", err)
	}

	handler := NewTicketHandler(
		&ticketHandlerServiceStub{details: &tickets.TicketDetails{Metadata: tickets.Ticket{Base: common.Base{ID: "ticket-1"}}}},
		nil,
		pyrusRepo,
	)

	taskID, ok := handler.resolvePyrusTaskIDForTicket(t.Context(), "ticket-1")
	if !ok {
		t.Fatal("ожидали успешное определение task_id через pyrus_ticket_links")
	}
	if taskID != 345176232 {
		t.Fatalf("ожидали task_id=345176232, получили %d", taskID)
	}
}

func TestTicketHandler_PublishPyrusCommentSyncForTicketUsesTicketLink(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:ticket-handler-pyrus-publish?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&pyrus.TicketLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}
	pyrusRepo := infraRepos.NewPyrusRepo(db)
	if err := pyrusRepo.UpsertTicketLink(t.Context(), &pyrus.TicketLink{
		TicketID:    "ticket-2",
		PyrusTaskID: 345176232,
	}); err != nil {
		t.Fatalf("не удалось сохранить pyrus_ticket_link: %v", err)
	}

	bus := &captureEventBus{}
	handler := NewTicketHandler(
		&ticketHandlerServiceStub{details: &tickets.TicketDetails{Metadata: tickets.Ticket{Base: common.Base{ID: "ticket-2"}}}},
		bus,
		pyrusRepo,
	)

	handler.publishPyrusCommentSyncForTicket(t.Context(), "ticket-2", tickets.TicketComment{
		ID:   "comment-1",
		Text: "Ответ клиенту",
	}, 77)

	if len(bus.events) != 1 {
		t.Fatalf("ожидали одно опубликованное событие, получили %d", len(bus.events))
	}
	if bus.events[0].Type != events.PyrusCommentSyncRequested {
		t.Fatalf("ожидали событие %q, получили %q", events.PyrusCommentSyncRequested, bus.events[0].Type)
	}

	payload, ok := bus.events[0].Payload.(events.PyrusSyncEntityPayload)
	if !ok {
		t.Fatalf("ожидали payload типа PyrusSyncEntityPayload, получили %T", bus.events[0].Payload)
	}
	if payload.TaskID != 345176232 {
		t.Fatalf("ожидали task_id=345176232 из pyrus_ticket_links, получили %d", payload.TaskID)
	}
}
