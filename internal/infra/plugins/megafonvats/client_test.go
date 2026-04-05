package megafonvats

import (
	"context"
	"encoding/json"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClient_ListUsersUsesPaginationAndAPIKey(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if got := r.Header.Get("X-API-KEY"); got != "secret-key" {
			t.Fatalf("ожидали X-API-KEY=secret-key, получили %q", got)
		}
		if !strings.HasPrefix(r.URL.Path, "/crmapi/v1/users") {
			t.Fatalf("неожиданный путь %q", r.URL.Path)
		}

		switch r.URL.Query().Get("start") {
		case "":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"login": "admin", "name": "Иван Иванов", "status": "online"},
					{"login": "petrov", "name": "Петр Петров", "status": "offline"},
				},
				"info": map[string]any{
					"start": 0,
					"limit": 2,
					"total": 3,
					"next":  2,
				},
			})
		case "2":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{"login": "sidorov", "name": "Сидор Сидоров", "status": "online"},
				},
				"info": map[string]any{
					"start": 2,
					"limit": 2,
					"total": 3,
				},
			})
		default:
			t.Fatalf("неожиданный start=%q", r.URL.Query().Get("start"))
		}
	}))
	defer server.Close()

	client := NewClient(
		&config.Config{
			MegafonVATSBaseURL: server.URL,
			MegafonVATSAPIKey:  "secret-key",
			RequestTimeout:     2 * time.Second,
		},
		logger.New("", "test", "error", true),
	)

	items, err := client.ListUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListUsers вернул ошибку: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("ожидали 3 сотрудников после пагинации, получили %d", len(items))
	}
	if requests != 2 {
		t.Fatalf("ожидали 2 HTTP-запроса, получили %d", requests)
	}
}

func TestClient_GetUserReturnsNilOn404(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewClient(
		&config.Config{
			MegafonVATSBaseURL: server.URL,
			MegafonVATSAPIKey:  "secret-key",
			RequestTimeout:     2 * time.Second,
		},
		logger.New("", "test", "error", true),
	)

	item, err := client.GetUser(context.Background(), "missing", true)
	if err != nil {
		t.Fatalf("GetUser вернул ошибку: %v", err)
	}
	if item != nil {
		t.Fatalf("ожидали nil для отсутствующего сотрудника, получили %+v", item)
	}
}

func TestClient_ListHistoryBuildsQueryAndParsesJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-KEY"); got != "secret-key" {
			t.Fatalf("ожидали X-API-KEY=secret-key, получили %q", got)
		}
		if r.URL.Path != "/crmapi/v1/history/json" {
			t.Fatalf("неожиданный путь %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("period"); got != "today" {
			t.Fatalf("ожидали period=today, получили %q", got)
		}
		if got := r.URL.Query().Get("user"); got != "admin" {
			t.Fatalf("ожидали user=admin, получили %q", got)
		}
		if got := r.URL.Query().Get("processMissed"); got != "true" {
			t.Fatalf("ожидали processMissed=true, получили %q", got)
		}
		statuses := r.URL.Query()["missedStatus"]
		if len(statuses) != 2 || statuses[0] != "2" || statuses[1] != "3" {
			t.Fatalf("ожидали missedStatus=[2 3], получили %+v", statuses)
		}

		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"uid":          "call-001",
				"type":         "in",
				"status":       "success",
				"client":       "79260001122",
				"user":         "admin",
				"group_name":   "Техподдержка",
				"diversion":    "74950000001",
				"start":        "2026-04-05T12:00:00Z",
				"wait":         5,
				"duration":     23,
				"record":       "https://example.com/record.mp3",
				"missedStatus": 2,
			},
		})
	}))
	defer server.Close()

	client := NewClient(
		&config.Config{
			MegafonVATSBaseURL: server.URL,
			MegafonVATSAPIKey:  "secret-key",
			RequestTimeout:     2 * time.Second,
		},
		logger.New("", "test", "error", true),
	)

	items, err := client.ListHistory(context.Background(), HistoryFilter{
		Period:        "today",
		User:          "admin",
		ProcessMissed: true,
		MissedStatus:  []string{"2", "3"},
	})
	if err != nil {
		t.Fatalf("ListHistory вернул ошибку: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ожидали 1 запись истории, получили %d", len(items))
	}
	if items[0].UID != "call-001" {
		t.Fatalf("ожидали uid=call-001, получили %q", items[0].UID)
	}
	if items[0].MissedStatus == nil || *items[0].MissedStatus != 2 {
		t.Fatalf("ожидали missedStatus=2, получили %+v", items[0].MissedStatus)
	}
	if items[0].Wait == nil || *items[0].Wait != 5 {
		t.Fatalf("ожидали wait=5, получили %+v", items[0].Wait)
	}
}

func TestRedactURLForLogMasksSensitiveQueryParams(t *testing.T) {
	masked := RedactURLForLog("https://sd.example.ru/crmapi/v1/history/json?crm_token=supersecret&api_key=topsecret&limit=100")
	if strings.Contains(masked, "supersecret") || strings.Contains(masked, "topsecret") {
		t.Fatalf("ожидали маскирование секретов в URL, получили %q", masked)
	}
	if !strings.Contains(masked, "crm_token=su") || !strings.Contains(masked, "api_key=to") {
		t.Fatalf("ожидали сохранение читаемой маски в URL, получили %q", masked)
	}
	if !strings.Contains(masked, "limit=100") {
		t.Fatalf("ожидали сохранение несекретных параметров, получили %q", masked)
	}
}

func TestClient_ListUsersLogsMaskedRequestAndResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"login": "admin", "name": "Иван Иванов", "status": "online"},
			},
			"info": map[string]any{
				"start": 0,
				"limit": 100,
				"total": 1,
			},
		})
	}))
	defer server.Close()

	log := newCaptureMegafonLogger()
	client := NewClient(
		&config.Config{
			MegafonVATSBaseURL: server.URL,
			MegafonVATSAPIKey:  "secret-key",
			RequestTimeout:     2 * time.Second,
		},
		log,
	)

	_, err := client.ListUsers(context.Background(), true)
	if err != nil {
		t.Fatalf("ListUsers вернул ошибку: %v", err)
	}

	requestEntry, ok := log.find("Мегафон ВАТС API исходящий запрос")
	if !ok {
		t.Fatal("ожидали лог исходящего запроса")
	}
	requestFields := captureArgsToMap(requestEntry.args)
	requestURL, _ := requestFields["url"].(string)
	if !strings.Contains(requestURL, "/crmapi/v1/users?") {
		t.Fatalf("ожидали полный URL запроса, получили %q", requestURL)
	}
	headers, ok := requestFields["headers"].(map[string]string)
	if !ok {
		t.Fatalf("ожидали map заголовков в логе, получили %#v", requestFields["headers"])
	}
	apiKeyHeader := headers["X-Api-Key"]
	if apiKeyHeader == "secret-key" || !strings.Contains(apiKeyHeader, "*") {
		t.Fatalf("ожидали маскирование X-API-KEY, получили %q", apiKeyHeader)
	}

	responseEntry, ok := log.find("Мегафон ВАТС API входящий ответ")
	if !ok {
		t.Fatal("ожидали лог входящего ответа")
	}
	responseFields := captureArgsToMap(responseEntry.args)
	if gotStatus, ok := responseFields["status_code"].(int); !ok || gotStatus != http.StatusOK {
		t.Fatalf("ожидали status_code=200 в логе ответа, получили %#v", responseFields["status_code"])
	}
	if gotURL, _ := responseFields["url"].(string); gotURL != requestURL {
		t.Fatalf("ожидали совпадающий URL в логе ответа, получили %q и %q", gotURL, requestURL)
	}
}

type captureMegafonLogEntry struct {
	msg  string
	args []any
}

type captureMegafonLogger struct {
	mu      sync.Mutex
	entries []captureMegafonLogEntry
}

func newCaptureMegafonLogger() *captureMegafonLogger {
	return &captureMegafonLogger{
		entries: make([]captureMegafonLogEntry, 0, 8),
	}
}

func (l *captureMegafonLogger) Debug(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureMegafonLogger) Info(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureMegafonLogger) Warn(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureMegafonLogger) Error(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureMegafonLogger) Fatal(msg string, args ...any) {
	l.append(msg, args...)
}

func (l *captureMegafonLogger) With(args ...any) logger.LoggerInterface {
	return l
}

func (l *captureMegafonLogger) find(msg string) (captureMegafonLogEntry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, entry := range l.entries {
		if entry.msg == msg {
			return entry, true
		}
	}
	return captureMegafonLogEntry{}, false
}

func (l *captureMegafonLogger) append(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, captureMegafonLogEntry{
		msg:  msg,
		args: append([]any(nil), args...),
	})
}

func captureArgsToMap(args []any) map[string]any {
	result := make(map[string]any, len(args)/2)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		result[key] = args[i+1]
	}
	return result
}
