package handlers

import (
	"context"
	"encoding/json"
	"etalon-server/internal/domain/pyrus"
	"etalon-server/internal/domain/telephony"
	infraRepos "etalon-server/internal/infra/repositories"
	"etalon-server/internal/services"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
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

	handler := NewIntegrationSyncHandler(services.NewIntegrationSyncControlService(repo, nil, nil, nil))
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
			ID        string `json:"id"`
			Provider  string `json:"provider"`
			Direction string `json:"direction"`
			EventName string `json:"event_name"`
			Status    string `json:"status"`
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

func TestIntegrationSyncHandler_ListMegafonIncomingEvents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:integration-sync-handler-megafon?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&telephony.IncomingEvent{}); err != nil {
		t.Fatalf("не удалось выполнить миграцию: %v", err)
	}

	repo := infraRepos.NewTelephonyRepo(db)
	receivedAt := time.Now().Add(-time.Minute)
	if _, err := repo.InsertIncomingEventIfNotExists(context.Background(), &telephony.IncomingEvent{
		ID:             "megafon-event-1",
		Provider:       telephony.ProviderMegafonVATS,
		Cmd:            telephony.IncomingEventCommandEvent,
		EventName:      "INCOMING",
		ExternalCallID: "call-100",
		PayloadHash:    "megafon-hash-1",
		PayloadRaw:     "cmd=event&type=INCOMING&callid=call-100",
		Status:         telephony.IncomingEventStatusQueued,
		ReceivedAt:     receivedAt,
	}); err != nil {
		t.Fatalf("не удалось сохранить событие телефонии: %v", err)
	}

	handler := NewIntegrationSyncHandler(services.NewIntegrationSyncControlService(nil, nil, repo, nil))
	router := chi.NewRouter()
	handler.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/megafon-vats/sync/incoming-events?limit=10", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("ожидали HTTP 200, получили %d, body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Data []struct {
			ID               string `json:"id"`
			Provider         string `json:"provider"`
			Direction        string `json:"direction"`
			EventName        string `json:"event_name"`
			ExternalEntityID string `json:"external_entity_id"`
			Status           string `json:"status"`
		} `json:"data"`
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("не удалось распарсить ответ: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("ожидали один элемент в ответе, total=%d len=%d", payload.Total, len(payload.Data))
	}
	if payload.Data[0].Provider != "megafon-vats" || payload.Data[0].Direction != "incoming" {
		t.Fatalf("ожидали provider=megafon-vats и direction=incoming, получили %+v", payload.Data[0])
	}
	if payload.Data[0].EventName != "event:INCOMING" {
		t.Fatalf("ожидали event_name=event:INCOMING, получили %q", payload.Data[0].EventName)
	}
	if payload.Data[0].ExternalEntityID != "call:call-100" {
		t.Fatalf("ожидали external_entity_id=call:call-100, получили %q", payload.Data[0].ExternalEntityID)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}
