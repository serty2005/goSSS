package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/pyrus"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationSyncHandler_ListIncomingEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:integration-sync-handler?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&pyrus.IncomingEvent{}); err != nil {
		t.Fatalf("не удалось выполнить миграцию: %v", err)
	}

	repo := infraRepos.NewPyrusRepo(db)
	receivedAt := time.Now().Add(-time.Minute)
	if _, err := repo.InsertIncomingEventIfNotExists(context.Background(), &pyrus.IncomingEvent{
		ID:          "event-1",
		EventName:   "form_task_changed",
		PyrusTaskID: int64Ptr(123),
		PayloadHash: "hash-1",
		PayloadRaw:  `{"task_id":123}`,
		Status:      pyrus.IncomingEventStatusNew,
		ReceivedAt:  receivedAt,
	}); err != nil {
		t.Fatalf("не удалось сохранить событие: %v", err)
	}

	handler := NewIntegrationSyncHandler(services.NewIntegrationSyncControlService(repo, nil))
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/pyrus/sync/incoming-events?limit=10", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидали HTTP 200, получили %d, body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data []struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
			Direction string `json:"direction"`
			EventName string `json:"event_name"`
			Status   string `json:"status"`
		} `json:"data"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("не удалось распарсить ответ: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("ожидали один элемент в ответе, total=%d len=%d", payload.Total, len(payload.Data))
	}
	if payload.Data[0].Provider != "pyrus" || payload.Data[0].Direction != "incoming" {
		t.Fatalf("ожидали provider=pyrus и direction=incoming, получили %+v", payload.Data[0])
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
