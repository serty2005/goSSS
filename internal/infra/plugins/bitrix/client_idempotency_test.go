package bitrix

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
)

func newIdempotencyTestClient(serverURL string, timeout time.Duration) *Client {
	return NewClient(&config.Config{
		BitrixBaseURL:  serverURL + "/rest/1/key",
		RequestTimeout: timeout,
	}, logger.New("", "test", "error", true))
}

// Bitrix24 мог создать комментарий, но ответить позже таймаута: повтор создал бы дубль.
func TestTimelineCommentAdd_TimeoutIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(300 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 1})
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 100*time.Millisecond)
	_, err := client.TimelineCommentAdd(context.Background(), 45, "text", nil)
	if !errors.Is(err, ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown, получено %v", err)
	}
	// Дожидаемся завершения обработчика, чтобы убедиться, что дополнительных запросов не было.
	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("ожидался ровно один запрос, получено %d", got)
	}
}

func TestTimelineCommentAdd_GatewayErrorIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"result":0}`))
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 2*time.Second)
	_, err := client.TimelineCommentAdd(context.Background(), 45, "text", nil)
	if !errors.Is(err, ErrResultUnknown) {
		t.Fatalf("ожидалась ErrResultUnknown для 502, получено %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("ожидался ровно один запрос, получено %d", got)
	}
}

// Отказ по лимиту гарантирует, что запрос не обработан, поэтому повтор безопасен.
func TestTimelineCommentAdd_QueryLimitIsRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"error": "QUERY_LIMIT_EXCEEDED", "error_description": "Too many requests"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 42})
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 2*time.Second)
	id, err := client.TimelineCommentAdd(context.Background(), 45, "text", nil)
	if err != nil {
		t.Fatalf("ожидался успех после повтора по лимиту: %v", err)
	}
	if id != 42 {
		t.Fatalf("ожидался id=42, получено %d", id)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("ожидалось 2 запроса, получено %d", got)
	}
}

func TestTimelineCommentAdd_ConnectionRefusedIsRetried(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close()

	client := newIdempotencyTestClient(url, 500*time.Millisecond)
	_, err := client.TimelineCommentAdd(context.Background(), 45, "text", nil)
	if err == nil {
		t.Fatalf("ожидалась ошибка недоступного сервера")
	}
	if errors.Is(err, ErrResultUnknown) {
		t.Fatalf("при ошибке установки соединения запрос не отправлялся, результат известен: %v", err)
	}
}

// Идемпотентные методы по-прежнему повторяются при таймауте.
func TestIdempotentMethod_TimeoutIsRetried(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(300 * time.Millisecond)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": map[string]interface{}{"ID": 45, "COMMENT": "text"}})
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 100*time.Millisecond)
	comment, err := client.TimelineCommentGet(context.Background(), 45)
	if err != nil {
		t.Fatalf("ожидался успех после повтора: %v", err)
	}
	if comment == nil || comment.ID != 45 {
		t.Fatalf("получен неверный комментарий: %+v", comment)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("ожидалось 2 запроса, получено %d", got)
	}
}

func TestTimelineCommentListAll_FollowsPagination(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Start int `json:"start"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body.Start {
		case 0:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": []map[string]interface{}{{"ID": "1", "COMMENT": "a", "ENTITY_TYPE": "deal", "ENTITY_ID": "45"}},
				"next":   50,
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"result": []map[string]interface{}{{"ID": "2", "COMMENT": "b", "ENTITY_TYPE": "deal", "ENTITY_ID": "45"}},
			})
		}
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 2*time.Second)
	items, err := client.TimelineCommentListAll(context.Background(), 45)
	if err != nil {
		t.Fatalf("TimelineCommentListAll: %v", err)
	}
	if len(items) != 2 || items[0].ID != 1 || items[1].ID != 2 {
		t.Fatalf("ожидались комментарии 1 и 2, получено %+v", items)
	}
}

// Запись получает увеличенный таймаут, чтобы не обрывать ответ, пока Bitrix24 еще выполняет запрос; чтение остается коротким.
func TestWriteTimeout_AppliesOnlyToNonIdempotentMethods(t *testing.T) {
	var addCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		if strings.HasSuffix(r.URL.Path, "/crm.timeline.comment.add.json") {
			addCalls.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 77})
	}))
	defer server.Close()

	client := NewClient(&config.Config{
		BitrixBaseURL:      server.URL + "/rest/1/key",
		RequestTimeout:     100 * time.Millisecond,
		BitrixWriteTimeout: 3 * time.Second,
	}, logger.New("", "test", "error", true))

	id, err := client.TimelineCommentAdd(context.Background(), 45, "text", nil)
	if err != nil || id != 77 {
		t.Fatalf("медленный add должен дождаться ответа: id=%d err=%v", id, err)
	}
	if got := addCalls.Load(); got != 1 {
		t.Fatalf("ожидался один вызов add, получено %d", got)
	}
}

func TestTimelineCommentListSince_StopsAtBoundary(t *testing.T) {
	var pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Start int               `json:"start"`
			Order map[string]string `json:"order"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Order["ID"] != "DESC" {
			t.Errorf("ожидалась сортировка ID DESC, получено %v", body.Order)
		}
		pages.Add(1)
		item := func(id int) map[string]interface{} {
			return map[string]interface{}{"ID": strconv.Itoa(id), "COMMENT": "c", "ENTITY_TYPE": "deal", "ENTITY_ID": "45"}
		}
		switch body.Start {
		case 0:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": []map[string]interface{}{item(900), item(800)}, "next": 50})
		case 50:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": []map[string]interface{}{item(700), item(600)}, "next": 100})
		default:
			t.Errorf("чтение должно остановиться на границе, но запрошена страница start=%d", body.Start)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": []map[string]interface{}{}})
		}
	}))
	defer server.Close()

	client := newIdempotencyTestClient(server.URL, 2*time.Second)
	items, err := client.TimelineCommentListSince(context.Background(), 45, 650)
	if err != nil {
		t.Fatalf("TimelineCommentListSince: %v", err)
	}
	if len(items) != 3 || items[0].ID != 900 || items[2].ID != 700 {
		t.Fatalf("ожидались комментарии 900, 800, 700, получено %+v", items)
	}
	if got := pages.Load(); got != 2 {
		t.Fatalf("ожидалось 2 страницы, получено %d", got)
	}
}
