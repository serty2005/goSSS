package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"etalon-agent/internal/adapters"
	"etalon-agent/internal/client"
	"etalon-agent/internal/config"
	"etalon-agent/internal/connectivity"
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
	connectivity       *connectivity.Manager
	machineFingerprint string
	mu                 sync.Mutex
}

func NewAgent(cfg config.Config, cli *client.ServiceDeskClient) (*Agent, error) {
	if cli == nil {
		return nil, fmt.Errorf("не задан HTTP-клиент ServiceDesk")
	}
	connectivityManager, err := connectivity.NewManager(
		filepath.Join(cfg.DataDir, "connectivity_state.json"),
		connectivity.Policy{
			BaseRetry:             cfg.ConnectivityBaseRetry,
			MaxRetry:              cfg.ConnectivityMaxRetry,
			RegistrationCooldown:  cfg.RegistrationCooldown,
			AuthorizationCooldown: cfg.AuthorizationCooldown,
			RateLimitCooldown:     cfg.RateLimitCooldown,
			ConfigErrorCooldown:   cfg.ConfigErrorCooldown,
			ProtocolErrorCooldown: cfg.ProtocolErrorCooldown,
			RetryJitterFactor:     cfg.RetryJitterFactor,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("не удалось инициализировать manager связи: %w", err)
	}

	a := &Agent{
		cfg:              cfg,
		client:           cli,
		scheduler:        services.NewScheduler(),
		workflows:        make(map[string]workflow),
		registryStore:    state.NewRegistryStore(cfg.RegistryPath),
		inventoryService: inventory.NewService(cfg.InventoryInterval),
		adapterManager:   adapters.NewManager(cfg.AdapterDir, cli),
		connectivity:     connectivityManager,
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
	a.logRuntimeConfiguration()

	if err := a.bootstrapIdentity(); err != nil {
		return err
	}
	a.logRestoredConnectivityState()

	a.scheduler.AddTask("inventory-refresh", a.inventoryService.Interval(), func(ctx context.Context) {
		if _, err := a.inventoryService.CollectNow(ctx); err != nil {
			log.Printf("Ошибка обновления inventory: %v", err)
		}
	})

	var (
		wg           sync.WaitGroup
		schedulerErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := a.scheduler.Run(ctx); err != nil && ctx.Err() == nil {
			schedulerErr = fmt.Errorf("inventory-планировщик завершился с ошибкой: %w", err)
		}
	}()

	connectivityErr := a.runConnectivityLoop(ctx)
	wg.Wait()

	if connectivityErr != nil {
		return connectivityErr
	}
	return schedulerErr
}

func (a *Agent) bootstrapIdentity() error {
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
	return nil
}

func (a *Agent) ensureAuth(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, err := a.ensureAuthLocked(ctx)
	return err
}

func (a *Agent) ensureAuthLocked(ctx context.Context) (connectivity.Operation, error) {
	if a.tokens == nil {
		tokens, err := a.registryStore.LoadTokens()
		if err != nil {
			return connectivity.OpNone, err
		}
		a.tokens = tokens
	}

	now := time.Now()
	if a.tokens != nil && strings.TrimSpace(a.tokens.AccessToken) != "" && now.Add(a.cfg.AccessTokenGracePeriod).Before(a.tokens.AccessTokenExpiresAt) {
		return connectivity.OpNone, nil
	}

	if a.tokens != nil && strings.TrimSpace(a.tokens.RefreshToken) != "" && now.Before(a.tokens.RefreshTokenExpiresAt) {
		resp, err := a.client.RefreshTokens(ctx, protocol.AgentTokenRefreshRequestDTO{
			AgentUUID:    a.identity.UUID,
			RefreshToken: a.tokens.RefreshToken,
		})
		if err == nil {
			return connectivity.OpRefreshToken, a.applyTokenRefreshResponse(*resp)
		}
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
			log.Printf(
				"Refresh token отклонен сервером: agent_uuid=%s endpoint=%s status_code=401. Перехожу к bootstrap-регистрации.",
				a.identity.UUID,
				a.refreshEndpoint(),
			)
		} else {
			return connectivity.OpRefreshToken, err
		}
	}

	registrationReason := "access token отсутствует или просрочен"
	switch {
	case a.tokens == nil:
		registrationReason = "локальные токены агента отсутствуют"
	case strings.TrimSpace(a.tokens.RefreshToken) == "":
		registrationReason = "локальный refresh token отсутствует"
	case !now.Before(a.tokens.RefreshTokenExpiresAt):
		registrationReason = fmt.Sprintf("локальный refresh token просрочен (%s)", a.tokens.RefreshTokenExpiresAt.Format(time.RFC3339))
	}

	if err := a.registerAndFetchTokens(ctx, registrationReason); err != nil {
		return connectivity.OpRegister, err
	}
	return connectivity.OpRegister, nil
}

func (a *Agent) registerAndFetchTokens(ctx context.Context, reason string) error {
	req := a.buildRegistrationRequest(time.Now())
	a.logRegistrationAttempt(reason, req)
	resp, err := a.client.Register(ctx, a.cfg.BootstrapAPIKey, req)
	if err != nil {
		a.logRegistrationFailure(reason, req, err)
		return err
	}
	log.Printf("Bootstrap-регистрация завершена: agent_uuid=%s status=%s", req.AgentUUID, resp.Status)
	if err := a.applyRegistrationResponse(*resp); err != nil {
		var pendingErr *connectivity.PendingRegistrationError
		if errors.As(err, &pendingErr) {
			log.Printf(
				"Bootstrap-регистрация ожидает подтверждения оператора: agent_uuid=%s endpoint=%s message=%q",
				req.AgentUUID,
				a.registerEndpoint(),
				pendingErr.Error(),
			)
		}
		return err
	}
	return nil
}

func (a *Agent) applyRegistrationResponse(resp protocol.AgentRegistrationResponseDTO) error {
	status := strings.ToLower(strings.TrimSpace(resp.Status))
	switch status {
	case "", "ok":
	case "pending_approval":
		message := strings.TrimSpace(resp.Message)
		if message == "" {
			message = "Регистрация ожидает подтверждения оператором"
		}
		return &connectivity.PendingRegistrationError{Message: message}
	default:
		if message := strings.TrimSpace(resp.Message); message != "" {
			return fmt.Errorf("сервер вернул неподдерживаемый статус регистрации %q: %s", resp.Status, message)
		}
		return fmt.Errorf("сервер вернул неподдерживаемый статус регистрации %q", resp.Status)
	}

	if err := validateIssuedTokens(resp.AccessToken, resp.RefreshToken, resp.AccessTokenExpiresAt, resp.RefreshTokenExpiresAt); err != nil {
		return err
	}
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

func (a *Agent) buildRegistrationRequest(now time.Time) protocol.RegistrationRequestDTO {
	host := a.hostname()
	initialData := protocol.AgentDataDTO{
		Hostname:     host,
		CurrentTime:  now.Format(time.RFC3339),
		AgentUUID:    a.identity.UUID,
		AgentType:    a.cfg.AgentType,
		AgentVersion: a.cfg.AgentVersion,
	}
	a.attachInventoryData(&initialData)

	return protocol.RegistrationRequestDTO{
		AgentUUID:          a.identity.UUID,
		Hostname:           host,
		AgentVersion:       a.cfg.AgentVersion,
		MachineFingerprint: a.machineFingerprint,
		InitialData:        initialData,
		SystemInfo:         a.registryStore.CollectRegistrationSystemInfo(a.cfg.AgentProcessName),
	}
}

func (a *Agent) logRuntimeConfiguration() {
	log.Printf(
		"Локальная конфигурация агента: source=%s server_url=%s agent_type=%s registry_path=HKLM\\%s data_dir=%s adapter_dir=%s heartbeat_interval=%s inventory_interval=%s update_check_interval=%s connectivity_state_file=%s connectivity_base_retry=%s connectivity_max_retry=%s authorization_cooldown=%s",
		a.cfg.ConfigSource,
		a.cfg.ServerURL,
		a.cfg.AgentType,
		a.cfg.RegistryPath,
		a.cfg.DataDir,
		a.cfg.AdapterDir,
		a.cfg.HeartbeatInterval,
		a.cfg.InventoryInterval,
		a.cfg.UpdateCheckInterval,
		a.connectivity.Path(),
		a.cfg.ConnectivityBaseRetry,
		a.cfg.ConnectivityMaxRetry,
		a.cfg.AuthorizationCooldown,
	)
}

func (a *Agent) logRegistrationAttempt(reason string, req protocol.RegistrationRequestDTO) {
	log.Printf(
		"Старт bootstrap-регистрации: reason=%s endpoint=%s/api/agents/register auth=Authorization: Bearer <bootstrap_api_key> registry_path=HKLM\\%s payload=%s",
		strings.TrimSpace(reason),
		strings.TrimRight(a.cfg.ServerURL, "/"),
		a.cfg.RegistryPath,
		marshalLogJSON(req),
	)
}

func (a *Agent) logRegistrationFailure(reason string, req protocol.RegistrationRequestDTO, err error) {
	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode == 401 {
			log.Printf(
				"Bootstrap-регистрация отклонена сервером: reason=%s status_code=%d agent_uuid=%s endpoint=%s/api/agents/register response_body=%q. Проверь соответствие BootstrapAPIKey агента и AGENT_API_KEY сервера.",
				strings.TrimSpace(reason),
				httpErr.StatusCode,
				req.AgentUUID,
				strings.TrimRight(a.cfg.ServerURL, "/"),
				strings.TrimSpace(httpErr.Body),
			)
			return
		}
		log.Printf(
			"Bootstrap-регистрация завершилась HTTP-ошибкой: reason=%s status_code=%d agent_uuid=%s endpoint=%s/api/agents/register response_body=%q",
			strings.TrimSpace(reason),
			httpErr.StatusCode,
			req.AgentUUID,
			strings.TrimRight(a.cfg.ServerURL, "/"),
			strings.TrimSpace(httpErr.Body),
		)
		return
	}

	log.Printf(
		"Bootstrap-регистрация завершилась ошибкой: reason=%s agent_uuid=%s endpoint=%s/api/agents/register error=%v",
		strings.TrimSpace(reason),
		req.AgentUUID,
		strings.TrimRight(a.cfg.ServerURL, "/"),
		err,
	)
}

func marshalLogJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	return string(raw)
}

func (a *Agent) runConnectivityLoop(ctx context.Context) error {
	initialDelay := a.connectivity.WaitBeforeAttempt()
	log.Printf(
		"Контур связи запущен: regular_interval=%s state_file=%s",
		a.regularConnectivityInterval(),
		a.connectivity.Path(),
	)

	timer := time.NewTimer(initialDelay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Контур связи остановлен")
			return nil
		case <-timer.C:
			nextDelay, err := a.communicateOnce(ctx)
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				timer.Reset(nextDelay)
				continue
			}
			timer.Reset(nextDelay)
		}
	}
}

func (a *Agent) communicateOnce(ctx context.Context) (time.Duration, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.communicateLocked(ctx)
}

func (a *Agent) communicateLocked(ctx context.Context) (time.Duration, error) {
	authOp, err := a.ensureAuthLocked(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return a.recordConnectivityFailure(authOp, a.operationEndpoint(authOp), err), err
	}

	payload, err := a.buildHeartbeatPayload()
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return a.recordConnectivityFailure(connectivity.OpHeartbeat, a.dataEndpoint(), err), err
	}

	resp, err := a.client.SendHeartbeat(ctx, a.identity.UUID, payload, a.tokens.AccessToken)
	if err != nil {
		var httpErr *client.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == 401 {
			log.Printf(
				"Heartbeat отклонен сервером: status_code=401 agent_uuid=%s endpoint=%s. Пытаюсь восстановить авторизацию.",
				a.identity.UUID,
				a.dataEndpoint(),
			)
			a.tokens.AccessTokenExpiresAt = time.Time{}
			authOp, err = a.ensureAuthLocked(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return 0, ctx.Err()
				}
				return a.recordConnectivityFailure(authOp, a.operationEndpoint(authOp), err), err
			}
			payload, err = a.buildHeartbeatPayload()
			if err != nil {
				if ctx.Err() != nil {
					return 0, ctx.Err()
				}
				return a.recordConnectivityFailure(connectivity.OpHeartbeat, a.dataEndpoint(), err), err
			}
			resp, err = a.client.SendHeartbeat(ctx, a.identity.UUID, payload, a.tokens.AccessToken)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return a.recordConnectivityFailure(connectivity.OpHeartbeat, a.dataEndpoint(), err), err
	}

	update, saveErr := a.connectivity.RecordSuccess(connectivity.OpHeartbeat, a.dataEndpoint())
	if saveErr != nil {
		log.Printf("Не удалось сохранить состояние связи: %v", saveErr)
	}
	if update.Previous.State != connectivity.StateStarting && update.Previous.State != connectivity.StateOnline {
		log.Printf(
			"Связь с сервером восстановлена: previous_state=%s previous_reason=%s endpoint=%s previous_failures=%d",
			update.Previous.State,
			update.Previous.ReasonKind,
			update.Current.Endpoint,
			update.Previous.ConsecutiveFailures,
		)
	}

	log.Printf(
		"Heartbeat отправлен: status=%s tasks=%d manifests=%d next_attempt_in=%s",
		resp.Status,
		len(resp.Tasks),
		len(resp.AdapterManifests),
		a.regularConnectivityInterval(),
	)
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
	return a.regularConnectivityInterval(), nil
}

func (a *Agent) recordConnectivityFailure(op connectivity.Operation, endpoint string, err error) time.Duration {
	update, retryAfter, saveErr := a.connectivity.RecordFailure(op, endpoint, err)
	if saveErr != nil {
		log.Printf("Не удалось сохранить состояние связи: %v", saveErr)
	}

	hint := ""
	switch update.Current.State {
	case connectivity.StateAuthorizationRejected:
		hint = " hint=\"авторизация отклонена сервером; повтор будет выполнен через 24 часа\""
	case connectivity.StateRegistrationRejected:
		hint = " hint=\"проверь bootstrap API key и маршрут /api/agents/register\""
	case connectivity.StateRegistrationPendingApproval:
		hint = " hint=\"регистрация ожидает подтверждения оператором; heartbeat не будет отправляться до выдачи токенов\""
	case connectivity.StateDNSFailed:
		hint = " hint=\"проверь DNS и значение server_url\""
	case connectivity.StateNoNetwork:
		hint = " hint=\"проверь локальную сеть и маршрутизацию\""
	}

	log.Printf(
		"Связь с сервером недоступна: operation=%s state=%s reason_kind=%s http_status=%d consecutive_failures=%d next_retry_in=%s endpoint=%s detail=%q error=%v%s",
		op,
		update.Current.State,
		update.Current.ReasonKind,
		update.Current.HTTPStatusCode,
		update.Current.ConsecutiveFailures,
		retryAfter,
		update.Current.Endpoint,
		update.Current.ReasonDetail,
		err,
		hint,
	)
	return retryAfter
}

func (a *Agent) regularConnectivityInterval() time.Duration {
	interval := a.cfg.HeartbeatInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if a.cfg.UpdateCheckInterval > 0 && a.cfg.UpdateCheckInterval < interval {
		return a.cfg.UpdateCheckInterval
	}
	return interval
}

func (a *Agent) logRestoredConnectivityState() {
	status := a.connectivity.Status()
	wait := a.connectivity.WaitBeforeAttempt()
	if status.State == connectivity.StateStarting && status.ConsecutiveFailures == 0 {
		return
	}

	if wait > 0 {
		log.Printf(
			"Восстановлено состояние связи из локального файла: state=%s reason_kind=%s consecutive_failures=%d next_retry_in=%s endpoint=%s",
			status.State,
			status.ReasonKind,
			status.ConsecutiveFailures,
			wait,
			status.Endpoint,
		)
		return
	}

	log.Printf(
		"Восстановлено состояние связи из локального файла: state=%s reason_kind=%s consecutive_failures=%d endpoint=%s",
		status.State,
		status.ReasonKind,
		status.ConsecutiveFailures,
		status.Endpoint,
	)
}

func (a *Agent) registerEndpoint() string {
	return strings.TrimRight(a.cfg.ServerURL, "/") + "/api/agents/register"
}

func (a *Agent) refreshEndpoint() string {
	return strings.TrimRight(a.cfg.ServerURL, "/") + "/api/agents/auth/refresh"
}

func (a *Agent) dataEndpoint() string {
	agentUUID := ""
	if a.identity != nil {
		agentUUID = a.identity.UUID
	}
	return strings.TrimRight(a.cfg.ServerURL, "/") + "/api/agents/" + agentUUID + "/data"
}

func (a *Agent) operationEndpoint(op connectivity.Operation) string {
	switch op {
	case connectivity.OpRegister:
		return a.registerEndpoint()
	case connectivity.OpRefreshToken:
		return a.refreshEndpoint()
	case connectivity.OpHeartbeat:
		return a.dataEndpoint()
	default:
		return strings.TrimRight(a.cfg.ServerURL, "/")
	}
}

func (a *Agent) applyTokenRefreshResponse(resp protocol.AgentTokenRefreshResponseDTO) error {
	if err := validateIssuedTokens(resp.AccessToken, resp.RefreshToken, resp.AccessTokenExpiresAt, resp.RefreshTokenExpiresAt); err != nil {
		return err
	}
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

func validateIssuedTokens(accessToken, refreshToken string, accessExpiresAt, refreshExpiresAt time.Time) error {
	switch {
	case strings.TrimSpace(accessToken) == "":
		return fmt.Errorf("сервер вернул пустой access token агента")
	case strings.TrimSpace(refreshToken) == "":
		return fmt.Errorf("сервер вернул пустой refresh token агента")
	case accessExpiresAt.IsZero():
		return fmt.Errorf("сервер вернул access token без срока действия")
	case refreshExpiresAt.IsZero():
		return fmt.Errorf("сервер вернул refresh token без срока действия")
	default:
		return nil
	}
}

func (a *Agent) heartbeat(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.heartbeatLocked(ctx)
}

func (a *Agent) heartbeatLocked(ctx context.Context) error {
	if _, err := a.ensureAuthLocked(ctx); err != nil {
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
			if _, authErr := a.ensureAuthLocked(ctx); authErr != nil {
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
	a.attachInventoryData(&payload)
	return payload, nil
}

func (a *Agent) attachInventoryData(payload *protocol.AgentDataDTO) {
	if payload == nil || a.inventoryService == nil {
		return
	}

	snapshot, ok := a.inventoryService.Snapshot()
	if !ok {
		return
	}

	payload.Inventory = &snapshot
	if snapshot.HostInfo == nil {
		return
	}

	payload.URLRms = strings.TrimSpace(snapshot.HostInfo.CashServerURL)
	payload.TeamviewerID = strings.TrimSpace(snapshot.HostInfo.TeamviewerID)
	payload.AnydeskID = strings.TrimSpace(snapshot.HostInfo.AnydeskID)
	payload.LitemanagerID = strings.TrimSpace(snapshot.HostInfo.LitemanagerID)
	payload.RustdeskID = strings.TrimSpace(snapshot.HostInfo.RustdeskID)
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
