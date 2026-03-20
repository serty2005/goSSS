package services

import (
	"context"
	"encoding/json"
	"etalon-server/internal/core/events"
	"etalon-server/internal/domain/models"
	"etalon-server/internal/infra/logger"
	api "etalon-server/internal/transport/http/dtos"
	"etalon-server/pkg/eventbus"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
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
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

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

func TestProcessData_SSSRunerВозвращаетAdapterManifestsИзКонфига(t *testing.T) {
	manifestConfig, err := json.Marshal(api.AgentConfigDTO{
		AdapterManifests: []api.AdapterManifestDTO{
			{
				AdapterID:       "atol",
				AdapterType:     "fiscal",
				Version:         "1.0.0",
				ProtocolVersion: "phase0",
				DownloadURL:     "https://example.test/atol.exe",
				SHA256:          "abc123",
			},
		},
	})
	require.NoError(t, err)

	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-sssruner",
			Type:          "sssruner",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
			Config:        datatypes.JSON(manifestConfig),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	resp, err := svc.ProcessData(context.Background(), "agent-sssruner", &api.AgentDataDTO{
		AgentUUID: "agent-sssruner",
		AgentType: "sssruner",
		Hostname:  "ws-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.AdapterManifests)
	require.Len(t, *resp.AdapterManifests, 1)
	require.Equal(t, "atol", (*resp.AdapterManifests)[0].AdapterID)
}

func TestProcessData_SSSRunerВозвращаетПустыеAdapterManifestsПриПустомКонфиге(t *testing.T) {
	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-empty-config",
			Type:          "sssruner",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	resp, err := svc.ProcessData(context.Background(), "agent-empty-config", &api.AgentDataDTO{
		AgentUUID: "agent-empty-config",
		AgentType: "sssruner",
		Hostname:  "ws-1",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.AdapterManifests)
	require.Empty(t, *resp.AdapterManifests)
}

func TestProcessData_SSSRunerБитыйКонфигНеЛомаетHeartbeat(t *testing.T) {
	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-bad-config",
			Type:          "sssruner",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
			Config:        datatypes.JSON([]byte(`{"adapter_manifests":`)),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	resp, err := svc.ProcessData(context.Background(), "agent-bad-config", &api.AgentDataDTO{
		AgentUUID: "agent-bad-config",
		AgentType: "sssruner",
		Hostname:  "ws-1",
	})
	require.NoError(t, err)
	require.Equal(t, "ok", resp.Status)
	require.NotNil(t, resp.AdapterManifests)
	require.Empty(t, *resp.AdapterManifests)
}

func TestProcessData_НеSSSRunerСохраняетПрежнееПоведение(t *testing.T) {
	manifestConfig, err := json.Marshal(api.AgentConfigDTO{
		AdapterManifests: []api.AdapterManifestDTO{{AdapterID: "atol"}},
	})
	require.NoError(t, err)

	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-workstation",
			Type:          "workstation",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
			Config:        datatypes.JSON(manifestConfig),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	resp, err := svc.ProcessData(context.Background(), "agent-workstation", &api.AgentDataDTO{
		AgentUUID: "agent-workstation",
		Hostname:  "ws-1",
	})
	require.NoError(t, err)
	require.Nil(t, resp.AdapterManifests)
	require.Empty(t, resp.Tasks)
}

func TestProcessData_СохраняетПоследнийHeartbeatSnapshotВAgent(t *testing.T) {
	repo := &fakeAgentRepo{
		agent: &models.Agent{
			UUID:          "agent-phase1",
			Type:          "sssruner",
			Status:        models.StatusActive,
			LastHeartbeat: time.Now().Add(-time.Hour),
		},
	}
	bus := &fakeEventBus{}
	svc := NewAgentService(logger.New("", "test", "error", true), repo, nil, bus)

	_, err := svc.ProcessData(context.Background(), "agent-phase1", &api.AgentDataDTO{
		AgentUUID:   "agent-phase1",
		AgentType:   "sssruner",
		Hostname:    "ws-phase1",
		CurrentTime: "2026-03-21T09:30:00Z",
		Inventory: &api.InventorySnapshotDTO{
			CollectedAt: time.Date(2026, 3, 21, 9, 30, 0, 0, time.UTC),
			Hostname:    "ws-phase1",
			OS:          "windows",
			Arch:        "amd64",
		},
		AdapterStatuses: []api.AdapterStatusDTO{
			{AdapterID: "atol", Status: "ready", Version: "1.0.0"},
		},
	})
	require.NoError(t, err)

	require.NotNil(t, repo.agent.LastObservedAt)
	require.Equal(t, time.Date(2026, 3, 21, 9, 30, 0, 0, time.UTC), repo.agent.LastObservedAt.UTC())

	var latestInventory map[string]any
	require.NoError(t, json.Unmarshal(repo.agent.LatestInventorySnapshot, &latestInventory))
	require.Equal(t, "ws-phase1", latestInventory["hostname"])

	var latestStatuses []map[string]any
	require.NoError(t, json.Unmarshal(repo.agent.LatestAdapterStatuses, &latestStatuses))
	require.Len(t, latestStatuses, 1)
	require.Equal(t, "atol", latestStatuses[0]["adapter_id"])
}
