package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/connectivity"
	"etalon-agent/internal/hostinfo"
	"etalon-agent/internal/inventory"
	"etalon-agent/internal/protocol"
	"etalon-agent/internal/state"
	"etalon-agent/internal/workflows"
)

type stubRuntimeClient struct {
	registerFn      func(context.Context, string, protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error)
	refreshFn       func(context.Context, protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error)
	sendHeartbeatFn func(context.Context, string, protocol.AgentDataDTO, string) (*protocol.HeartbeatResponseDTO, error)
}

func (c stubRuntimeClient) Register(ctx context.Context, bootstrapAPIKey string, req protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
	if c.registerFn == nil {
		return nil, errors.New("register не настроен")
	}
	return c.registerFn(ctx, bootstrapAPIKey, req)
}

func (c stubRuntimeClient) RefreshTokens(ctx context.Context, req protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error) {
	if c.refreshFn == nil {
		return nil, errors.New("refresh не настроен")
	}
	return c.refreshFn(ctx, req)
}

func (c stubRuntimeClient) SendHeartbeat(ctx context.Context, agentUUID string, data protocol.AgentDataDTO, accessToken string) (*protocol.HeartbeatResponseDTO, error) {
	if c.sendHeartbeatFn == nil {
		return nil, errors.New("heartbeat не настроен")
	}
	return c.sendHeartbeatFn(ctx, agentUUID, data, accessToken)
}

type stubRuntimeRegistryStore struct {
	loadTokensFn                func() (*state.Tokens, error)
	saveTokensFn                func(state.Tokens) error
	ensureIdentityFn            func(string, func() (string, error)) (*state.Identity, error)
	collectRegistrationInfoFunc func(string) map[string]interface{}
}

func (s stubRuntimeRegistryStore) EnsureIdentity(currentFingerprintHash string, uuidFactory func() (string, error)) (*state.Identity, error) {
	if s.ensureIdentityFn == nil {
		return nil, errors.New("EnsureIdentity не настроен")
	}
	return s.ensureIdentityFn(currentFingerprintHash, uuidFactory)
}

func (s stubRuntimeRegistryStore) LoadTokens() (*state.Tokens, error) {
	if s.loadTokensFn == nil {
		return nil, nil
	}
	return s.loadTokensFn()
}

func (s stubRuntimeRegistryStore) SaveTokens(tokens state.Tokens) error {
	if s.saveTokensFn == nil {
		return nil
	}
	return s.saveTokensFn(tokens)
}

func (s stubRuntimeRegistryStore) CollectRegistrationSystemInfo(agentProcessName string) map[string]interface{} {
	if s.collectRegistrationInfoFunc == nil {
		return map[string]interface{}{"agent_process": agentProcessName}
	}
	return s.collectRegistrationInfoFunc(agentProcessName)
}

type stubRuntimeInventoryService struct {
	snapshot SnapshotResult
}

type SnapshotResult struct {
	value inventory.Snapshot
	ok    bool
}

func (s stubRuntimeInventoryService) Interval() time.Duration {
	return time.Minute
}

func (s stubRuntimeInventoryService) CollectNow(context.Context) (inventory.Snapshot, error) {
	return s.snapshot.value, nil
}

func (s stubRuntimeInventoryService) Snapshot() (inventory.Snapshot, bool) {
	return s.snapshot.value, s.snapshot.ok
}

type stubRuntimeAdapterManager struct {
	statuses []adapters.Status
	listErr  error
	syncFn   func(context.Context, []adapters.ManifestItem) ([]adapters.Status, error)
	runFn    func(context.Context, adapters.RunRequest) (adapters.RunResult, error)
}

func (m stubRuntimeAdapterManager) EnsureLayout() error {
	return nil
}

func (m stubRuntimeAdapterManager) ListStatuses() ([]adapters.Status, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	return m.statuses, nil
}

func (m stubRuntimeAdapterManager) Sync(ctx context.Context, manifests []adapters.ManifestItem) ([]adapters.Status, error) {
	if m.syncFn == nil {
		return nil, nil
	}
	return m.syncFn(ctx, manifests)
}

func (m stubRuntimeAdapterManager) Run(ctx context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
	if m.runFn == nil {
		return adapters.RunResult{}, errors.New("run не настроен")
	}
	return m.runFn(ctx, req)
}

func newTestConnectivityManager(t *testing.T) *connectivity.Manager {
	t.Helper()

	manager, err := connectivity.NewManager(filepath.Join(t.TempDir(), "connectivity-state.json"), connectivity.Policy{
		BaseRetry:             15 * time.Second,
		MaxRetry:              10 * time.Minute,
		RegistrationCooldown:  30 * time.Minute,
		AuthorizationCooldown: 24 * time.Hour,
		RateLimitCooldown:     5 * time.Minute,
		ConfigErrorCooldown:   time.Hour,
		ProtocolErrorCooldown: 30 * time.Minute,
		RetryJitterFactor:     0,
	})
	if err != nil {
		t.Fatalf("не удалось создать manager связи: %v", err)
	}
	return manager
}

func TestAgentBuildHeartbeatPayloadIncludesInventoryAndAdapterStatuses(t *testing.T) {
	t.Parallel()

	snapshot := inventory.Snapshot{
		CollectedAt: time.Unix(300, 0).UTC(),
		Hostname:    "inventory-host",
		OS:          "windows",
		Arch:        "amd64",
		HostInfo: &hostinfo.Snapshot{
			CashServerURL: "https://demo.iiko.ru:443/resto/",
			TeamviewerID:  "111111111",
			AnydeskID:     "222222222",
			LitemanagerID: "333333333",
			RustdeskID:    "444444444",
		},
	}
	statuses := []adapters.Status{{
		AdapterID: "adapter-a",
		Version:   "1.0.0",
		Status:    "ready",
	}}

	agent := &Agent{
		cfg: config.Config{
			AgentVersion: "test-version",
			AgentType:    "test-agent",
		},
		identity: &state.Identity{UUID: "agent-uuid"},
		inventoryService: stubRuntimeInventoryService{
			snapshot: SnapshotResult{value: snapshot, ok: true},
		},
		adapterManager: stubRuntimeAdapterManager{statuses: statuses},
	}

	payload, err := agent.buildHeartbeatPayload()
	if err != nil {
		t.Fatalf("buildHeartbeatPayload завершился ошибкой: %v", err)
	}
	if payload.AgentUUID != "agent-uuid" {
		t.Fatalf("ожидался uuid agent-uuid, получено %q", payload.AgentUUID)
	}
	if payload.AgentType != "test-agent" {
		t.Fatalf("ожидался agent_type test-agent, получено %q", payload.AgentType)
	}
	if payload.Inventory == nil {
		t.Fatal("ожидался inventory в payload")
	}
	if !reflect.DeepEqual(*payload.Inventory, snapshot) {
		t.Fatalf("inventory в payload отличается: %+v", *payload.Inventory)
	}
	if !reflect.DeepEqual(payload.AdapterStatuses, statuses) {
		t.Fatalf("adapter_statuses в payload отличаются: %+v", payload.AdapterStatuses)
	}
	if payload.URLRms != "https://demo.iiko.ru:443/resto/" {
		t.Fatalf("ожидался url_rms из inventory, получено %q", payload.URLRms)
	}
	if payload.TeamviewerID != "111111111" {
		t.Fatalf("ожидался teamviewer_id 111111111, получено %q", payload.TeamviewerID)
	}
	if payload.AnydeskID != "222222222" {
		t.Fatalf("ожидался anydesk_id 222222222, получено %q", payload.AnydeskID)
	}
	if payload.LitemanagerID != "333333333" {
		t.Fatalf("ожидался litemanager_id 333333333, получено %q", payload.LitemanagerID)
	}
	if payload.RustdeskID != "444444444" {
		t.Fatalf("ожидался rustdesk_id 444444444, получено %q", payload.RustdeskID)
	}
}

func TestAgentBuildHeartbeatPayloadWithoutInventory(t *testing.T) {
	t.Parallel()

	agent := &Agent{
		cfg: config.Config{
			AgentVersion: "test-version",
			AgentType:    "test-agent",
		},
		identity:         &state.Identity{UUID: "agent-uuid"},
		inventoryService: stubRuntimeInventoryService{},
		adapterManager: stubRuntimeAdapterManager{statuses: []adapters.Status{{
			AdapterID: "adapter-a",
			Version:   "1.0.0",
			Status:    "ready",
		}}},
	}

	payload, err := agent.buildHeartbeatPayload()
	if err != nil {
		t.Fatalf("buildHeartbeatPayload завершился ошибкой: %v", err)
	}
	if payload.Inventory != nil {
		t.Fatalf("при отсутствии inventory payload не должен содержать snapshot: %+v", payload.Inventory)
	}
}

func TestAgentHeartbeatSyncRunsOnlyWhenServerReturnedManifests(t *testing.T) {
	t.Parallel()

	newAgent := func(resp protocol.HeartbeatResponseDTO, syncCounter *int) *Agent {
		return &Agent{
			cfg: config.Config{
				AgentVersion:           "test-version",
				AgentType:              "test-agent",
				AccessTokenGracePeriod: time.Minute,
			},
			client: stubRuntimeClient{
				sendHeartbeatFn: func(context.Context, string, protocol.AgentDataDTO, string) (*protocol.HeartbeatResponseDTO, error) {
					return &resp, nil
				},
			},
			identity: &state.Identity{UUID: "agent-uuid"},
			tokens: &state.Tokens{
				AccessToken:          "access-token",
				AccessTokenExpiresAt: time.Now().Add(time.Hour),
			},
			inventoryService: stubRuntimeInventoryService{},
			adapterManager: stubRuntimeAdapterManager{
				syncFn: func(_ context.Context, manifests []adapters.ManifestItem) ([]adapters.Status, error) {
					*syncCounter++
					return nil, nil
				},
			},
		}
	}

	var syncWithoutManifests int
	agentWithoutManifests := newAgent(protocol.HeartbeatResponseDTO{Status: "ok"}, &syncWithoutManifests)
	if err := agentWithoutManifests.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("heartbeat без manifest завершился ошибкой: %v", err)
	}
	if syncWithoutManifests != 0 {
		t.Fatalf("без manifest Sync не должен вызываться, получено %d вызовов", syncWithoutManifests)
	}

	var syncWithManifests int
	agentWithManifests := newAgent(protocol.HeartbeatResponseDTO{
		Status: "ok",
		AdapterManifests: []adapters.ManifestItem{{
			AdapterID:   "adapter-a",
			Version:     "1.0.0",
			DownloadURL: "https://example.invalid/adapter-a",
		}},
	}, &syncWithManifests)
	if err := agentWithManifests.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("heartbeat с manifest завершился ошибкой: %v", err)
	}
	if syncWithManifests != 1 {
		t.Fatalf("при наличии manifest ожидался один вызов Sync, получено %d", syncWithManifests)
	}
}

func TestAgentHeartbeatRetriesAfter401(t *testing.T) {
	t.Parallel()

	var saveCalls int
	var sendCalls int
	var refreshCalls int
	accessTokens := make([]string, 0, 2)

	agent := &Agent{
		cfg: config.Config{
			AgentVersion:           "test-version",
			AgentType:              "test-agent",
			AccessTokenGracePeriod: time.Minute,
		},
		client: stubRuntimeClient{
			refreshFn: func(_ context.Context, req protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error) {
				refreshCalls++
				if req.AgentUUID != "agent-uuid" {
					t.Fatalf("ожидался AgentUUID agent-uuid, получено %q", req.AgentUUID)
				}
				if req.RefreshToken != "refresh-token" {
					t.Fatalf("ожидался refresh token refresh-token, получено %q", req.RefreshToken)
				}
				return &protocol.AgentTokenRefreshResponseDTO{
					AccessToken:           "new-access-token",
					RefreshToken:          "new-refresh-token",
					AccessTokenExpiresAt:  time.Now().Add(time.Hour),
					RefreshTokenExpiresAt: time.Now().Add(2 * time.Hour),
				}, nil
			},
			sendHeartbeatFn: func(_ context.Context, agentUUID string, _ protocol.AgentDataDTO, accessToken string) (*protocol.HeartbeatResponseDTO, error) {
				sendCalls++
				accessTokens = append(accessTokens, accessToken)
				if agentUUID != "agent-uuid" {
					t.Fatalf("ожидался agentUUID agent-uuid, получено %q", agentUUID)
				}
				if sendCalls == 1 {
					return nil, &client.HTTPError{StatusCode: 401, Body: "unauthorized"}
				}
				return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
			},
		},
		registryStore: stubRuntimeRegistryStore{
			saveTokensFn: func(tokens state.Tokens) error {
				saveCalls++
				if tokens.AccessToken != "new-access-token" || tokens.RefreshToken != "new-refresh-token" {
					t.Fatalf("сохранены неожиданные токены: %+v", tokens)
				}
				return nil
			},
		},
		identity: &state.Identity{UUID: "agent-uuid"},
		tokens: &state.Tokens{
			AccessToken:           "old-access-token",
			RefreshToken:          "refresh-token",
			AccessTokenExpiresAt:  time.Now().Add(time.Hour),
			RefreshTokenExpiresAt: time.Now().Add(2 * time.Hour),
		},
		inventoryService: stubRuntimeInventoryService{
			snapshot: SnapshotResult{
				value: inventory.Snapshot{
					CollectedAt: time.Unix(400, 0).UTC(),
					Hostname:    "inventory-host",
				},
				ok: true,
			},
		},
		adapterManager: stubRuntimeAdapterManager{},
	}

	if err := agent.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("heartbeat после 401 завершился ошибкой: %v", err)
	}
	if refreshCalls != 1 {
		t.Fatalf("ожидался один вызов RefreshTokens, получено %d", refreshCalls)
	}
	if saveCalls != 1 {
		t.Fatalf("ожидалось одно сохранение токенов, получено %d", saveCalls)
	}
	if sendCalls != 2 {
		t.Fatalf("ожидалось две попытки heartbeat, получено %d", sendCalls)
	}
	if !reflect.DeepEqual(accessTokens, []string{"old-access-token", "new-access-token"}) {
		t.Fatalf("ожидалась повторная отправка heartbeat с новым access token, получено %v", accessTokens)
	}
	if agent.tokens == nil || agent.tokens.AccessToken != "new-access-token" {
		t.Fatalf("в агенте не обновился access token: %+v", agent.tokens)
	}
	if agent.tokens.RefreshToken != "new-refresh-token" {
		t.Fatalf("в агенте не обновился refresh token: %+v", agent.tokens)
	}
}

func TestAgentHeartbeatAdapterTaskErrorQueuesTaskResultAndDoesNotBreakLoop(t *testing.T) {
	t.Parallel()

	var heartbeatCalls int
	agent := &Agent{
		cfg: config.Config{
			AgentVersion:           "test-version",
			AgentType:              "sssruner",
			AccessTokenGracePeriod: time.Minute,
		},
		client: stubRuntimeClient{
			sendHeartbeatFn: func(_ context.Context, agentUUID string, data protocol.AgentDataDTO, accessToken string) (*protocol.HeartbeatResponseDTO, error) {
				heartbeatCalls++
				if agentUUID != "agent-uuid" {
					t.Fatalf("ожидался agentUUID agent-uuid, получено %q", agentUUID)
				}
				if accessToken != "access-token" {
					t.Fatalf("ожидался access token access-token, получено %q", accessToken)
				}

				switch heartbeatCalls {
				case 1:
					if len(data.TaskResults) != 0 {
						t.Fatalf("первый heartbeat не должен содержать task_results, получено %d", len(data.TaskResults))
					}
					return &protocol.HeartbeatResponseDTO{
						Status: "ok",
						Tasks: []protocol.AgentTaskDTO{{
							ID:   42,
							Type: "run_adapter",
							Payload: []byte(`{
								"adapter_id":"fiscal-atol",
								"operation":"collect",
								"timeout_seconds":5,
								"device_params":{"connection_type":"tcp","ip":"10.0.0.10","port":5555}
							}`),
						}},
					}, nil
				case 2:
					if len(data.TaskResults) != 1 {
						t.Fatalf("второй heartbeat должен содержать один task_result, получено %d", len(data.TaskResults))
					}
					result := data.TaskResults[0]
					if result.ID != 42 {
						t.Fatalf("ожидался task id 42, получено %d", result.ID)
					}
					if result.Type != "run_adapter" {
						t.Fatalf("ожидался task type run_adapter, получено %q", result.Type)
					}
					if result.Status != "failed" {
						t.Fatalf("ожидался статус failed, получено %q", result.Status)
					}
					if result.AdapterID != "fiscal-atol" {
						t.Fatalf("ожидался adapter_id fiscal-atol, получено %q", result.AdapterID)
					}
					if result.Error == "" {
						t.Fatal("ожидалась текстовая ошибка запуска адаптера")
					}
					return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
				default:
					return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
				}
			},
		},
		workflows: make(map[string]workflow),
		identity:  &state.Identity{UUID: "agent-uuid"},
		tokens: &state.Tokens{
			AccessToken:          "access-token",
			AccessTokenExpiresAt: time.Now().Add(time.Hour),
		},
		inventoryService: stubRuntimeInventoryService{},
		adapterManager: stubRuntimeAdapterManager{
			runFn: func(_ context.Context, req adapters.RunRequest) (adapters.RunResult, error) {
				if req.AdapterID != "fiscal-atol" {
					t.Fatalf("ожидался adapter_id fiscal-atol, получено %q", req.AdapterID)
				}
				if req.Command != "run" {
					t.Fatalf("ожидалась команда run, получено %q", req.Command)
				}
				return adapters.RunResult{
					AdapterID:   req.AdapterID,
					Command:     req.Command,
					CompletedAt: time.Now().UTC(),
					RunStatus:   "failed",
				}, errors.New("локальный бинарник адаптера fiscal-atol не найден")
			},
		},
	}
	adapterWorkflow := workflows.NewAdapterRunWorkflow(agent.adapterManager)
	agent.registerWorkflow(adapterWorkflow)
	agent.registerWorkflowAlias("adapter_run", adapterWorkflow)

	if err := agent.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("heartbeat с ошибкой запуска адаптера не должен завершаться ошибкой: %v", err)
	}
	if len(agent.pendingTaskResults) != 1 {
		t.Fatalf("после первого heartbeat ожидался один накопленный task_result, получено %d", len(agent.pendingTaskResults))
	}

	if err := agent.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("heartbeat с отправкой task_result завершился ошибкой: %v", err)
	}
	if len(agent.pendingTaskResults) != 0 {
		t.Fatalf("после успешной отправки task_result очередь должна очиститься, осталось %d", len(agent.pendingTaskResults))
	}
	if heartbeatCalls != 2 {
		t.Fatalf("ожидалось два heartbeat вызова, получено %d", heartbeatCalls)
	}
}

func TestAgentApplyRegistrationResponseRejectsEmptyTokens(t *testing.T) {
	t.Parallel()

	agent := &Agent{}
	err := agent.applyRegistrationResponse(protocol.AgentRegistrationResponseDTO{
		Status:                "ok",
		AgentUUID:             "agent-uuid",
		AccessToken:           "",
		RefreshToken:          "refresh-token",
		AccessTokenExpiresAt:  time.Now().Add(time.Hour),
		RefreshTokenExpiresAt: time.Now().Add(2 * time.Hour),
	})
	if err == nil {
		t.Fatal("ожидалась ошибка на пустом access token")
	}
	if agent.tokens != nil {
		t.Fatalf("токены не должны сохраняться при невалидном ответе: %+v", agent.tokens)
	}
}

func TestAgentApplyRegistrationResponsePendingApprovalDoesNotPersistTokens(t *testing.T) {
	t.Parallel()

	agent := &Agent{}
	err := agent.applyRegistrationResponse(protocol.AgentRegistrationResponseDTO{
		Status:    "pending_approval",
		Message:   "Регистрация ожидает подтверждения оператором",
		AgentUUID: "agent-uuid",
	})
	if err == nil {
		t.Fatal("ожидалась ошибка pending approval")
	}

	var pendingErr *connectivity.PendingRegistrationError
	if !errors.As(err, &pendingErr) {
		t.Fatalf("ожидалась ошибка *PendingRegistrationError, получено %T", err)
	}
	if pendingErr.Error() != "Регистрация ожидает подтверждения оператором" {
		t.Fatalf("получено неожиданное сообщение pending approval: %q", pendingErr.Error())
	}
	if agent.tokens != nil {
		t.Fatalf("токены не должны сохраняться до подтверждения регистрации: %+v", agent.tokens)
	}
}

func TestAgentApplyTokenRefreshResponseRejectsZeroExpiry(t *testing.T) {
	t.Parallel()

	agent := &Agent{}
	err := agent.applyTokenRefreshResponse(protocol.AgentTokenRefreshResponseDTO{
		Status:                "ok",
		AgentUUID:             "agent-uuid",
		AccessToken:           "access-token",
		RefreshToken:          "refresh-token",
		RefreshTokenExpiresAt: time.Now().Add(2 * time.Hour),
	})
	if err == nil {
		t.Fatal("ожидалась ошибка на нулевом сроке жизни access token")
	}
	if agent.tokens != nil {
		t.Fatalf("токены не должны сохраняться при невалидном refresh-ответе: %+v", agent.tokens)
	}
}

func TestAgentBuildHeartbeatPayloadUsesHostname(t *testing.T) {
	t.Parallel()

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("не удалось получить hostname системы: %v", err)
	}

	agent := &Agent{
		cfg: config.Config{
			AgentVersion: "test-version",
			AgentType:    "test-agent",
		},
		identity:         &state.Identity{UUID: "agent-uuid"},
		inventoryService: stubRuntimeInventoryService{},
		adapterManager:   stubRuntimeAdapterManager{},
	}

	payload, err := agent.buildHeartbeatPayload()
	if err != nil {
		t.Fatalf("buildHeartbeatPayload завершился ошибкой: %v", err)
	}
	if payload.Hostname != host {
		t.Fatalf("ожидался hostname %q, получено %q", host, payload.Hostname)
	}
}

func TestAgentBuildRegistrationRequestIncludesSystemInfo(t *testing.T) {
	t.Parallel()

	host, err := os.Hostname()
	if err != nil {
		t.Fatalf("не удалось получить hostname системы: %v", err)
	}

	expectedSystemInfo := map[string]interface{}{
		"agent_process": "XenionAgent",
		"hostname":      host,
		"os":            "windows",
		"registry_path": `Software\MyHoreca\XenionAgent`,
	}

	agent := &Agent{
		cfg: config.Config{
			AgentProcessName: "XenionAgent",
			AgentType:        "sssruner",
			AgentVersion:     "1.2.3",
		},
		registryStore: stubRuntimeRegistryStore{
			collectRegistrationInfoFunc: func(agentProcessName string) map[string]interface{} {
				if agentProcessName != "XenionAgent" {
					t.Fatalf("ожидалось имя процесса XenionAgent, получено %q", agentProcessName)
				}
				return expectedSystemInfo
			},
		},
		identity:           &state.Identity{UUID: "agent-uuid"},
		machineFingerprint: "fingerprint-hash",
		inventoryService: stubRuntimeInventoryService{
			snapshot: SnapshotResult{
				value: inventory.Snapshot{
					CollectedAt: time.Unix(500, 0).UTC(),
					Hostname:    "inventory-host",
					HostInfo: &hostinfo.Snapshot{
						CashServerURL: "https://demo.syrve.online/resto/",
						TeamviewerID:  "tv-id",
						AnydeskID:     "ad-id",
						LitemanagerID: "lm-id",
						RustdeskID:    "rd-id",
					},
				},
				ok: true,
			},
		},
	}

	now := time.Date(2026, time.March, 20, 15, 4, 5, 0, time.UTC)
	req := agent.buildRegistrationRequest(now)

	if req.AgentUUID != "agent-uuid" {
		t.Fatalf("ожидался agent_uuid agent-uuid, получено %q", req.AgentUUID)
	}
	if req.Hostname != host {
		t.Fatalf("ожидался hostname %q, получено %q", host, req.Hostname)
	}
	if req.AgentVersion != "1.2.3" {
		t.Fatalf("ожидалась версия 1.2.3, получено %q", req.AgentVersion)
	}
	if req.MachineFingerprint != "fingerprint-hash" {
		t.Fatalf("ожидался machine_fingerprint fingerprint-hash, получено %q", req.MachineFingerprint)
	}
	if !reflect.DeepEqual(req.SystemInfo, expectedSystemInfo) {
		t.Fatalf("system_info отличается: %+v", req.SystemInfo)
	}
	if req.InitialData.Hostname != host {
		t.Fatalf("в initial_data ожидался hostname %q, получено %q", host, req.InitialData.Hostname)
	}
	if req.InitialData.CurrentTime != now.Format(time.RFC3339) {
		t.Fatalf("в initial_data ожидалось время %q, получено %q", now.Format(time.RFC3339), req.InitialData.CurrentTime)
	}
	if req.InitialData.AgentUUID != "agent-uuid" {
		t.Fatalf("в initial_data ожидался uuid agent-uuid, получено %q", req.InitialData.AgentUUID)
	}
	if req.InitialData.AgentType != "sssruner" {
		t.Fatalf("в initial_data ожидался agent_type sssruner, получено %q", req.InitialData.AgentType)
	}
	if req.InitialData.AgentVersion != "1.2.3" {
		t.Fatalf("в initial_data ожидалась версия 1.2.3, получено %q", req.InitialData.AgentVersion)
	}
	if req.InitialData.URLRms != "https://demo.syrve.online/resto/" {
		t.Fatalf("в initial_data ожидался url_rms, получено %q", req.InitialData.URLRms)
	}
	if req.InitialData.TeamviewerID != "tv-id" {
		t.Fatalf("в initial_data ожидался teamviewer_id, получено %q", req.InitialData.TeamviewerID)
	}
	if req.InitialData.AnydeskID != "ad-id" {
		t.Fatalf("в initial_data ожидался anydesk_id, получено %q", req.InitialData.AnydeskID)
	}
	if req.InitialData.LitemanagerID != "lm-id" {
		t.Fatalf("в initial_data ожидался litemanager_id, получено %q", req.InitialData.LitemanagerID)
	}
	if req.InitialData.RustdeskID != "rd-id" {
		t.Fatalf("в initial_data ожидался rustdesk_id, получено %q", req.InitialData.RustdeskID)
	}
	if req.InitialData.Inventory == nil {
		t.Fatal("в initial_data ожидался inventory snapshot")
	}
}

func TestAgentRegisterAndFetchTokensUsesBootstrapKeyAndPersistsTokens(t *testing.T) {
	t.Parallel()

	accessExpiresAt := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	refreshExpiresAt := time.Date(2026, time.April, 20, 10, 0, 0, 0, time.UTC)

	var capturedBootstrapKey string
	var capturedReq protocol.RegistrationRequestDTO
	var savedTokens state.Tokens

	agent := &Agent{
		cfg: config.Config{
			AgentProcessName: "XenionAgent",
			AgentType:        "sssruner",
			AgentVersion:     "1.2.3",
			BootstrapAPIKey:  "bootstrap-key",
			ServerURL:        "http://localhost:8080",
		},
		client: stubRuntimeClient{
			registerFn: func(_ context.Context, bootstrapAPIKey string, req protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
				capturedBootstrapKey = bootstrapAPIKey
				capturedReq = req
				return &protocol.AgentRegistrationResponseDTO{
					Status:                "ok",
					AgentUUID:             req.AgentUUID,
					AccessToken:           "access-token",
					AccessTokenExpiresAt:  accessExpiresAt,
					RefreshToken:          "refresh-token",
					RefreshTokenExpiresAt: refreshExpiresAt,
				}, nil
			},
		},
		registryStore: stubRuntimeRegistryStore{
			collectRegistrationInfoFunc: func(string) map[string]interface{} {
				return map[string]interface{}{
					"os":            "windows",
					"arch":          "amd64",
					"registry_path": `Software\MyHoreca\XenionAgent`,
				}
			},
			saveTokensFn: func(tokens state.Tokens) error {
				savedTokens = tokens
				return nil
			},
		},
		identity:           &state.Identity{UUID: "agent-uuid"},
		machineFingerprint: "fingerprint-hash",
	}

	if err := agent.registerAndFetchTokens(context.Background(), "тестовая регистрация"); err != nil {
		t.Fatalf("registerAndFetchTokens завершился ошибкой: %v", err)
	}

	if capturedBootstrapKey != "bootstrap-key" {
		t.Fatalf("ожидался bootstrap API key bootstrap-key, получено %q", capturedBootstrapKey)
	}
	if capturedReq.AgentUUID != "agent-uuid" {
		t.Fatalf("ожидался agent_uuid agent-uuid, получено %q", capturedReq.AgentUUID)
	}
	if capturedReq.MachineFingerprint != "fingerprint-hash" {
		t.Fatalf("ожидался machine_fingerprint fingerprint-hash, получено %q", capturedReq.MachineFingerprint)
	}
	if capturedReq.InitialData.AgentType != "sssruner" {
		t.Fatalf("ожидался agent_type sssruner, получено %q", capturedReq.InitialData.AgentType)
	}
	if agent.tokens == nil {
		t.Fatal("ожидалось, что токены будут сохранены в агенте")
	}
	if agent.tokens.AccessToken != "access-token" || agent.tokens.RefreshToken != "refresh-token" {
		t.Fatalf("в агенте сохранены неожиданные токены: %+v", agent.tokens)
	}
	if savedTokens.AccessToken != "access-token" || savedTokens.RefreshToken != "refresh-token" {
		t.Fatalf("в реестровое хранилище сохранены неожиданные токены: %+v", savedTokens)
	}
	if !savedTokens.AccessTokenExpiresAt.Equal(accessExpiresAt) {
		t.Fatalf("ожидался access expires at %s, получено %s", accessExpiresAt, savedTokens.AccessTokenExpiresAt)
	}
	if !savedTokens.RefreshTokenExpiresAt.Equal(refreshExpiresAt) {
		t.Fatalf("ожидался refresh expires at %s, получено %s", refreshExpiresAt, savedTokens.RefreshTokenExpiresAt)
	}
}

func TestAgentRegisterAndFetchTokensPendingApprovalDoesNotPersistTokens(t *testing.T) {
	t.Parallel()

	var saveCalls int
	agent := &Agent{
		cfg: config.Config{
			AgentProcessName: "XenionAgent",
			AgentType:        "sssruner",
			AgentVersion:     "1.2.3",
			BootstrapAPIKey:  "bootstrap-key",
			ServerURL:        "http://localhost:8080",
		},
		client: stubRuntimeClient{
			registerFn: func(_ context.Context, bootstrapAPIKey string, req protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
				if bootstrapAPIKey != "bootstrap-key" {
					t.Fatalf("ожидался bootstrap API key bootstrap-key, получено %q", bootstrapAPIKey)
				}
				return &protocol.AgentRegistrationResponseDTO{
					Status:    "pending_approval",
					Message:   "Регистрация ожидает подтверждения оператором",
					AgentUUID: req.AgentUUID,
				}, nil
			},
		},
		registryStore: stubRuntimeRegistryStore{
			collectRegistrationInfoFunc: func(string) map[string]interface{} {
				return map[string]interface{}{"os": "windows"}
			},
			saveTokensFn: func(state.Tokens) error {
				saveCalls++
				return nil
			},
		},
		identity:           &state.Identity{UUID: "agent-uuid"},
		machineFingerprint: "fingerprint-hash",
	}

	err := agent.registerAndFetchTokens(context.Background(), "ожидание подтверждения")
	if err == nil {
		t.Fatal("ожидалась ошибка pending approval")
	}
	var pendingErr *connectivity.PendingRegistrationError
	if !errors.As(err, &pendingErr) {
		t.Fatalf("ожидалась ошибка *PendingRegistrationError, получено %T", err)
	}
	if saveCalls != 0 {
		t.Fatalf("токены не должны сохраняться до подтверждения регистрации, получено %d сохранений", saveCalls)
	}
	if agent.tokens != nil {
		t.Fatalf("в агенте не должно быть токенов до подтверждения регистрации: %+v", agent.tokens)
	}
}

func TestAgentEnsureAuthLocked_Refresh403DoesNotFallbackToRegister(t *testing.T) {
	t.Parallel()

	var registerCalls int
	agent := &Agent{
		cfg: config.Config{
			AgentType:              "sssruner",
			AgentVersion:           "1.2.3",
			ServerURL:              "http://localhost:8080",
			AccessTokenGracePeriod: time.Minute,
		},
		client: stubRuntimeClient{
			refreshFn: func(_ context.Context, req protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error) {
				if req.AgentUUID != "agent-uuid" {
					t.Fatalf("ожидался AgentUUID agent-uuid, получено %q", req.AgentUUID)
				}
				if req.RefreshToken != "refresh-token" {
					t.Fatalf("ожидался refresh token refresh-token, получено %q", req.RefreshToken)
				}
				return nil, &client.HTTPError{StatusCode: 403, Body: "forbidden"}
			},
			registerFn: func(context.Context, string, protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
				registerCalls++
				return nil, errors.New("register не должен вызываться при 403 от refresh")
			},
		},
		identity: &state.Identity{UUID: "agent-uuid"},
		tokens: &state.Tokens{
			RefreshToken:          "refresh-token",
			RefreshTokenExpiresAt: time.Now().Add(time.Hour),
		},
	}

	operation, err := agent.ensureAuthLocked(context.Background())
	if err == nil {
		t.Fatal("ожидалась ошибка refresh 403")
	}
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("ожидалась HTTPError, получено %T", err)
	}
	if httpErr.StatusCode != 403 {
		t.Fatalf("ожидался статус 403, получено %d", httpErr.StatusCode)
	}
	if operation != connectivity.OpRefreshToken {
		t.Fatalf("ожидалась операция %s, получено %s", connectivity.OpRefreshToken, operation)
	}
	if registerCalls != 0 {
		t.Fatalf("register не должен вызываться, получено %d вызовов", registerCalls)
	}
}

func TestAgentCommunicateOnce_Register403SetsAuthorizationCooldown(t *testing.T) {
	t.Parallel()

	manager := newTestConnectivityManager(t)
	agent := &Agent{
		cfg: config.Config{
			AgentProcessName:       "XenionAgent",
			AgentType:              "sssruner",
			AgentVersion:           "1.2.3",
			BootstrapAPIKey:        "bootstrap-key",
			ServerURL:              "http://localhost:8080",
			HeartbeatInterval:      15 * time.Second,
			UpdateCheckInterval:    60 * time.Second,
			AccessTokenGracePeriod: time.Minute,
		},
		client: stubRuntimeClient{
			registerFn: func(context.Context, string, protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
				return nil, &client.HTTPError{StatusCode: 403, Body: "forbidden"}
			},
		},
		registryStore: stubRuntimeRegistryStore{
			loadTokensFn: func() (*state.Tokens, error) {
				return nil, nil
			},
			collectRegistrationInfoFunc: func(string) map[string]interface{} {
				return map[string]interface{}{
					"os":            "windows",
					"arch":          "amd64",
					"registry_path": `Software\MyHoreca\XenionAgent`,
				}
			},
		},
		identity:           &state.Identity{UUID: "agent-uuid"},
		machineFingerprint: "fingerprint-hash",
		inventoryService:   stubRuntimeInventoryService{},
		adapterManager:     stubRuntimeAdapterManager{},
		connectivity:       manager,
	}

	delay, err := agent.communicateOnce(context.Background())
	if err == nil {
		t.Fatal("ожидалась ошибка регистрации 403")
	}
	if delay != 24*time.Hour {
		t.Fatalf("ожидался cooldown 24h, получено %s", delay)
	}

	status := manager.Status()
	if status.State != connectivity.StateAuthorizationRejected {
		t.Fatalf("ожидалось состояние %s, получено %s", connectivity.StateAuthorizationRejected, status.State)
	}
	if status.ReasonKind != connectivity.ReasonHTTP403 {
		t.Fatalf("ожидалась причина %s, получено %s", connectivity.ReasonHTTP403, status.ReasonKind)
	}
}

func TestAgentCommunicateOnce_PendingApprovalDoesNotSendHeartbeat(t *testing.T) {
	t.Parallel()

	manager := newTestConnectivityManager(t)
	var sendHeartbeatCalls int
	agent := &Agent{
		cfg: config.Config{
			AgentProcessName:       "XenionAgent",
			AgentType:              "sssruner",
			AgentVersion:           "1.2.3",
			BootstrapAPIKey:        "bootstrap-key",
			ServerURL:              "http://localhost:8080",
			HeartbeatInterval:      15 * time.Second,
			UpdateCheckInterval:    60 * time.Second,
			AccessTokenGracePeriod: time.Minute,
			ConnectivityBaseRetry:  15 * time.Second,
		},
		client: stubRuntimeClient{
			registerFn: func(context.Context, string, protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error) {
				return &protocol.AgentRegistrationResponseDTO{
					Status:    "pending_approval",
					Message:   "Регистрация ожидает подтверждения оператором",
					AgentUUID: "agent-uuid",
				}, nil
			},
			sendHeartbeatFn: func(context.Context, string, protocol.AgentDataDTO, string) (*protocol.HeartbeatResponseDTO, error) {
				sendHeartbeatCalls++
				return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
			},
		},
		registryStore: stubRuntimeRegistryStore{
			loadTokensFn: func() (*state.Tokens, error) {
				return nil, nil
			},
			collectRegistrationInfoFunc: func(string) map[string]interface{} {
				return map[string]interface{}{"os": "windows"}
			},
		},
		identity:           &state.Identity{UUID: "agent-uuid"},
		machineFingerprint: "fingerprint-hash",
		inventoryService:   stubRuntimeInventoryService{},
		adapterManager:     stubRuntimeAdapterManager{},
		connectivity:       manager,
	}

	delay, err := agent.communicateOnce(context.Background())
	if err == nil {
		t.Fatal("ожидалась ошибка pending approval")
	}
	var pendingErr *connectivity.PendingRegistrationError
	if !errors.As(err, &pendingErr) {
		t.Fatalf("ожидалась ошибка *PendingRegistrationError, получено %T", err)
	}
	if delay != 15*time.Second {
		t.Fatalf("ожидался retry 15s, получено %s", delay)
	}
	if sendHeartbeatCalls != 0 {
		t.Fatalf("heartbeat не должен отправляться до подтверждения регистрации, получено %d вызовов", sendHeartbeatCalls)
	}

	status := manager.Status()
	if status.State != connectivity.StateRegistrationPendingApproval {
		t.Fatalf("ожидалось состояние %s, получено %s", connectivity.StateRegistrationPendingApproval, status.State)
	}
	if status.ReasonKind != connectivity.ReasonRegistrationPendingApproval {
		t.Fatalf("ожидалась причина %s, получено %s", connectivity.ReasonRegistrationPendingApproval, status.ReasonKind)
	}
}
