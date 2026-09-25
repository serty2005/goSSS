package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"etalon-server/internal/domain/models"

	"github.com/glebarez/sqlite"
	"github.com/go-chi/chi/v5"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func TestListLatestForAgents_ReturnsLatestObservationPerAgent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:latest_for_agents?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&models.AgentObservation{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	agentA := "agent-a"
	agentB := "agent-b"
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	create := func(uid string, agentUUID *string, source string, observedAt time.Time, payload string) {
		t.Helper()
		item := models.AgentObservation{
			ObservationUID: uid,
			Source:         source,
			AgentUUID:      agentUUID,
			ObservedAt:     observedAt,
			PayloadJSON:    datatypes.JSON(payload),
			PayloadHash:    uid,
			Status:         "processed",
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("не удалось создать наблюдение %s: %v", uid, err)
		}
	}
	create("a-old", &agentA, "http", base, `{"v_time":"2026-09-01 10:00:00"}`)
	create("a-new", &agentA, "http", base.Add(time.Hour), `{"v_time":"2026-09-01 11:00:00"}`)
	// legacy-наблюдение без agent_uuid, где агент указан в source
	create("b-legacy", nil, agentB, base.Add(2*time.Hour), `{"v_time":"2026-09-01 12:00:00"}`)
	create("other", nil, "other-agent", base.Add(3*time.Hour), `{}`)

	router := chi.NewRouter()
	NewAgentObservationFeedHandler(db).RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodGet, "/agent-observations/latest?agent_uuids=agent-a,agent-b,agent-missing,agent-a", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ожидали 200, получили %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Data []observationFeedRow `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("не удалось разобрать ответ: %v", err)
	}
	byAgent := map[string]observationFeedRow{}
	for _, row := range body.Data {
		byAgent[trimPtrValue(row.AgentUUID)] = row
	}
	if len(byAgent) != 2 {
		t.Fatalf("ожидали наблюдения двух агентов, получили %+v", body.Data)
	}
	if got := trimPtrValue(byAgent[agentA].VTimeRaw); got != "2026-09-01 11:00:00" {
		t.Fatalf("для agent-a ожидали последнее наблюдение, получили v_time=%q", got)
	}
	if got := trimPtrValue(byAgent[agentB].VTimeRaw); got != "2026-09-01 12:00:00" {
		t.Fatalf("для agent-b ожидали legacy-наблюдение по source, получили v_time=%q", got)
	}
}
