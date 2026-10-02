package integrations

import (
	"context"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	pyrusplugin "etalon-server/internal/infra/plugins/pyrus"
	"etalon-server/internal/services"
	"etalon-server/pkg/eventbus"
)

type fakePyrusSync struct {
	mu       sync.Mutex
	imported []events.TicketCommentImportedPayload
}

func (f *fakePyrusSync) IsEnabled() bool                                           { return true }
func (f *fakePyrusSync) Start(context.Context)                                     {}
func (f *fakePyrusSync) ListMembers(context.Context) ([]pyrusplugin.Member, error) { return nil, nil }
func (f *fakePyrusSync) EnqueueEvent(context.Context, string, events.PyrusSyncEntityPayload) error {
	return nil
}

func (f *fakePyrusSync) EnqueueImportedComment(_ context.Context, payload events.TicketCommentImportedPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imported = append(f.imported, payload)
	return nil
}

func (f *fakePyrusSync) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.imported)
}

type fakeBitrixSync struct {
	services.BitrixSyncService
	mu      sync.Mutex
	tickets []string
}

func (f *fakeBitrixSync) IsEnabled() bool { return true }

func (f *fakeBitrixSync) SyncPendingComments(_ context.Context, ticketID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets = append(f.tickets, ticketID)
	return nil
}

func (f *fakeBitrixSync) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tickets)
}

// Импортированный комментарий доставляется во вторую систему и никогда не возвращается в ту, откуда пришёл.
func TestImportedCommentIsBridgedBetweenBitrixAndPyrusWithoutEcho(t *testing.T) {
	log := logger.New("", "test", "error", true)
	cfg := &config.Config{EnableBitrixGateway: true, EnablePyrusGateway: true}
	bus := eventbus.NewInMemoryEventBus(100)
	pyrusSync := &fakePyrusSync{}
	bitrixSync := &fakeBitrixSync{}
	RegisterPyrusEventHandlers(cfg, log, bus, pyrusSync)
	RegisterBitrixEventHandlers(cfg, log, bus, bitrixSync)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go bus.Start(ctx, log)

	comment := &tickets.TicketComment{ID: "c-1", TicketID: "t-1", Text: "Текст"}
	bus.Publish(eventbus.Event{Type: events.TicketCommentImported, Payload: events.TicketCommentImportedPayload{
		TicketID: "t-1", Comment: comment, Source: events.CommentImportSourceBitrix,
	}})
	deadline := time.Now().Add(2 * time.Second)
	for pyrusSync.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if pyrusSync.count() != 1 || bitrixSync.count() != 0 {
		t.Fatalf("комментарий из Bitrix24 должен уйти только в Pyrus: pyrus=%d bitrix=%d", pyrusSync.count(), bitrixSync.count())
	}

	bus.Publish(eventbus.Event{Type: events.TicketCommentImported, Payload: events.TicketCommentImportedPayload{
		TicketID: "t-1", Comment: comment, Source: events.CommentImportSourcePyrus,
	}})
	bus.Publish(eventbus.Event{Type: events.TicketCommentImported, Payload: events.TicketCommentImportedPayload{
		TicketID: "t-2", Comment: &tickets.TicketComment{ID: "c-2", IsPrivate: true}, Source: events.CommentImportSourcePyrus,
	}})
	deadline = time.Now().Add(2 * time.Second)
	for bitrixSync.count() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if bitrixSync.count() != 1 || pyrusSync.count() != 1 {
		t.Fatalf("комментарий из Pyrus должен уйти только в Bitrix24, приватный не уходит: pyrus=%d bitrix=%d", pyrusSync.count(), bitrixSync.count())
	}
}
