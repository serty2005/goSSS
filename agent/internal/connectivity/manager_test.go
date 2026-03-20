package connectivity

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"etalon-agent/internal/client"
)

func newTestManager(t *testing.T, now time.Time) *Manager {
	t.Helper()

	manager, err := NewManager(filepath.Join(t.TempDir(), "connectivity-state.json"), Policy{
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
	manager.now = func() time.Time { return now }
	return manager
}

func TestManagerRecordFailure_HTTP403UsesAuthorizationCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	update, retryAfter, err := manager.RecordFailure(OpHeartbeat, "http://localhost:8080/api/agents/agent-uuid/data", &client.HTTPError{
		StatusCode: 403,
		Body:       "forbidden",
		Method:     "POST",
		URL:        "http://localhost:8080/api/agents/agent-uuid/data",
	})
	if err != nil {
		t.Fatalf("не удалось сохранить состояние связи: %v", err)
	}
	if retryAfter != 24*time.Hour {
		t.Fatalf("ожидался cooldown 24h, получено %s", retryAfter)
	}
	if update.Current.State != StateAuthorizationRejected {
		t.Fatalf("ожидалось состояние %s, получено %s", StateAuthorizationRejected, update.Current.State)
	}
	if update.Current.ReasonKind != ReasonHTTP403 {
		t.Fatalf("ожидалась причина %s, получено %s", ReasonHTTP403, update.Current.ReasonKind)
	}
	if !update.Current.NextAllowedAttemptAt.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("ожидалось next_allowed_attempt_at %s, получено %s", now.Add(24*time.Hour), update.Current.NextAllowedAttemptAt)
	}
}

func TestManagerRecordFailure_HTTP429UsesRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	update, retryAfter, err := manager.RecordFailure(OpHeartbeat, "http://localhost:8080/api/agents/agent-uuid/data", &client.HTTPError{
		StatusCode: 429,
		Body:       "too many requests",
		Method:     "POST",
		URL:        "http://localhost:8080/api/agents/agent-uuid/data",
		RetryAfter: "120",
	})
	if err != nil {
		t.Fatalf("не удалось сохранить состояние связи: %v", err)
	}
	if retryAfter != 2*time.Minute {
		t.Fatalf("ожидался retry-after 2m, получено %s", retryAfter)
	}
	if update.Current.State != StateRateLimited {
		t.Fatalf("ожидалось состояние %s, получено %s", StateRateLimited, update.Current.State)
	}
}

func TestManagerRecordFailure_DNSErrorUsesExponentialBackoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	firstUpdate, firstDelay, err := manager.RecordFailure(OpHeartbeat, "http://bad-host/api/agents/agent-uuid/data", &client.TransportError{
		Method: "POST",
		URL:    "http://bad-host/api/agents/agent-uuid/data",
		Err:    &net.DNSError{Err: "no such host", Name: "bad-host"},
	})
	if err != nil {
		t.Fatalf("не удалось сохранить первое состояние связи: %v", err)
	}
	if firstDelay != 15*time.Second {
		t.Fatalf("ожидался первый backoff 15s, получено %s", firstDelay)
	}
	if firstUpdate.Current.State != StateDNSFailed {
		t.Fatalf("ожидалось состояние %s, получено %s", StateDNSFailed, firstUpdate.Current.State)
	}

	manager.now = func() time.Time { return now.Add(15 * time.Second) }
	secondUpdate, secondDelay, err := manager.RecordFailure(OpHeartbeat, "http://bad-host/api/agents/agent-uuid/data", &client.TransportError{
		Method: "POST",
		URL:    "http://bad-host/api/agents/agent-uuid/data",
		Err:    &net.DNSError{Err: "no such host", Name: "bad-host"},
	})
	if err != nil {
		t.Fatalf("не удалось сохранить второе состояние связи: %v", err)
	}
	if secondDelay != 30*time.Second {
		t.Fatalf("ожидался второй backoff 30s, получено %s", secondDelay)
	}
	if secondUpdate.Current.ConsecutiveFailures != 2 {
		t.Fatalf("ожидалось 2 последовательных ошибки, получено %d", secondUpdate.Current.ConsecutiveFailures)
	}
}

func TestManagerRecordFailure_RequestBuildErrorUsesConfigCooldown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.March, 21, 10, 0, 0, 0, time.UTC)
	manager := newTestManager(t, now)

	update, retryAfter, err := manager.RecordFailure(OpRegister, "http://:bad", &client.RequestBuildError{
		Method: "POST",
		URL:    "http://:bad",
		Err:    errors.New("missing protocol scheme"),
	})
	if err != nil {
		t.Fatalf("не удалось сохранить состояние связи: %v", err)
	}
	if retryAfter != time.Hour {
		t.Fatalf("ожидался cooldown конфигурации 1h, получено %s", retryAfter)
	}
	if update.Current.State != StateConfigError {
		t.Fatalf("ожидалось состояние %s, получено %s", StateConfigError, update.Current.State)
	}
	if update.Current.ReasonKind != ReasonRequestBuild {
		t.Fatalf("ожидалась причина %s, получено %s", ReasonRequestBuild, update.Current.ReasonKind)
	}
}
