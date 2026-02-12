package services

import (
	"context"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeAgentRepo struct {
	mu       sync.Mutex
	agent    *models.Agent
	created  int
	updated  int
	commands []models.AgentCommand
}

func (r *fakeAgentRepo) GetByUUID(_ context.Context, _ string) (*models.Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agent == nil {
		return nil, nil
	}
	copyAgent := *r.agent
	return &copyAgent, nil
}

func (r *fakeAgentRepo) Create(_ context.Context, agent *models.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyAgent := *agent
	r.agent = &copyAgent
	r.created++
	return nil
}

func (r *fakeAgentRepo) Update(_ context.Context, agent *models.Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copyAgent := *agent
	r.agent = &copyAgent
	r.updated++
	return nil
}

func (r *fakeAgentRepo) CountByOwnerUUID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}

func (r *fakeAgentRepo) GetPendingCommands(_ context.Context, _ string) ([]models.AgentCommand, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.AgentCommand, len(r.commands))
	copy(out, r.commands)
	return out, nil
}

func (r *fakeAgentRepo) MarkCommandsAsSent(_ context.Context, _ []uint) error {
	return nil
}

type fakeEventBus struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (b *fakeEventBus) Publish(event eventbus.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, event)
}

func (b *fakeEventBus) Subscribe(_ string, _ eventbus.EventHandler) {}

func (b *fakeEventBus) SubscribeChannel(_ context.Context, _ int, _ ...string) <-chan eventbus.Event {
	ch := make(chan eventbus.Event)
	close(ch)
	return ch
}

func (b *fakeEventBus) Start(_ context.Context, _ logger.LoggerInterface) {}

func (b *fakeEventBus) GetDebugInfo() eventbus.DebugInfo {
	return eventbus.DebugInfo{}
}

func (b *fakeEventBus) lastEvent() (eventbus.Event, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) == 0 {
		return eventbus.Event{}, false
	}
	return b.events[len(b.events)-1], true
}

func TestProcessData_ПубликуетСобытиеНаблюденияАгента(t *testing.T) {
	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-1",
			Type:          "workstation",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, nil, bus)

	_, err := svc.ProcessData(context.Background(), "agent-1", &api.AgentDataDTO{
		AgentUUID: "agent-1",
		Hostname:  "ws-1",
	})
	require.NoError(t, err)

	ev, ok := bus.lastEvent()
	require.True(t, ok)
	require.Equal(t, events.AgentObservationRequested, ev.Type)

	payload, ok := ev.Payload.(events.AgentObservationPayload)
	require.True(t, ok)
	require.Equal(t, "agent-1", payload.Source)
	require.Equal(t, "ws-1", payload.Data.Hostname)
	require.Equal(t, 1, repo.updated)
}
