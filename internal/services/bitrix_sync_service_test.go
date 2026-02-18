package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBitrixSyncService_UpsertDealAndLink_UsesExistingDealLinkWithoutCreatingDuplicate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&bitrix.DealLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему bitrix: %v", err)
	}

	repo := repositories.NewBitrixRepo(db)
	ctx := context.Background()
	ticketID := "d041b595-65a2-4614-8472-630d1fdbae78"
	if err := repo.UpsertDealLink(ctx, &bitrix.DealLink{
		TicketID:   ticketID,
		B24DealID:  6299,
		LastSyncAt: time.Now(),
	}); err != nil {
		t.Fatalf("не удалось подготовить исходный deal_link: %v", err)
	}

	var mu sync.Mutex
	updateCalls := 0
	listCalls := 0
	addCalls := 0
	updatedDealID := int64(0)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/crm.deal.update.json"):
			var payload struct {
				ID     int64                  `json:"id"`
				Fields map[string]interface{} `json:"fields"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			mu.Lock()
			updateCalls++
			updatedDealID = payload.ID
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": true})
		case strings.HasSuffix(r.URL.Path, "/crm.deal.list.json"):
			mu.Lock()
			listCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": []interface{}{}})
		case strings.HasSuffix(r.URL.Path, "/crm.deal.add.json"):
			mu.Lock()
			addCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"result": 6301})
		default:
			http.Error(w, "unexpected method", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway: true,
		RequestTimeout:      2 * time.Second,
		BitrixBaseURL:       server.URL + "/rest/457/secret",
		BitrixOriginatorID:  "ETALON_SD",
		BitrixCategoryID:    17,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    logger.New("", "test", "error", true),
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repo,
	}

	pointID := int64(16961)
	ticket := &tickets.Ticket{
		Number:               67627,
		Subject:              "Задача №6299",
		Status:               tickets.StatusNew,
		Type:                 tickets.TypeIncident,
		SyncWithBitrix:       true,
		BitrixServicePointID: &pointID,
		BitrixDealTitle:      "Задача №6299",
	}
	ticket.ID = ticketID

	dealID, err := svc.upsertDealAndLink(ctx, ticket)
	if err != nil {
		t.Fatalf("upsertDealAndLink завершился ошибкой: %v", err)
	}

	mu.Lock()
	gotUpdateCalls := updateCalls
	gotListCalls := listCalls
	gotAddCalls := addCalls
	gotUpdatedDealID := updatedDealID
	mu.Unlock()

	if dealID != 6299 {
		t.Fatalf("ожидался dealID=6299, получено %d", dealID)
	}
	if gotUpdateCalls != 1 {
		t.Fatalf("ожидался ровно один вызов crm.deal.update, получено %d", gotUpdateCalls)
	}
	if gotUpdatedDealID != 6299 {
		t.Fatalf("ожидалось обновление сделки 6299, получено %d", gotUpdatedDealID)
	}
	if gotListCalls != 0 {
		t.Fatalf("crm.deal.list не должен вызываться при существующем deal_link, получено %d", gotListCalls)
	}
	if gotAddCalls != 0 {
		t.Fatalf("crm.deal.add не должен вызываться при существующем deal_link, получено %d", gotAddCalls)
	}
}
