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

type fakePyrusIncomingForReplay struct {
	replayedTasks []int64
	problemCalled bool
}

func (f *fakePyrusIncomingForReplay) HandleWebhook(context.Context, []byte, string) error { return nil }
func (f *fakePyrusIncomingForReplay) Start(context.Context)                               {}
func (f *fakePyrusIncomingForReplay) ReplayEvent(context.Context, string) error           { return nil }

func (f *fakePyrusIncomingForReplay) ReplayTask(_ context.Context, taskID int64) (int, error) {
	f.replayedTasks = append(f.replayedTasks, taskID)
	return 3, nil
}

func (f *fakePyrusIncomingForReplay) ReplayProblemTasks(context.Context) (int, int, error) {
	f.problemCalled = true
	return 2, 5, nil
}

func newPyrusTasksTestRouter(t *testing.T, name string) (chi.Router, *fakePyrusIncomingForReplay) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&pyrus.IncomingEvent{}, &pyrus.TicketLink{}); err != nil {
		t.Fatalf("не удалось выполнить миграцию: %v", err)
	}
	repo := infraRepos.NewPyrusRepo(db)
	base := time.Now().Add(-time.Hour)
	events := []pyrus.IncomingEvent{
		{ID: "e-1", PyrusTaskID: int64Ptr(500), Status: pyrus.IncomingEventStatusWaiting, ReceivedAt: base},
		{ID: "e-2", PyrusTaskID: int64Ptr(500), Status: pyrus.IncomingEventStatusFailed, ReceivedAt: base.Add(time.Minute)},
		{ID: "e-3", PyrusTaskID: int64Ptr(501), Status: pyrus.IncomingEventStatusDone, ReceivedAt: base.Add(2 * time.Minute)},
	}
	for i := range events {
		events[i].EventName = "comment"
		events[i].PayloadHash = events[i].ID
		events[i].PayloadRaw = `{"task_id":500,"task":{"id":500,"form_id":1,"fields":[{"code":"CrmId","name":"CRMID","type":"text","value":"CRM-77"}]}}`
		if _, err := repo.InsertIncomingEventIfNotExists(context.Background(), &events[i]); err != nil {
			t.Fatalf("не удалось сохранить событие: %v", err)
		}
	}
	fake := &fakePyrusIncomingForReplay{}
	router := chi.NewRouter()
	NewIntegrationSyncHandler(services.NewIntegrationSyncControlService(repo, fake, nil, nil)).RegisterRoutes(router)
	return router, fake
}

type pyrusTasksResponse struct {
	Data []struct {
		TaskID         int64            `json:"task_id"`
		CRMID          string           `json:"crm_id"`
		NeedsAttention bool             `json:"needs_attention"`
		StatusCounts   map[string]int64 `json:"status_counts"`
	} `json:"data"`
	Meta struct {
		Total int64 `json:"total"`
	} `json:"meta"`
}

func TestIntegrationSyncHandler_ListPyrusIncomingTasks(t *testing.T) {
	router, _ := newPyrusTasksTestRouter(t, "integration-sync-tasks")

	call := func(url string) (int, pyrusTasksResponse) {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		var payload pyrusTasksResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &payload)
		return rec.Code, payload
	}

	code, problem := call("/pyrus/sync/incoming-tasks")
	if code != http.StatusOK || problem.Meta.Total != 1 || len(problem.Data) != 1 {
		t.Fatalf("ожидали одну проблемную задачу, code=%d total=%d", code, problem.Meta.Total)
	}
	if problem.Data[0].TaskID != 500 || !problem.Data[0].NeedsAttention || problem.Data[0].CRMID != "CRM-77" {
		t.Fatalf("неверная сводка: %+v", problem.Data[0])
	}
	if counts := problem.Data[0].StatusCounts; counts["waiting"] != 1 || counts["failed"] != 1 {
		t.Fatalf("неверные счётчики статусов: %+v", counts)
	}

	if _, all := call("/pyrus/sync/incoming-tasks?scope=all"); all.Meta.Total != 2 {
		t.Fatalf("ожидали две задачи при scope=all, получили %d", all.Meta.Total)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pyrus/sync/incoming-events?task_id=500", nil))
	var events pyrusTasksResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &events)
	if events.Meta.Total != 2 {
		t.Fatalf("ожидали два события задачи 500, получили %d", events.Meta.Total)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/megafon-vats/sync/incoming-tasks", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("для провайдера без поддержки ожидали 404, получили %d", rec.Code)
	}
}

func TestIntegrationSyncHandler_ReplayPyrusTasks(t *testing.T) {
	router, fake := newPyrusTasksTestRouter(t, "integration-sync-replay")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pyrus/sync/incoming-tasks/500/replay", nil))
	if rec.Code != http.StatusAccepted || len(fake.replayedTasks) != 1 || fake.replayedTasks[0] != 500 {
		t.Fatalf("ожидали replay задачи 500, code=%d tasks=%v body=%s", rec.Code, fake.replayedTasks, rec.Body.String())
	}
	var replay struct {
		Data struct {
			Events int `json:"events"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &replay)
	if replay.Data.Events != 3 {
		t.Fatalf("ожидали в ответе число запущенных событий 3, получили %d (body=%s)", replay.Data.Events, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pyrus/sync/incoming-tasks/abc/replay", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("для некорректного task_id ожидали 400, получили %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/pyrus/sync/incoming-tasks/replay-problem", nil))
	if rec.Code != http.StatusAccepted || !fake.problemCalled {
		t.Fatalf("ожидали replay всех проблемных задач, code=%d body=%s", rec.Code, rec.Body.String())
	}
}
