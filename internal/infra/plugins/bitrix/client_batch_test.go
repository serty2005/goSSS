package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
)

func TestUserGetAll_UsesBatchPagination(t *testing.T) {
	var (
		mu           sync.Mutex
		userGetCalls int
		batchCalls   int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/user.get.json"):
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if got := int(body["start"].(float64)); got != 0 {
				t.Fatalf("ожидался первый запрос user.get со start=0, получено %d", got)
			}
			mu.Lock()
			userGetCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": buildBitrixUsersPayload(1, 50),
				"next":   50,
				"total":  120,
			})
		case strings.HasSuffix(r.URL.Path, "/batch.json"):
			var body struct {
				Cmd map[string]string `json:"cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			batchCalls++
			mu.Unlock()
			if len(body.Cmd) != 2 {
				t.Fatalf("ожидалось 2 команды в batch, получено %d", len(body.Cmd))
			}
			if _, ok := body.Cmd["page_50"]; !ok {
				t.Fatalf("в batch отсутствует команда page_50: %#v", body.Cmd)
			}
			if _, ok := body.Cmd["page_100"]; !ok {
				t.Fatalf("в batch отсутствует команда page_100: %#v", body.Cmd)
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": map[string]interface{}{
					"result": map[string]interface{}{
						"page_50":  buildBitrixUsersPayload(51, 50),
						"page_100": buildBitrixUsersPayload(101, 20),
					},
					"result_error": map[string]interface{}{},
					"result_total": map[string]interface{}{
						"page_50":  120,
						"page_100": 120,
					},
					"result_next": map[string]interface{}{
						"page_50": 100,
					},
				},
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		BitrixBaseURL:         server.URL + "/rest/1/key",
		RequestTimeout:        2 * time.Second,
		BitrixRateLimitPerMin: 120,
		BitrixRateLimitBurst:  50,
	}
	client := NewClient(cfg, logger.New("", "test", "error", true))

	users, err := client.UserGetAll(context.Background())
	if err != nil {
		t.Fatalf("ошибка UserGetAll: %v", err)
	}
	if len(users) != 120 {
		t.Fatalf("ожидалось 120 пользователей, получено %d", len(users))
	}

	mu.Lock()
	gotUserGetCalls := userGetCalls
	gotBatchCalls := batchCalls
	mu.Unlock()
	if gotUserGetCalls != 1 {
		t.Fatalf("ожидался 1 прямой вызов user.get, получено %d", gotUserGetCalls)
	}
	if gotBatchCalls != 1 {
		t.Fatalf("ожидался 1 batch-вызов, получено %d", gotBatchCalls)
	}
}

func buildBitrixUsersPayload(startID int, count int) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, count)
	for i := 0; i < count; i++ {
		id := startID + i
		items = append(items, map[string]interface{}{
			"ID":              id,
			"NAME":            "Иван",
			"LAST_NAME":       "Петров",
			"SECOND_NAME":     "",
			"EMAIL":           "ivan@example.com",
			"PERSONAL_MOBILE": "+79990000000",
			"ACTIVE":          "Y",
		})
	}
	return items
}
