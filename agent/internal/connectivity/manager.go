package connectivity

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"etalon-agent/internal/client"
)

type Operation string

const (
	OpNone         Operation = "none"
	OpRegister     Operation = "register"
	OpRefreshToken Operation = "refresh_token"
	OpHeartbeat    Operation = "heartbeat"
)

type State string

const (
	StateStarting                    State = "starting"
	StateOnline                      State = "online"
	StateDegraded                    State = "degraded"
	StateNoNetwork                   State = "no_network"
	StateDNSFailed                   State = "dns_failed"
	StateServerUnreachable           State = "server_unreachable"
	StateRegistrationRejected        State = "registration_rejected"
	StateRegistrationPendingApproval State = "registration_pending_approval"
	StateAuthorizationRejected       State = "authorization_rejected"
	StateRegisterRequired            State = "register_required"
	StateTokenRefreshFailed          State = "token_refresh_failed"
	StateServerError                 State = "server_error"
	StateRateLimited                 State = "rate_limited"
	StateConfigError                 State = "config_error"
	StateProtocolError               State = "protocol_error"
)

type ReasonKind string

const (
	ReasonNone                        ReasonKind = "none"
	ReasonRegistrationPendingApproval ReasonKind = "registration_pending_approval"
	ReasonHTTP401                     ReasonKind = "http_401"
	ReasonHTTP403                     ReasonKind = "http_403"
	ReasonHTTP404                     ReasonKind = "http_404"
	ReasonHTTP409                     ReasonKind = "http_409"
	ReasonHTTP429                     ReasonKind = "http_429"
	ReasonHTTP5xx                     ReasonKind = "http_5xx"
	ReasonHTTPUnexpected              ReasonKind = "http_unexpected"
	ReasonTimeout                     ReasonKind = "timeout"
	ReasonDNS                         ReasonKind = "dns"
	ReasonConnectRefused              ReasonKind = "connect_refused"
	ReasonNoNetwork                   ReasonKind = "no_network"
	ReasonTLS                         ReasonKind = "tls"
	ReasonRequestBuild                ReasonKind = "request_build"
	ReasonRequestEncode               ReasonKind = "request_encode"
	ReasonResponseDecode              ReasonKind = "response_decode"
	ReasonTransport                   ReasonKind = "transport"
	ReasonUnknown                     ReasonKind = "unknown"
)

type Status struct {
	State                State      `json:"state"`
	LastOperation        Operation  `json:"last_operation"`
	ReasonKind           ReasonKind `json:"reason_kind"`
	ReasonDetail         string     `json:"reason_detail"`
	HTTPStatusCode       int        `json:"http_status_code"`
	ConsecutiveFailures  int        `json:"consecutive_failures"`
	LastSuccessAt        time.Time  `json:"last_success_at"`
	LastFailureAt        time.Time  `json:"last_failure_at"`
	NextAllowedAttemptAt time.Time  `json:"next_allowed_attempt_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	Endpoint             string     `json:"endpoint"`
}

type Update struct {
	Previous Status
	Current  Status
}

func (u Update) StateChanged() bool {
	return u.Previous.State != u.Current.State ||
		u.Previous.ReasonKind != u.Current.ReasonKind ||
		u.Previous.HTTPStatusCode != u.Current.HTTPStatusCode ||
		u.Previous.Endpoint != u.Current.Endpoint ||
		u.Previous.LastOperation != u.Current.LastOperation
}

type Policy struct {
	BaseRetry             time.Duration
	MaxRetry              time.Duration
	RegistrationCooldown  time.Duration
	AuthorizationCooldown time.Duration
	RateLimitCooldown     time.Duration
	ConfigErrorCooldown   time.Duration
	ProtocolErrorCooldown time.Duration
	RetryJitterFactor     float64
}

type Manager struct {
	mu     sync.Mutex
	policy Policy
	path   string
	now    func() time.Time
	rnd    *rand.Rand
	status Status
}

type retryMode int

const (
	retryExponential retryMode = iota
	retryFixed
)

type assessment struct {
	state          State
	reason         ReasonKind
	detail         string
	httpStatusCode int
	retryAfter     time.Duration
	mode           retryMode
}

type PendingRegistrationError struct {
	Message string
}

func (e *PendingRegistrationError) Error() string {
	if strings.TrimSpace(e.Message) != "" {
		return strings.TrimSpace(e.Message)
	}
	return "регистрация агента ожидает подтверждения оператором"
}

func NewManager(path string, policy Policy) (*Manager, error) {
	manager := &Manager{
		policy: normalizePolicy(policy),
		path:   filepath.Clean(strings.TrimSpace(path)),
		now:    time.Now,
		rnd:    rand.New(rand.NewSource(time.Now().UnixNano())),
		status: Status{
			State:         StateStarting,
			LastOperation: OpNone,
			ReasonKind:    ReasonNone,
		},
	}
	manager.load()
	return manager, nil
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) WaitBeforeAttempt() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	if !m.status.NextAllowedAttemptAt.After(now) {
		return 0
	}
	return m.status.NextAllowedAttemptAt.Sub(now)
}

func (m *Manager) RecordSuccess(op Operation, endpoint string) (Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	previous := m.status
	m.status = Status{
		State:                StateOnline,
		LastOperation:        op,
		ReasonKind:           ReasonNone,
		ReasonDetail:         "",
		HTTPStatusCode:       0,
		ConsecutiveFailures:  0,
		LastSuccessAt:        now,
		LastFailureAt:        previous.LastFailureAt,
		NextAllowedAttemptAt: now,
		UpdatedAt:            now,
		Endpoint:             strings.TrimSpace(endpoint),
	}

	err := m.saveLocked()
	return Update{Previous: previous, Current: m.status}, err
}

func (m *Manager) RecordFailure(op Operation, endpoint string, err error) (Update, time.Duration, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := m.now()
	previous := m.status
	failures := previous.ConsecutiveFailures + 1
	info := m.classifyFailure(op, err)

	retryAfter := info.retryAfter
	if info.mode == retryExponential {
		retryAfter = m.exponentialBackoff(failures)
	}

	m.status = Status{
		State:                info.state,
		LastOperation:        op,
		ReasonKind:           info.reason,
		ReasonDetail:         strings.TrimSpace(info.detail),
		HTTPStatusCode:       info.httpStatusCode,
		ConsecutiveFailures:  failures,
		LastSuccessAt:        previous.LastSuccessAt,
		LastFailureAt:        now,
		NextAllowedAttemptAt: now.Add(retryAfter),
		UpdatedAt:            now,
		Endpoint:             strings.TrimSpace(endpoint),
	}

	saveErr := m.saveLocked()
	return Update{Previous: previous, Current: m.status}, retryAfter, saveErr
}

func normalizePolicy(policy Policy) Policy {
	if policy.BaseRetry <= 0 {
		policy.BaseRetry = 15 * time.Second
	}
	if policy.MaxRetry <= 0 {
		policy.MaxRetry = 10 * time.Minute
	}
	if policy.MaxRetry < policy.BaseRetry {
		policy.MaxRetry = policy.BaseRetry
	}
	if policy.RegistrationCooldown <= 0 {
		policy.RegistrationCooldown = 30 * time.Minute
	}
	if policy.AuthorizationCooldown <= 0 {
		policy.AuthorizationCooldown = 24 * time.Hour
	}
	if policy.RateLimitCooldown <= 0 {
		policy.RateLimitCooldown = 5 * time.Minute
	}
	if policy.ConfigErrorCooldown <= 0 {
		policy.ConfigErrorCooldown = time.Hour
	}
	if policy.ProtocolErrorCooldown <= 0 {
		policy.ProtocolErrorCooldown = 30 * time.Minute
	}
	if policy.RetryJitterFactor < 0 {
		policy.RetryJitterFactor = 0
	}
	if policy.RetryJitterFactor > 0.9 {
		policy.RetryJitterFactor = 0.9
	}
	return policy
}

func (m *Manager) load() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.path == "" {
		return
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return
	}

	var status Status
	if err := json.Unmarshal(raw, &status); err != nil {
		return
	}
	if strings.TrimSpace(string(status.State)) == "" {
		return
	}
	m.status = status
}

func (m *Manager) saveLocked() error {
	if m.path == "" {
		return nil
	}
	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(m.status, "", "  ")
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp(dir, "connectivity-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(raw); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	_ = os.Remove(m.path)
	return os.Rename(tmpPath, m.path)
}

func (m *Manager) classifyFailure(op Operation, err error) assessment {
	if err == nil {
		return assessment{state: StateOnline, reason: ReasonNone, mode: retryFixed}
	}

	var pendingErr *PendingRegistrationError
	if errors.As(err, &pendingErr) {
		return assessment{
			state:      StateRegistrationPendingApproval,
			reason:     ReasonRegistrationPendingApproval,
			detail:     pendingErr.Error(),
			retryAfter: m.policy.BaseRetry,
			mode:       retryFixed,
		}
	}

	var httpErr *client.HTTPError
	if errors.As(err, &httpErr) {
		return m.classifyHTTPFailure(op, httpErr)
	}

	var buildErr *client.RequestBuildError
	if errors.As(err, &buildErr) {
		return assessment{
			state:      StateConfigError,
			reason:     ReasonRequestBuild,
			detail:     buildErr.Error(),
			retryAfter: m.policy.ConfigErrorCooldown,
			mode:       retryFixed,
		}
	}

	var encodeErr *client.RequestEncodeError
	if errors.As(err, &encodeErr) {
		return assessment{
			state:      StateProtocolError,
			reason:     ReasonRequestEncode,
			detail:     encodeErr.Error(),
			retryAfter: m.policy.ProtocolErrorCooldown,
			mode:       retryFixed,
		}
	}

	var decodeErr *client.ResponseDecodeError
	if errors.As(err, &decodeErr) {
		return assessment{
			state:      StateProtocolError,
			reason:     ReasonResponseDecode,
			detail:     decodeErr.Error(),
			retryAfter: m.policy.ProtocolErrorCooldown,
			mode:       retryFixed,
		}
	}

	var transportErr *client.TransportError
	if errors.As(err, &transportErr) {
		return m.classifyTransportFailure(transportErr.Err)
	}

	if errors.Is(err, context.DeadlineExceeded) || os.IsTimeout(err) {
		return assessment{
			state:  StateServerUnreachable,
			reason: ReasonTimeout,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	}

	return assessment{
		state:  StateDegraded,
		reason: ReasonUnknown,
		detail: strings.TrimSpace(err.Error()),
		mode:   retryExponential,
	}
}

func (m *Manager) classifyHTTPFailure(op Operation, err *client.HTTPError) assessment {
	switch {
	case err.StatusCode == http.StatusUnauthorized:
		state := StateRegisterRequired
		retryAfter := m.policy.BaseRetry
		if op == OpRegister {
			state = StateRegistrationRejected
			retryAfter = m.policy.RegistrationCooldown
		}
		if op == OpRefreshToken {
			state = StateTokenRefreshFailed
		}
		return assessment{
			state:          state,
			reason:         ReasonHTTP401,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     retryAfter,
			mode:           retryFixed,
		}
	case err.StatusCode == http.StatusForbidden:
		return assessment{
			state:          StateAuthorizationRejected,
			reason:         ReasonHTTP403,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     m.policy.AuthorizationCooldown,
			mode:           retryFixed,
		}
	case err.StatusCode == http.StatusNotFound:
		return assessment{
			state:          StateConfigError,
			reason:         ReasonHTTP404,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     m.policy.ConfigErrorCooldown,
			mode:           retryFixed,
		}
	case err.StatusCode == http.StatusConflict:
		return assessment{
			state:          StateRegistrationRejected,
			reason:         ReasonHTTP409,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     m.policy.RegistrationCooldown,
			mode:           retryFixed,
		}
	case err.StatusCode == http.StatusTooManyRequests:
		retryAfter := parseRetryAfter(err.RetryAfter, m.now())
		if retryAfter <= 0 {
			retryAfter = m.policy.RateLimitCooldown
		}
		return assessment{
			state:          StateRateLimited,
			reason:         ReasonHTTP429,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     retryAfter,
			mode:           retryFixed,
		}
	case err.StatusCode >= http.StatusInternalServerError:
		return assessment{
			state:          StateServerError,
			reason:         ReasonHTTP5xx,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			mode:           retryExponential,
		}
	default:
		return assessment{
			state:          StateProtocolError,
			reason:         ReasonHTTPUnexpected,
			detail:         strings.TrimSpace(err.Body),
			httpStatusCode: err.StatusCode,
			retryAfter:     m.policy.ProtocolErrorCooldown,
			mode:           retryFixed,
		}
	}
}

func (m *Manager) classifyTransportFailure(err error) assessment {
	if err == nil {
		return assessment{
			state:  StateDegraded,
			reason: ReasonTransport,
			mode:   retryExponential,
		}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return assessment{
			state:  StateDNSFailed,
			reason: ReasonDNS,
			detail: strings.TrimSpace(dnsErr.Error()),
			mode:   retryExponential,
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return assessment{
			state:  StateServerUnreachable,
			reason: ReasonTimeout,
			detail: strings.TrimSpace(netErr.Error()),
			mode:   retryExponential,
		}
	}

	text := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case os.IsTimeout(err) || errors.Is(err, context.DeadlineExceeded) || strings.Contains(text, "timeout"):
		return assessment{
			state:  StateServerUnreachable,
			reason: ReasonTimeout,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	case strings.Contains(text, "no such host"), strings.Contains(text, "server misbehaving"):
		return assessment{
			state:  StateDNSFailed,
			reason: ReasonDNS,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	case strings.Contains(text, "connection refused"), strings.Contains(text, "actively refused"):
		return assessment{
			state:  StateServerUnreachable,
			reason: ReasonConnectRefused,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	case strings.Contains(text, "network is unreachable"),
		strings.Contains(text, "host is unreachable"),
		strings.Contains(text, "no route to host"),
		strings.Contains(text, "unreachable network"):
		return assessment{
			state:  StateNoNetwork,
			reason: ReasonNoNetwork,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	case strings.Contains(text, "tls"), strings.Contains(text, "certificate"):
		return assessment{
			state:      StateConfigError,
			reason:     ReasonTLS,
			detail:     strings.TrimSpace(err.Error()),
			retryAfter: m.policy.ConfigErrorCooldown,
			mode:       retryFixed,
		}
	default:
		return assessment{
			state:  StateDegraded,
			reason: ReasonTransport,
			detail: strings.TrimSpace(err.Error()),
			mode:   retryExponential,
		}
	}
}

func (m *Manager) exponentialBackoff(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}

	backoff := time.Duration(float64(m.policy.BaseRetry) * math.Pow(2, float64(failures-1)))
	if backoff > m.policy.MaxRetry {
		backoff = m.policy.MaxRetry
	}
	if backoff < time.Second {
		backoff = time.Second
	}
	if m.policy.RetryJitterFactor == 0 {
		return backoff
	}

	minFactor := 1 - m.policy.RetryJitterFactor
	maxFactor := 1 + m.policy.RetryJitterFactor
	factor := minFactor + m.rnd.Float64()*(maxFactor-minFactor)
	withJitter := time.Duration(float64(backoff) * factor)
	if withJitter < time.Second {
		return time.Second
	}
	return withJitter
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}

	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}

	if deadline, err := http.ParseTime(value); err == nil {
		if !deadline.After(now) {
			return 0
		}
		return deadline.Sub(now)
	}

	return 0
}
