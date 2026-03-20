package runtime

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/inventory"
	"etalon-agent/internal/protocol"
	"etalon-agent/internal/services"
	"etalon-agent/internal/state"
	"etalon-agent/internal/updater"
	"etalon-agent/internal/workflows"

	"github.com/google/uuid"
)

type workflow interface {
	Type() string
	Run(ctx context.Context, payload []byte) error
}

type serviceDeskClient interface {
	Register(context.Context, string, protocol.RegistrationRequestDTO) (*protocol.AgentRegistrationResponseDTO, error)
	RefreshTokens(context.Context, protocol.AgentTokenRefreshRequestDTO) (*protocol.AgentTokenRefreshResponseDTO, error)
	SendHeartbeat(context.Context, string, protocol.AgentDataDTO, string) (*protocol.HeartbeatResponseDTO, error)
}

type registryStore interface {
	EnsureIdentity(string, func() (string, error)) (*state.Identity, error)
	LoadTokens() (*state.Tokens, error)
	SaveTokens(state.Tokens) error
	CollectRegistrationSystemInfo(string) map[string]interface{}
}

type inventoryService interface {
	Interval() time.Duration
	CollectNow(context.Context) (inventory.Snapshot, error)
	Snapshot() (inventory.Snapshot, bool)
}

type adapterManager interface {
	EnsureLayout() error
	ListStatuses() ([]adapters.Status, error)
	Sync(context.Context, []adapters.ManifestItem) ([]adapters.Status, error)
}

type Agent struct {
	cfg                config.Config
	client             serviceDeskClient
	scheduler          *services.Scheduler
	workflows          map[string]workflow
	registryStore      registryStore
	identity           *state.Identity
	tokens             *state.Tokens
	inventoryService   inventoryService
	adapterManager     adapterManager
	machineFingerprint string
	mu                 sync.Mutex
}

func NewAgent(cfg config.Config, cli *client.ServiceDeskClient) (*Agent, error) {
	if cli == nil {
		return nil, fmt.Errorf("не задан HTTP-клиент ServiceDesk")
	}

	a := &Agent{
		cfg:              cfg,
		client:           cli,
		scheduler:        services.NewScheduler(),
		workflows:        make(map[string]workflow),
		registryStore:    state.NewRegistryStore(cfg.RegistryPath),
		inventoryService: inventory.NewService(cfg.InventoryInterval),
		adapterManager:   adapters.NewManager(cfg.AdapterDir, cli),
	}
	a.registerWorkflow(workflows.NewSelfUpdateWorkflow(cfg.AgentVersion, updater.NewService(cfg.DataDir, cli)))
	return a, nil
}

func (a *Agent) Run(ctx context.Context) error {
	if err := a.adapterManager.EnsureLayout(); err != nil {
		return fmt.Errorf("не удалось подготовить каталог адаптеров: %w", err)
	}
	if _, err := a.inventoryService.CollectNow(ctx); err != nil {
		log.Printf("Первичный inventory не собран: %v", err)
	}

	if err := a.bootstrapIdentityAndTokens(ctx); err != nil {
		return err
	}

	a.scheduler.AddTask("inventory-refresh", a.inventoryService.Interval(), func(ctx context.Context) {
		if _, err := a.inventoryService.CollectNow(ctx); err != nil {
			log.Printf("Ошибка обновления inventory: %v", err)
		}
	})

	a.scheduler.AddTask("heartbeat", a.cfg.HeartbeatInterval, func(ctx context.Context) {
		if err := a.heartbeat(ctx); err != nil {
			log.Printf("Ошибка heartbeat: %v", err)
		}
	})

	if a.cfg.UpdateCheckInterval != a.cfg.HeartbeatInterval {
		a.scheduler.AddTask("update-poll", a.cfg.UpdateCheckInterval, func(ctx context.Context) {
			if err := a.heartbeat(ctx); err != nil {
				log.Printf("Ошибка проверки обновления через heartbeat: %v", err)
			}
		})
	}

	return a.scheduler.Run(ctx)
}

func (a *Agent) bootstrapIdentityAndTokens(ctx context.Context) error {
	fpHash, err := state.ComputeMachineFingerprintHash()
	if err != nil {
		return fmt.Errorf("не удалось вычислить fingerprint машины: %w", err)
	}
	a.machineFingerprint = fpHash

	identity, err := a.registryStore.EnsureIdentity(fpHash, func() (string, error) {
		return uuid.NewString(), nil
	})
	if err != nil {
		return fmt.Errorf("не удалось подготовить identity агента в реестре: %w", err)
	}
	a.identity = identity

	if identity.ResetPerformed {
		log.Printf("Fingerprint машины изменился, выполнен сброс identity и токенов, agent_uuid=%s", identity.UUID)
	}

	if err := a.ensureAuth(ctx); err != nil {
		return fmt.Errorf("не удалось получить токены агента: %w", err)
	}
	return nil
}

func (a *Agent) ensureAuth(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ensureAuthLocked(ctx)
}

func (a *Agent) ensureAuthLocked(ctx context.Context) error {
	if a.tokens == nil {
		tokens, err := a.registryStore.LoadTokens()
		if err != nil {
			return err
		}
		a.tokens = tokens
	}

	now := time.Now()
	if a.tokens != nil && strings.TrimSpace(a.tokens.AccessToken) != "" && now.Add(a.cfg.AccessTokenGracePeriod).Before(a.tokens.AccessTokenExpiresAt) {
		return nil
	}

	if a.tokens != nil && strings.TrimSpace(a.tokens.RefreshToken) != "" && now.Before(a.tokens.RefreshTokenExpiresAt) {
		resp, err := a.client.RefreshTokens(ctx, protocol.AgentTokenRefreshRequestDTO{
			AgentUUID:    a.identity.UUID,
			RefreshToken: a.tokens.RefreshToken,
		})
		if err == nil {
			return a.applyTokenRefreshResponse(*resp)
		}
		log.Printf("Не удалось обновить токены через refresh, выполняю bootstrap-регистрацию: %v", err)
	}

	return a.registerAndFetchTokens(ctx)
}

func (a *Agent) registerAndFetchTokens(ctx context.Context) error {
	host := a.hostname()
	req := protocol.RegistrationRequestDTO{
		AgentUUID:          a.identity.UUID,
		Hostname:           host,
		AgentVersion:       a.cfg.AgentVersion,
		MachineFingerprint: a.machineFingerprint,
		InitialData: protocol.AgentDataDTO{
			Hostname:     host,
			CurrentTime:  time.Now().Format(time.RFC3339),
			AgentUUID:    a.identity.UUID,
			AgentType:    a.cfg.AgentType,
			AgentVersion: a.cfg.AgentVersion,
		},
		SystemInfo: a.registryStore.CollectRegistrationSystemInfo(a.cfg.AgentProcessName),
	}
	resp, err := a.client.Register(ctx, a.cfg.BootstrapAPIKey, req)
	if err != nil {
		return err
	}
	return a.applyRegistrationResponse(*resp)
}

func (a *Agent) applyRegistrationResponse(resp protocol.AgentRegistrationResponseDTO) error {
	a.tokens = &state.Tokens{
		AccessToken:           resp.AccessToken,
		RefreshToken:          resp.RefreshToken,
		AccessTokenExpiresAt:  resp.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: resp.RefreshTokenExpiresAt,
		LastTokenRefreshAt:    time.Now(),
	}
	if err := a.registryStore.SaveTokens(*a.tokens); err != nil {
		return fmt.Errorf("не удалось сохранить токены агента в реестре: %w", err)
	}
	log.Printf("Получены токены агента (access до %s)", resp.AccessTokenExpiresAt.Format(time.RFC3339))
	return nil
}

func (a *Agent) applyTokenRefreshResponse(resp protocol.AgentTokenRefreshResponseDTO) error {
	a.tokens = &state.Tokens{
		AccessToken:           resp.AccessToken,
		RefreshToken:          resp.RefreshToken,
		AccessTokenExpiresAt:  resp.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: resp.RefreshTokenExpiresAt,
		LastTokenRefreshAt:    time.Now(),
	}
	if err := a.registryStore.SaveTokens(*a.tokens); err != nil {
		return fmt.Errorf("не удалось сохранить обновленные токены в реестре: %w", err)
	}
	log.Printf("Токены агента обновлены (access до %s)", resp.AccessTokenExpiresAt.Format(time.RFC3339))
	return nil
}

func (a *Agent) heartbeat(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.heartbeatLocked(ctx)
}

func (a *Agent) heartbeatLocked(ctx context.Context) error {
	if err := a.ensureAuthLocked(ctx); err != nil {
		return err
	}

	payload, err := a.buildHeartbeatPayload()
	if err != nil {
		return err
	}

	resp, err := a.client.SendHeartbeat(ctx, a.identity.UUID, payload, a.tokens.AccessToken)
	if err != nil {
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
			log.Printf("Access token отклонен сервером, пытаюсь обновить токены")
			a.tokens.AccessTokenExpiresAt = time.Time{}
			if authErr := a.ensureAuthLocked(ctx); authErr != nil {
				return fmt.Errorf("не удалось восстановить авторизацию агента: %w", authErr)
			}
			payload, err = a.buildHeartbeatPayload()
			if err != nil {
				return err
			}
			resp, err = a.client.SendHeartbeat(ctx, a.identity.UUID, payload, a.tokens.AccessToken)
		}
	}
	if err != nil {
		return err
	}

	log.Printf("Heartbeat отправлен: status=%s tasks=%d manifests=%d", resp.Status, len(resp.Tasks), len(resp.AdapterManifests))
	for _, task := range resp.Tasks {
		if err := a.executeTask(ctx, task); err != nil {
			log.Printf("Задача id=%d type=%s завершилась с ошибкой: %v", task.ID, task.Type, err)
		}
	}
	if len(resp.AdapterManifests) > 0 {
		if _, err := a.adapterManager.Sync(ctx, resp.AdapterManifests); err != nil {
			log.Printf("Синхронизация адаптеров завершилась с ошибкой: %v", err)
		}
	}
	return nil
}

func (a *Agent) buildHeartbeatPayload() (protocol.AgentDataDTO, error) {
	adapterStatuses, err := a.adapterManager.ListStatuses()
	if err != nil {
		return protocol.AgentDataDTO{}, fmt.Errorf("не удалось получить статусы адаптеров: %w", err)
	}

	payload := protocol.AgentDataDTO{
		Hostname:        a.hostname(),
		CurrentTime:     time.Now().Format(time.RFC3339),
		AgentVersion:    a.cfg.AgentVersion,
		AgentUUID:       a.identity.UUID,
		AgentType:       a.cfg.AgentType,
		AdapterStatuses: adapterStatuses,
	}
	if snapshot, ok := a.inventoryService.Snapshot(); ok {
		payload.Inventory = &snapshot
	}
	return payload, nil
}

func (a *Agent) executeTask(ctx context.Context, task protocol.AgentTaskDTO) error {
	wf, ok := a.workflows[task.Type]
	if !ok {
		log.Printf("Неподдерживаемая задача от сервера: id=%d type=%s", task.ID, task.Type)
		return nil
	}
	log.Printf("Выполнение задачи id=%d type=%s", task.ID, task.Type)
	return wf.Run(ctx, task.Payload)
}

func (a *Agent) registerWorkflow(w workflow) {
	a.workflows[w.Type()] = w
}

func (a *Agent) hostname() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return "unknown-host"
	}
	return host
}
