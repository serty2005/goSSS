package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/telephony"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestBitrixSyncServiceBuildDealFieldsAddsContactIDFromTelephonyContact(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bitrix-contact-sync?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&telephony.Contact{}); err != nil {
		t.Fatalf("не удалось подготовить схему телефонии: %v", err)
	}

	telephonyRepo := repositories.NewTelephonyRepo(db)
	ctx := context.Background()
	contactName := "Юрий"
	localContact, err := telephonyRepo.UpsertContact(ctx, telephony.ContactUpsert{
		PhoneNormalized: "+79040002517",
		PhoneDisplay:    "+79040002517",
		Name:            &contactName,
	})
	if err != nil {
		t.Fatalf("не удалось сохранить локальный контакт: %v", err)
	}

	var (
		mu                 sync.Mutex
		getContactCalls    int
		updateContactCalls int
		lastUpdatedName    string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/rest/457/secret/crm.duplicate.findbycomm.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"CONTACT": []int64{1501},
				},
			})
		case "/rest/457/secret/crm.contact.get.json":
			mu.Lock()
			getContactCalls++
			currentName := bitrixAutoCreatedContactName
			if updateContactCalls > 0 {
				currentName = contactName
			}
			mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"ID":          "1501",
					"NAME":        currentName,
					"SECOND_NAME": "",
					"LAST_NAME":   "",
					"COMPANY_ID":  "",
					"DATE_MODIFY": "2026-04-07T10:15:00+03:00",
					"PHONE": []map[string]any{
						{
							"ID":         "5065",
							"TYPE_ID":    "PHONE",
							"VALUE_TYPE": "WORK",
							"VALUE":      "+79040002517",
						},
					},
				},
			})
		case "/rest/457/secret/crm.contact.update.json":
			var body struct {
				Fields map[string]any `json:"fields"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("не удалось прочитать запрос crm.contact.update: %v", err)
			}

			mu.Lock()
			updateContactCalls++
			if name, ok := body.Fields["NAME"].(string); ok {
				lastUpdatedName = name
			}
			mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		default:
			t.Fatalf("неожиданный метод Bitrix24: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:   true,
		RequestTimeout:        2 * time.Second,
		BitrixBaseURL:         server.URL + "/rest/457/secret",
		BitrixOriginatorID:    "ETALON_SD",
		BitrixCategoryID:      17,
		BitrixRateLimitPerMin: 120,
		BitrixRateLimitBurst:  50,
	}
	svc := &bitrixSyncService{
		cfg:           cfg,
		log:           logger.New("", "test", "error", true),
		client:        b24.NewClient(cfg, logger.New("", "test", "error", true)),
		telephonyRepo: telephonyRepo,
	}

	pointID := int64(17)
	ticket := &tickets.Ticket{
		Status:               tickets.StatusNew,
		Type:                 tickets.TypeIncident,
		ContactID:            &localContact.ID,
		BitrixServicePointID: &pointID,
	}
	ticket.ID = "ticket-1"

	fields, err := svc.buildDealFields(ctx, ticket)
	if err != nil {
		t.Fatalf("buildDealFields завершился ошибкой: %v", err)
	}

	gotContactID, ok := fields["CONTACT_ID"].(int64)
	if !ok {
		t.Fatalf("ожидали CONTACT_ID типа int64, получили %T", fields["CONTACT_ID"])
	}
	if gotContactID != 1501 {
		t.Fatalf("ожидали CONTACT_ID=1501, получили %d", gotContactID)
	}

	updatedLocalContact, err := telephonyRepo.GetContactByID(ctx, localContact.ID)
	if err != nil {
		t.Fatalf("не удалось перечитать локальный контакт: %v", err)
	}
	if updatedLocalContact == nil || updatedLocalContact.BitrixContactID == nil {
		t.Fatalf("ожидали сохранение bitrix_contact_id в локальном контакте")
	}
	if *updatedLocalContact.BitrixContactID != "1501" {
		t.Fatalf("ожидали bitrix_contact_id=1501, получили %q", *updatedLocalContact.BitrixContactID)
	}
	if updatedLocalContact.Name == nil || *updatedLocalContact.Name != contactName {
		actualName := "<nil>"
		if updatedLocalContact.Name != nil {
			actualName = *updatedLocalContact.Name
		}
		t.Fatalf("ожидали имя локального контакта %q, получили %q", contactName, actualName)
	}

	mu.Lock()
	gotGetCalls := getContactCalls
	gotUpdateCalls := updateContactCalls
	gotUpdatedName := lastUpdatedName
	mu.Unlock()

	if gotGetCalls < 2 {
		t.Fatalf("ожидали минимум два чтения crm.contact.get, получили %d", gotGetCalls)
	}
	if gotUpdateCalls != 1 {
		t.Fatalf("ожидали один вызов crm.contact.update, получили %d", gotUpdateCalls)
	}
	if gotUpdatedName != contactName {
		t.Fatalf("ожидали обновление имени контакта до %q, получили %q", contactName, gotUpdatedName)
	}
}

func TestBitrixSyncServiceUpsertDealAndLinkSetsDealContactItems(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:bitrix-deal-contact-sync?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть sqlite: %v", err)
	}
	if err := db.AutoMigrate(&telephony.Contact{}, &bitrix.DealLink{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	telephonyRepo := repositories.NewTelephonyRepo(db)
	bitrixRepo := repositories.NewBitrixRepo(db)
	ctx := context.Background()
	contactName := "Наталья бухгалтер"
	bitrixContactID := "1501"
	localContact, err := telephonyRepo.UpsertContact(ctx, telephony.ContactUpsert{
		PhoneNormalized: "+79280371097",
		PhoneDisplay:    "+79280371097",
		Name:            &contactName,
		BitrixContactID: &bitrixContactID,
	})
	if err != nil {
		t.Fatalf("не удалось сохранить локальный контакт: %v", err)
	}

	ticketID := "ticket-contact-sync"
	if err := bitrixRepo.UpsertDealLink(ctx, &bitrix.DealLink{
		TicketID:   ticketID,
		B24DealID:  6299,
		LastSyncAt: time.Now(),
	}); err != nil {
		t.Fatalf("не удалось подготовить связь сделки: %v", err)
	}

	var (
		mu              sync.Mutex
		updateCalls     int
		contactSetCalls int
		lastContactSet  []map[string]any
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/rest/457/secret/crm.contact.get.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"ID":          "1501",
					"NAME":        contactName,
					"SECOND_NAME": "",
					"LAST_NAME":   "",
					"DATE_MODIFY": "2026-04-07T10:15:00+03:00",
					"PHONE": []map[string]any{
						{
							"ID":         "5065",
							"TYPE_ID":    "PHONE",
							"VALUE_TYPE": "WORK",
							"VALUE":      "+79280371097",
						},
					},
				},
			})
		case "/rest/457/secret/crm.deal.update.json":
			mu.Lock()
			updateCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		case "/rest/457/secret/crm.deal.contact.items.get.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{"CONTACT_ID": "2400", "IS_PRIMARY": "Y", "SORT": 10},
				},
			})
		case "/rest/457/secret/crm.deal.contact.items.set.json":
			var body struct {
				ID    int64            `json:"id"`
				Items []map[string]any `json:"items"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("не удалось прочитать запрос crm.deal.contact.items.set: %v", err)
			}
			if body.ID != 6299 {
				t.Fatalf("ожидали установку контактов сделки 6299, получили %d", body.ID)
			}
			mu.Lock()
			contactSetCalls++
			lastContactSet = body.Items
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		default:
			t.Fatalf("неожиданный метод Bitrix24: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:   true,
		RequestTimeout:        2 * time.Second,
		BitrixBaseURL:         server.URL + "/rest/457/secret",
		BitrixOriginatorID:    "ETALON_SD",
		BitrixCategoryID:      17,
		BitrixRateLimitPerMin: 120,
		BitrixRateLimitBurst:  50,
	}
	svc := &bitrixSyncService{
		cfg:           cfg,
		log:           logger.New("", "test", "error", true),
		client:        b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:          bitrixRepo,
		telephonyRepo: telephonyRepo,
	}

	pointID := int64(17)
	ticket := &tickets.Ticket{
		Number:               1001,
		Status:               tickets.StatusNew,
		Type:                 tickets.TypeIncident,
		SyncWithBitrix:       true,
		ContactID:            &localContact.ID,
		BitrixServicePointID: &pointID,
	}
	ticket.ID = ticketID

	dealID, err := svc.upsertDealAndLink(ctx, ticket)
	if err != nil {
		t.Fatalf("upsertDealAndLink завершился ошибкой: %v", err)
	}
	if dealID != 6299 {
		t.Fatalf("ожидали dealID=6299, получили %d", dealID)
	}

	mu.Lock()
	gotUpdateCalls := updateCalls
	gotContactSetCalls := contactSetCalls
	gotItems := lastContactSet
	mu.Unlock()

	if gotUpdateCalls != 1 {
		t.Fatalf("ожидали один crm.deal.update, получили %d", gotUpdateCalls)
	}
	if gotContactSetCalls != 1 {
		t.Fatalf("ожидали один crm.deal.contact.items.set, получили %d", gotContactSetCalls)
	}
	if len(gotItems) != 2 {
		t.Fatalf("ожидали два контакта в сделке, получили %v", gotItems)
	}
	if gotItems[0]["CONTACT_ID"] != float64(1501) || gotItems[0]["IS_PRIMARY"] != "Y" {
		t.Fatalf("ожидали привязанный контакт тикета первичным, получили %v", gotItems[0])
	}
	if gotItems[1]["CONTACT_ID"] != float64(2400) || gotItems[1]["IS_PRIMARY"] != "N" {
		t.Fatalf("ожидали сохранение существующего контакта вторым, получили %v", gotItems[1])
	}
}
