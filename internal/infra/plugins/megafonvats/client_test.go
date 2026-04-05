package megafonvats

import (
	"context"
	"encoding/json"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	"net/http"
	"net/http/httptest"
	"strings"
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
