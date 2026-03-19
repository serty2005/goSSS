package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/domain/company"
	"etalon-server/internal/domain/tickets"
	"etalon-server/internal/infra/config"
	"etalon-server/internal/infra/logger"
	b24 "etalon-server/internal/infra/plugins/bitrix"
	"etalon-server/internal/infra/repositories"
	contractsvc "etalon-server/internal/services/contract"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestExecuteContractReportSync_UsesFreshElementStateBeforeUpdate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}); err != nil {
		t.Fatalf("не удалось подготовить схему bitrix: %v", err)
	}

	repo := repositories.NewBitrixRepo(db)

	var (
		mu             sync.Mutex
		batchGetCalls  int
		updateCalls    int
		addCalls       int
		lastUpdateBody map[string]any
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
						},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"20": "TS Cloud",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":   "700",
						"NAME": "Точка 700",
						"PROPERTY_681": map[string]any{
							"500": "POINT-700",
						},
						"PROPERTY_361": map[string]any{
							"501": "Не активен",
						},
						"PROPERTY_777": map[string]any{
							"502": "устаревшее значение",
						},
					},
				},
				"total": 1,
			})
		case strings.HasSuffix(r.URL.Path, "/batch.json"):
			var body struct {
				Cmd map[string]string `json:"cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			isGetBatch := true
			for _, rawCmd := range body.Cmd {
				if !strings.HasPrefix(rawCmd, "lists.element.get?") {
					isGetBatch = false
					break
				}
			}

			if isGetBatch {
				mu.Lock()
				batchGetCalls++
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						"result": map[string]any{
							"element_700": []map[string]any{
								{
									"ID":   "700",
									"NAME": "Точка 700",
									"PROPERTY_681": map[string]any{
										"500": "POINT-700",
									},
									"PROPERTY_361": map[string]any{
										"501": "Не активен",
									},
									"PROPERTY_777": map[string]any{
										"7777": "актуальное значение",
									},
								},
							},
						},
						"result_error": map[string]any{},
					},
				})
				return
			}

			t.Fatalf("не ожидался batch на upsert, получено: %#v", body.Cmd)
		case strings.HasSuffix(r.URL.Path, "/lists.element.update.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			updateCalls++
			lastUpdateBody = body
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		case strings.HasSuffix(r.URL.Path, "/lists.element.add.json"):
			mu.Lock()
			addCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": 701})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    logger.New("", "test", "error", true),
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repo,
	}

	row := contractsvc.ContractReportRow{
		ContractorID:     "contractor-700",
		ServicePointCode: "POINT-700",
		ServicePointName: "Точка 700",
		ContractOn:       true,
		ContractType:     "TS Cloud",
	}

	result, err := svc.ExecuteContractReportSync(
		context.Background(),
		[]contractsvc.ContractReportRow{row},
		ContractReportSyncExecuteOptions{
			SelectedKeys: []string{contractReportSyncUpsertKey(row)},
			QueueItems: []ContractReportSyncPlanItem{
				{
					Key:                 contractReportSyncUpsertKey(row),
					Action:              ServicePointSyncActionUpdate,
					ServicePointName:    row.ServicePointName,
					ServicePointCode:    row.ServicePointCode,
					ContractType:        row.ContractType,
					B24ElementID:        ptrInt64(700),
					CurrentCode:         "POINT-700",
					CurrentContractType: "Не активен",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("ExecuteContractReportSync завершился ошибкой: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("ожидалось одно обновление, получено %d", result.Updated)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("не ожидались ошибки выполнения, получено: %#v", result.Errors)
	}

	mu.Lock()
	gotBatchGetCalls := batchGetCalls
	gotUpdateCalls := updateCalls
	gotAddCalls := addCalls
	gotBody := lastUpdateBody
	mu.Unlock()

	if gotBatchGetCalls != 1 {
		t.Fatalf("ожидался один batch-запрос за актуальными состояниями, получено %d", gotBatchGetCalls)
	}
	if gotUpdateCalls != 1 {
		t.Fatalf("ожидался один прямой вызов lists.element.update, получено %d", gotUpdateCalls)
	}
	if gotAddCalls != 0 {
		t.Fatalf("lists.element.add не должен вызываться для update, получено %d", gotAddCalls)
	}
	if gotBody["ELEMENT_ID"] != float64(700) {
		t.Fatalf("ожидался ELEMENT_ID=700, получено %#v", gotBody["ELEMENT_ID"])
	}
	fields, _ := gotBody["FIELDS"].(map[string]any)
	if fields["PROPERTY_777"] != "актуальное значение" {
		t.Fatalf("ожидалось сохранение свежего значения PROPERTY_777, получено %#v", fields["PROPERTY_777"])
	}
	if fields["PROPERTY_681"] != "POINT-700" {
		t.Fatalf("ожидалось обновление PROPERTY_681 кодом точки, получено %#v", fields["PROPERTY_681"])
	}
	if fields["PROPERTY_361"] != "20" {
		t.Fatalf("ожидался ID значения контракта 20, получено %#v", fields["PROPERTY_361"])
	}
}

func TestPreviewContractReportSync_PrefersMoreFilledDuplicateForMappedPoint(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}); err != nil {
		t.Fatalf("не удалось подготовить схему bitrix: %v", err)
	}

	repo := repositories.NewBitrixRepo(db)
	if err := repo.UpsertCompanyServicePointMapping(context.Background(), &bitrix.CompanyServicePointMapping{
		CompanyID:            "company-700",
		BitrixServicePointID: 700,
	}); err != nil {
		t.Fatalf("не удалось сохранить mapping: %v", err)
	}

	testLog := newTestLogger()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
						},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":   "700",
						"NAME": "Точка 700",
						"PROPERTY_681": map[string]any{
							"500": "POINT-700-OLD",
						},
						"PROPERTY_361": map[string]any{
							"501": "Не активен",
						},
					},
					{
						"ID":   "701",
						"NAME": "Точка 700",
						"PROPERTY_681": map[string]any{
							"502": "POINT-700-ACTUAL",
						},
						"PROPERTY_361": map[string]any{
							"503": "TS Standart",
						},
						"PROPERTY_777": map[string]any{
							"504": "Подробный адрес",
						},
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    testLog,
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repo,
	}

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-700",
		ServicePointCode: "POINT-700-OLD",
		ServicePointName: "Точка 700",
		ContractType:     "TS Standart",
	}

	preview, err := svc.PreviewContractReportSync(context.Background(), []contractsvc.ContractReportRow{row})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.ToUpdate != 1 {
		t.Fatalf("ожидалось одно обновление, получено %d", preview.ToUpdate)
	}
	if len(preview.UpsertItems) != 1 {
		t.Fatalf("ожидался один upsert-элемент, получено %d", len(preview.UpsertItems))
	}
	if preview.UpsertItems[0].B24ElementID == nil || *preview.UpsertItems[0].B24ElementID != 701 {
		t.Fatalf("ожидалось, что обновление пойдёт в более заполненный дубль 701, получено %#v", preview.UpsertItems[0].B24ElementID)
	}
	if len(preview.DeleteItems) != 1 {
		t.Fatalf("ожидался один delete-элемент, получено %d", len(preview.DeleteItems))
	}
	if preview.DeleteItems[0].B24ElementID == nil || *preview.DeleteItems[0].B24ElementID != 700 {
		t.Fatalf("ожидалось удаление старой точки 700, получено %#v", preview.DeleteItems[0].B24ElementID)
	}
	if !testLog.Contains("выбран элемент с единственным активным контрактом") {
		t.Fatalf("ожидалась диагностическая запись о выборе дубля, логи: %#v", testLog.Messages())
	}
}

func TestExecuteContractReportSync_RebindsMappingAndTicketsBeforeDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(&company.Company{}, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}, &tickets.Ticket{}); err != nil {
		t.Fatalf("не удалось подготовить схему для execute-теста: %v", err)
	}

	bitrixRepo := repositories.NewBitrixRepo(db)
	ticketRepo := repositories.NewTicketRepo(db)
	ctx := context.Background()
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            "company-700",
		BitrixServicePointID: 700,
	}); err != nil {
		t.Fatalf("не удалось сохранить исходный mapping: %v", err)
	}

	ticket := &tickets.Ticket{
		Subject:              "Тестовая заявка",
		Status:               tickets.StatusNew,
		Type:                 tickets.TypeIncident,
		CompanyID:            "company-700",
		SyncWithBitrix:       true,
		BitrixServicePointID: ptrInt64(700),
		BitrixDealTitle:      "Точка 700",
	}
	if err := ticketRepo.Create(ctx, ticket); err != nil {
		t.Fatalf("не удалось создать тикет: %v", err)
	}

	var (
		mu          sync.Mutex
		deleteCalls int
	)
	testLog := newTestLogger()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
						},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":   "700",
						"NAME": "Точка 700",
						"PROPERTY_681": map[string]any{
							"500": "POINT-700-OLD",
						},
						"PROPERTY_361": map[string]any{
							"501": "Не активен",
						},
					},
					{
						"ID":   "701",
						"NAME": "Точка 700",
						"PROPERTY_681": map[string]any{
							"502": "POINT-700-ACTUAL",
						},
						"PROPERTY_361": map[string]any{
							"503": "TS Standart",
						},
						"PROPERTY_777": map[string]any{
							"504": "Подробный адрес",
						},
					},
				},
				"total": 2,
			})
		case strings.HasSuffix(r.URL.Path, "/lists.element.delete.json"):
			mu.Lock()
			deleteCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:        cfg,
		log:        testLog,
		client:     b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:       bitrixRepo,
		ticketRepo: ticketRepo,
	}

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-700",
		ServicePointCode: "POINT-700-OLD",
		ServicePointName: "Точка 700",
		ContractType:     "TS Standart",
	}

	result, err := svc.ExecuteContractReportSync(
		ctx,
		[]contractsvc.ContractReportRow{row},
		ContractReportSyncExecuteOptions{
			SelectedKeys: []string{syncPlanDeleteKey(700)},
			QueueItems: []ContractReportSyncPlanItem{
				{
					Key:                 syncPlanDeleteKey(700),
					Action:              ServicePointSyncActionDelete,
					ServicePointName:    "Точка 700",
					ServicePointCode:    "POINT-700-OLD",
					ContractType:        "Не активен",
					CurrentName:         "Точка 700",
					CurrentCode:         "POINT-700-OLD",
					CurrentContractType: "Не активен",
					B24ElementID:        ptrInt64(700),
					IsMapped:            false,
					Reason:              "точка не сопоставлена с компанией в ServiceDesk",
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("ExecuteContractReportSync завершился ошибкой: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("не ожидались ошибки выполнения, получено: %#v", result.Errors)
	}
	if result.Deleted != 1 {
		t.Fatalf("ожидалось одно удаление, получено %d", result.Deleted)
	}

	mapping, err := bitrixRepo.GetCompanyServicePointMappingByCompanyID(ctx, "company-700")
	if err != nil {
		t.Fatalf("не удалось прочитать mapping после execute: %v", err)
	}
	if mapping == nil || mapping.BitrixServicePointID != 701 {
		t.Fatalf("ожидалось, что mapping будет перенесён на 701, получено %#v", mapping)
	}

	updatedTicket, err := ticketRepo.GetByID(ctx, ticket.ID)
	if err != nil {
		t.Fatalf("не удалось прочитать тикет после execute: %v", err)
	}
	if updatedTicket.BitrixServicePointID == nil || *updatedTicket.BitrixServicePointID != 701 {
		t.Fatalf("ожидалось, что тикет будет перепривязан на 701, получено %#v", updatedTicket.BitrixServicePointID)
	}

	mu.Lock()
	gotDeleteCalls := deleteCalls
	mu.Unlock()
	if gotDeleteCalls != 1 {
		t.Fatalf("ожидался один вызов delete в Bitrix24, получено %d", gotDeleteCalls)
	}
	if !testLog.Contains("переносим сопоставление на более заполненный дубль") {
		t.Fatalf("ожидался лог о переносе сопоставления, логи: %#v", testLog.Messages())
	}
}

func TestPreviewContractReportSync_ResolvesFullDuplicatesIntoDeleteQueue(t *testing.T) {
	ctx := t.Context()
	svc := &bitrixSyncService{
		cfg: &config.Config{
			EnableBitrixGateway:         true,
			RequestTimeout:              2 * time.Second,
			BitrixServicePointsIBlockID: 101,
			BitrixRateLimitPerMin:       120,
			BitrixRateLimitBurst:        50,
		},
		log:  logger.New("", "test", "error", true),
		repo: repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{fieldID: map[string]any{"MULTIPLE": "N"}}})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":           "1001",
						"NAME":         "Полный дубль",
						"PROPERTY_681": map[string]any{"1": "FULL-001"},
						"PROPERTY_361": map[string]any{"2": "TS Standart"},
						"PROPERTY_777": map[string]any{"3": "ул. Тестовая, 1"},
					},
					{
						"ID":           "1002",
						"NAME":         "Полный дубль",
						"PROPERTY_681": map[string]any{"4": "FULL-001"},
						"PROPERTY_361": map[string]any{"5": "TS Standart"},
						"PROPERTY_777": map[string]any{"6": "ул. Тестовая, 1"},
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc.cfg.BitrixBaseURL = server.URL + "/rest/457/secret"
	svc.client = b24.NewClient(svc.cfg, logger.New("", "test", "error", true))

	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{
		{
			ContractorID:     "company-full",
			ServicePointCode: "FULL-001",
			ServicePointName: "Полный дубль",
			ContractType:     "TS Standart",
		},
	})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 0 {
		t.Fatalf("полные дубли не должны блокировать строку, получено blocked=%d", preview.BlockedRows)
	}
	if len(preview.UpsertItems) != 0 {
		t.Fatalf("не ожидались элементы upsert для полностью совпадающих дублей, получено %d", len(preview.UpsertItems))
	}
	if len(preview.DeleteItems) != 1 {
		t.Fatalf("ожидался один кандидат на удаление, получено %d", len(preview.DeleteItems))
	}
	if !strings.Contains(preview.DeleteItems[0].Reason, "полный дубль") {
		t.Fatalf("ожидалась причина про полный дубль, получено %q", preview.DeleteItems[0].Reason)
	}
}

func TestPreviewContractReportSync_PrefersSingleActiveDuplicate(t *testing.T) {
	ctx := t.Context()
	svc := &bitrixSyncService{
		cfg: &config.Config{
			EnableBitrixGateway:         true,
			RequestTimeout:              2 * time.Second,
			BitrixServicePointsIBlockID: 101,
			BitrixRateLimitPerMin:       120,
			BitrixRateLimitBurst:        50,
		},
		log:  logger.New("", "test", "error", true),
		repo: repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{fieldID: map[string]any{"MULTIPLE": "N"}}})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":           "1101",
						"NAME":         "Точка с активным дублем",
						"PROPERTY_681": map[string]any{"1": "OLD-INACTIVE"},
						"PROPERTY_361": map[string]any{"2": "Не активен"},
					},
					{
						"ID":           "1102",
						"NAME":         "Точка с активным дублем",
						"PROPERTY_681": map[string]any{"3": "OLD-ACTIVE"},
						"PROPERTY_361": map[string]any{"4": "TS Standart"},
						"PROPERTY_777": map[string]any{"5": "ул. Активная, 2"},
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc.cfg.BitrixBaseURL = server.URL + "/rest/457/secret"
	svc.client = b24.NewClient(svc.cfg, logger.New("", "test", "error", true))

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-active",
		ServicePointCode: "NEW-ACTIVE",
		ServicePointName: "Точка с активным дублем",
		ContractType:     "TS Standart",
	}
	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{row})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 0 {
		t.Fatalf("группа с единственным активным дублем не должна блокировать строку, получено blocked=%d", preview.BlockedRows)
	}
	if preview.ToUpdate != 1 || len(preview.UpsertItems) != 1 {
		t.Fatalf("ожидалось одно обновление активной точки, получено to_update=%d items=%d", preview.ToUpdate, len(preview.UpsertItems))
	}
	if preview.UpsertItems[0].B24ElementID == nil || *preview.UpsertItems[0].B24ElementID != 1102 {
		t.Fatalf("ожидалось, что update пойдёт в активную точку 1102, получено %#v", preview.UpsertItems[0].B24ElementID)
	}
	if len(preview.DeleteItems) != 1 {
		t.Fatalf("ожидался один дубль на удаление, получено %d", len(preview.DeleteItems))
	}
	if preview.DeleteItems[0].B24ElementID == nil || *preview.DeleteItems[0].B24ElementID != 1101 {
		t.Fatalf("ожидалось удаление неактивного дубля 1101, получено %#v", preview.DeleteItems[0].B24ElementID)
	}
	if !strings.Contains(preview.DeleteItems[0].Reason, "контракт активен") {
		t.Fatalf("ожидалась причина про единственный активный контракт, получено %q", preview.DeleteItems[0].Reason)
	}
}

func TestPreviewContractReportSync_UsesNameWhenCodeSharedAcrossDifferentPoints(t *testing.T) {
	ctx := t.Context()
	svc := &bitrixSyncService{
		cfg: &config.Config{
			EnableBitrixGateway:         true,
			RequestTimeout:              2 * time.Second,
			BitrixServicePointsIBlockID: 101,
			BitrixRateLimitPerMin:       120,
			BitrixRateLimitBurst:        50,
		},
		log:  logger.New("", "test", "error", true),
		repo: repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{fieldID: map[string]any{"MULTIPLE": "N"}}})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":           "1201",
						"NAME":         "Точка Альфа",
						"PROPERTY_681": map[string]any{"1": "SHARED-001"},
						"PROPERTY_361": map[string]any{"2": "Не активен"},
					},
					{
						"ID":           "1202",
						"NAME":         "Точка Бета",
						"PROPERTY_681": map[string]any{"3": "SHARED-001"},
						"PROPERTY_361": map[string]any{"4": "TS Standart"},
					},
				},
				"total": 2,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc.cfg.BitrixBaseURL = server.URL + "/rest/457/secret"
	svc.client = b24.NewClient(svc.cfg, logger.New("", "test", "error", true))

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-alpha",
		ServicePointCode: "SHARED-001",
		ServicePointName: "Точка Альфа",
		ContractType:     "TS Standart",
	}
	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{row})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 0 {
		t.Fatalf("совпадающий код у разных имён не должен блокировать строку, получено blocked=%d", preview.BlockedRows)
	}
	if preview.ToUpdate != 1 || len(preview.UpsertItems) != 1 {
		t.Fatalf("ожидалось одно обновление по имени, получено to_update=%d items=%d", preview.ToUpdate, len(preview.UpsertItems))
	}
	if preview.UpsertItems[0].B24ElementID == nil || *preview.UpsertItems[0].B24ElementID != 1201 {
		t.Fatalf("ожидалось обновление точки 1201, получено %#v", preview.UpsertItems[0].B24ElementID)
	}
	if preview.UpsertItems[0].Action != ServicePointSyncActionUpdate {
		t.Fatalf("ожидалось действие update, получено %q", preview.UpsertItems[0].Action)
	}
	if len(preview.DeleteItems) != 0 {
		t.Fatalf("не ожидались удаления для разных точек с одинаковым кодом, получено %d", len(preview.DeleteItems))
	}
}

func TestPreviewContractReportSync_MatchesNameIgnoringQuotes(t *testing.T) {
	ctx := t.Context()
	svc := &bitrixSyncService{
		cfg: &config.Config{
			EnableBitrixGateway:         true,
			RequestTimeout:              2 * time.Second,
			BitrixServicePointsIBlockID: 101,
			BitrixRateLimitPerMin:       120,
			BitrixRateLimitBurst:        50,
		},
		log:  logger.New("", "test", "error", true),
		repo: repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{fieldID: map[string]any{"MULTIPLE": "N"}}})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":           "1301",
						"NAME":         "ПК 2-й Южнопортовый д12 (Москва, 2-ой Южнопортовый пер., д. 12)",
						"PROPERTY_681": map[string]any{"1": "QP-001"},
						"PROPERTY_361": map[string]any{"2": "Не активен"},
					},
				},
				"total": 1,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc.cfg.BitrixBaseURL = server.URL + "/rest/457/secret"
	svc.client = b24.NewClient(svc.cfg, logger.New("", "test", "error", true))

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-quotes",
		ServicePointCode: "QP-001",
		ServicePointName: "\"ПК\" 2-й Южнопортовый д12 (Москва, 2-ой Южнопортовый пер., д. 12)",
		ContractType:     "TS Standart",
	}
	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{row})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 0 {
		t.Fatalf("разница только в кавычках не должна блокировать строку, получено blocked=%d", preview.BlockedRows)
	}
	if preview.ToUpdate != 1 || preview.ToCreate != 0 || len(preview.UpsertItems) != 1 {
		t.Fatalf("ожидалось одно обновление по нормализованному имени, получено to_update=%d to_create=%d items=%d", preview.ToUpdate, preview.ToCreate, len(preview.UpsertItems))
	}
	if preview.UpsertItems[0].B24ElementID == nil || *preview.UpsertItems[0].B24ElementID != 1301 {
		t.Fatalf("ожидалось обновление точки 1301, получено %#v", preview.UpsertItems[0].B24ElementID)
	}
	if preview.UpsertItems[0].CurrentName != "ПК 2-й Южнопортовый д12 (Москва, 2-ой Южнопортовый пер., д. 12)" {
		t.Fatalf("ожидалось текущее имя из Bitrix24, получено %q", preview.UpsertItems[0].CurrentName)
	}
}

func TestPreviewContractReportSync_FallsBackToUniqueCodeMatchBeforeCreate(t *testing.T) {
	ctx := t.Context()
	svc := &bitrixSyncService{
		cfg: &config.Config{
			EnableBitrixGateway:         true,
			RequestTimeout:              2 * time.Second,
			BitrixServicePointsIBlockID: 101,
			BitrixRateLimitPerMin:       120,
			BitrixRateLimitBurst:        50,
		},
		log:  logger.New("", "test", "error", true),
		repo: repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{fieldID: map[string]any{"MULTIPLE": "N"}}})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":           "1401",
						"NAME":         "ПК Южнопортовый 12",
						"PROPERTY_681": map[string]any{"1": "FALLBACK-001"},
						"PROPERTY_361": map[string]any{"2": "Не активен"},
					},
				},
				"total": 1,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	svc.cfg.BitrixBaseURL = server.URL + "/rest/457/secret"
	svc.client = b24.NewClient(svc.cfg, logger.New("", "test", "error", true))

	row := contractsvc.ContractReportRow{
		ContractorID:     "company-fallback",
		ServicePointCode: "FALLBACK-001",
		ServicePointName: "Павильон Южнопортовый 12",
		ContractType:     "TS Standart",
	}
	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{row})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 0 {
		t.Fatalf("уникальное совпадение по коду не должно блокировать строку, получено blocked=%d", preview.BlockedRows)
	}
	if preview.ToUpdate != 1 || preview.ToCreate != 0 || len(preview.UpsertItems) != 1 {
		t.Fatalf("ожидалось одно обновление по fallback-коду, получено to_update=%d to_create=%d items=%d", preview.ToUpdate, preview.ToCreate, len(preview.UpsertItems))
	}
	if preview.UpsertItems[0].B24ElementID == nil || *preview.UpsertItems[0].B24ElementID != 1401 {
		t.Fatalf("ожидалось обновление точки 1401, получено %#v", preview.UpsertItems[0].B24ElementID)
	}
	if preview.UpsertItems[0].CurrentName != "ПК Южнопортовый 12" {
		t.Fatalf("ожидалось текущее имя до переименования, получено %q", preview.UpsertItems[0].CurrentName)
	}
	if len(preview.UpsertItems[0].ChangeSet) == 0 {
		t.Fatalf("ожидался набор изменений для обновления имени и контракта")
	}
}

func TestPreviewContractReportSync_ReturnsBlockedItemsWithReason(t *testing.T) {
	ctx := t.Context()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{"MULTIPLE": "N"},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": []map[string]any{
					{
						"ID":   "900",
						"NAME": "Конфликтная точка",
						"PROPERTY_681": map[string]any{
							"1": "OLD-900",
						},
						"PROPERTY_361": map[string]any{
							"2": "TS Standart",
						},
					},
					{
						"ID":   "901",
						"NAME": "Конфликтная точка",
						"PROPERTY_681": map[string]any{
							"3": "OLD-901",
						},
						"PROPERTY_361": map[string]any{
							"4": "TS Cloud",
						},
						"PROPERTY_777": map[string]any{
							"5": "Подробный адрес",
						},
					},
					{
						"ID":   "902",
						"NAME": "Конфликтная точка",
						"PROPERTY_681": map[string]any{
							"6": "OLD-902",
						},
						"PROPERTY_361": map[string]any{
							"7": "TS Standart",
						},
						"PROPERTY_778": map[string]any{
							"8": "доп. поле",
						},
					},
				},
				"total": 3,
			})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    logger.New("", "test", "error", true),
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repositories.NewBitrixRepo(mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})),
	}

	preview, err := svc.PreviewContractReportSync(ctx, []contractsvc.ContractReportRow{
		{
			ContractorID:     "company-900",
			ServicePointCode: "NEW-900",
			ServicePointName: "Конфликтная точка",
			ContractType:     "TS Standart",
		},
	})
	if err != nil {
		t.Fatalf("PreviewContractReportSync завершился ошибкой: %v", err)
	}
	if preview.BlockedRows != 1 {
		t.Fatalf("ожидалась одна заблокированная строка, получено %d", preview.BlockedRows)
	}
	if len(preview.BlockedItems) != 1 {
		t.Fatalf("ожидался один диагностический элемент блокировки, получено %d", len(preview.BlockedItems))
	}
	blocked := preview.BlockedItems[0]
	if !strings.Contains(blocked.Reason, "одинаковым названием") {
		t.Fatalf("ожидалась причина про дубли по названию, получено %q", blocked.Reason)
	}
	if blocked.ResolutionHint == "" {
		t.Fatalf("ожидалась подсказка по устранению блокировки")
	}
	if len(blocked.MatchedPointIDs) != 3 || blocked.MatchedPointIDs[0] != 900 || blocked.MatchedPointIDs[1] != 901 || blocked.MatchedPointIDs[2] != 902 {
		t.Fatalf("ожидались конфликтующие B24 ID [900 901 902], получено %#v", blocked.MatchedPointIDs)
	}
}

func TestExecuteContractReportSync_UsesBatchForMassUpdates(t *testing.T) {
	db := mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})
	repo := repositories.NewBitrixRepo(db)

	var (
		mu               sync.Mutex
		batchGetCalls    int
		batchUpdateCalls int
		directUpdateHits int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{"MULTIPLE": "N"},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			items := make([]map[string]any, 0, 11)
			for i := 0; i < 11; i++ {
				id := 800 + i
				items = append(items, map[string]any{
					"ID":   fmt.Sprintf("%d", id),
					"NAME": fmt.Sprintf("Точка %d", id),
					"PROPERTY_681": map[string]any{
						fmt.Sprintf("%d", 1000+i): fmt.Sprintf("CODE-%d", id),
					},
					"PROPERTY_361": map[string]any{
						fmt.Sprintf("%d", 2000+i): "Не активен",
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": items, "total": len(items)})
		case strings.HasSuffix(r.URL.Path, "/batch.json"):
			var body struct {
				Cmd map[string]string `json:"cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			allGet := true
			allUpdate := true
			for _, rawCmd := range body.Cmd {
				allGet = allGet && strings.HasPrefix(rawCmd, "lists.element.get?")
				allUpdate = allUpdate && strings.HasPrefix(rawCmd, "lists.element.update?")
			}

			switch {
			case allGet:
				mu.Lock()
				batchGetCalls++
				mu.Unlock()
				results := make(map[string]any, len(body.Cmd))
				for key := range body.Cmd {
					id := strings.TrimPrefix(key, "element_")
					results[key] = []map[string]any{
						{
							"ID":   id,
							"NAME": fmt.Sprintf("Точка %s", id),
							"PROPERTY_681": map[string]any{
								"1": fmt.Sprintf("CODE-%s", id),
							},
							"PROPERTY_361": map[string]any{
								"2": "Не активен",
							},
							"PROPERTY_777": map[string]any{
								"3": "служебное поле",
							},
						},
					}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						"result":       results,
						"result_error": map[string]any{},
					},
				})
			case allUpdate:
				mu.Lock()
				batchUpdateCalls++
				mu.Unlock()
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						"result":       map[string]any{},
						"result_error": map[string]any{},
					},
				})
			default:
				t.Fatalf("получен неожиданный batch-запрос: %#v", body.Cmd)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.update.json"):
			mu.Lock()
			directUpdateHits++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    logger.New("", "test", "error", true),
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repo,
	}

	rows := make([]contractsvc.ContractReportRow, 0, 11)
	queueItems := make([]ContractReportSyncPlanItem, 0, 11)
	selectedKeys := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		id := int64(800 + i)
		row := contractsvc.ContractReportRow{
			ContractorID:     fmt.Sprintf("company-%d", id),
			ServicePointCode: fmt.Sprintf("CODE-%d", id),
			ServicePointName: fmt.Sprintf("Точка %d", id),
			ContractType:     "TS Standart",
		}
		key := contractReportSyncUpsertKey(row)
		rows = append(rows, row)
		queueItems = append(queueItems, ContractReportSyncPlanItem{
			Key:                 key,
			Action:              ServicePointSyncActionUpdate,
			ServicePointName:    row.ServicePointName,
			ServicePointCode:    row.ServicePointCode,
			ContractType:        row.ContractType,
			B24ElementID:        ptrInt64(id),
			CurrentCode:         row.ServicePointCode,
			CurrentContractType: "Не активен",
		})
		selectedKeys = append(selectedKeys, key)
	}

	result, err := svc.ExecuteContractReportSync(t.Context(), rows, ContractReportSyncExecuteOptions{
		SelectedKeys: selectedKeys,
		QueueItems:   queueItems,
	})
	if err != nil {
		t.Fatalf("ExecuteContractReportSync завершился ошибкой: %v", err)
	}
	if result.Updated != 11 {
		t.Fatalf("ожидалось 11 обновлений, получено %d", result.Updated)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("не ожидались ошибки выполнения, получено: %#v", result.Errors)
	}

	mu.Lock()
	gotBatchGetCalls := batchGetCalls
	gotBatchUpdateCalls := batchUpdateCalls
	gotDirectUpdateHits := directUpdateHits
	mu.Unlock()
	if gotBatchGetCalls != 1 {
		t.Fatalf("ожидался один batch-запрос для чтения актуальных состояний, получено %d", gotBatchGetCalls)
	}
	if gotBatchUpdateCalls != 1 {
		t.Fatalf("ожидался один batch update, получено %d", gotBatchUpdateCalls)
	}
	if gotDirectUpdateHits != 0 {
		t.Fatalf("прямые вызовы lists.element.update не ожидались, получено %d", gotDirectUpdateHits)
	}
}

func TestExecuteContractReportSync_UsesBatchForMassDeletes(t *testing.T) {
	db := mustOpenSyncTestDB(t, &bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{})
	repo := repositories.NewBitrixRepo(db)

	var (
		mu               sync.Mutex
		batchDeleteCalls int
		directDeleteHits int
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		defer r.Body.Close()

		switch {
		case strings.HasSuffix(r.URL.Path, "/lists.get.iblock.type.id.json"):
			_ = json.NewEncoder(w).Encode(map[string]any{"result": "lists"})
		case strings.HasSuffix(r.URL.Path, "/lists.field.get.json"):
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fieldID, _ := body["FIELD_ID"].(string)
			switch fieldID {
			case bitrixServicePointOneCCodeProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{"MULTIPLE": "N"},
					},
				})
			case bitrixServicePointContractProperty:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"result": map[string]any{
						fieldID: map[string]any{
							"MULTIPLE": "N",
							"DISPLAY_VALUES_FORM": map[string]any{
								"10": "Не активен",
								"30": "TS Standart",
							},
						},
					},
				})
			default:
				http.Error(w, "unexpected field", http.StatusBadRequest)
			}
		case strings.HasSuffix(r.URL.Path, "/lists.element.get.json"):
			items := make([]map[string]any, 0, 11)
			for i := 0; i < 11; i++ {
				id := 900 + i
				items = append(items, map[string]any{
					"ID":   fmt.Sprintf("%d", id),
					"NAME": fmt.Sprintf("Дубль %d", id),
					"PROPERTY_681": map[string]any{
						fmt.Sprintf("%d", 3000+i): fmt.Sprintf("DELETE-%d", id),
					},
					"PROPERTY_361": map[string]any{
						fmt.Sprintf("%d", 4000+i): "Не активен",
					},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": items, "total": len(items)})
		case strings.HasSuffix(r.URL.Path, "/batch.json"):
			var body struct {
				Cmd map[string]string `json:"cmd"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			allDelete := true
			for _, rawCmd := range body.Cmd {
				allDelete = allDelete && strings.HasPrefix(rawCmd, "lists.element.delete?")
			}
			if !allDelete {
				t.Fatalf("ожидался batch delete, получено %#v", body.Cmd)
			}

			mu.Lock()
			batchDeleteCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result": map[string]any{
					"result":       map[string]any{},
					"result_error": map[string]any{},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/lists.element.delete.json"):
			mu.Lock()
			directDeleteHits++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"result": true})
		default:
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	cfg := &config.Config{
		EnableBitrixGateway:         true,
		RequestTimeout:              2 * time.Second,
		BitrixBaseURL:               server.URL + "/rest/457/secret",
		BitrixServicePointsIBlockID: 101,
		BitrixRateLimitPerMin:       120,
		BitrixRateLimitBurst:        50,
	}
	svc := &bitrixSyncService{
		cfg:    cfg,
		log:    logger.New("", "test", "error", true),
		client: b24.NewClient(cfg, logger.New("", "test", "error", true)),
		repo:   repo,
	}

	queueItems := make([]ContractReportSyncPlanItem, 0, 11)
	selectedKeys := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		id := int64(900 + i)
		key := syncPlanDeleteKey(id)
		queueItems = append(queueItems, ContractReportSyncPlanItem{
			Key:                 key,
			Action:              ServicePointSyncActionDelete,
			ServicePointName:    fmt.Sprintf("Дубль %d", id),
			ServicePointCode:    fmt.Sprintf("DELETE-%d", id),
			CurrentName:         fmt.Sprintf("Дубль %d", id),
			CurrentCode:         fmt.Sprintf("DELETE-%d", id),
			CurrentContractType: "Не активен",
			B24ElementID:        ptrInt64(id),
			Reason:              "точка содержит меньше заполненных данных, чем другие дубли",
		})
		selectedKeys = append(selectedKeys, key)
	}

	result, err := svc.ExecuteContractReportSync(t.Context(), nil, ContractReportSyncExecuteOptions{
		SelectedKeys: selectedKeys,
		QueueItems:   queueItems,
	})
	if err != nil {
		t.Fatalf("ExecuteContractReportSync завершился ошибкой: %v", err)
	}
	if result.Deleted != 11 {
		t.Fatalf("ожидалось 11 удалений, получено %d", result.Deleted)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("не ожидались ошибки выполнения, получено: %#v", result.Errors)
	}

	mu.Lock()
	gotBatchDeleteCalls := batchDeleteCalls
	gotDirectDeleteHits := directDeleteHits
	mu.Unlock()
	if gotBatchDeleteCalls != 1 {
		t.Fatalf("ожидался один batch delete, получено %d", gotBatchDeleteCalls)
	}
	if gotDirectDeleteHits != 0 {
		t.Fatalf("прямые вызовы lists.element.delete не ожидались, получено %d", gotDirectDeleteHits)
	}
}

func mustOpenSyncTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("не удалось подготовить схему тестовой БД: %v", err)
	}
	return db
}

func ptrInt64(value int64) *int64 {
	return &value
}

type testLogger struct {
	mu      sync.Mutex
	records []string
}

func newTestLogger() *testLogger {
	return &testLogger{
		records: make([]string, 0, 16),
	}
}

func (l *testLogger) Debug(msg string, args ...any) {
	l.append(msg)
}

func (l *testLogger) Info(msg string, args ...any) {
	l.append(msg)
}

func (l *testLogger) Warn(msg string, args ...any) {
	l.append(msg)
}

func (l *testLogger) Error(msg string, args ...any) {
	l.append(msg)
}

func (l *testLogger) Fatal(msg string, args ...any) {
	l.append(msg)
}

func (l *testLogger) With(args ...any) logger.LoggerInterface {
	return l
}

func (l *testLogger) Contains(fragment string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.records {
		if strings.Contains(record, fragment) {
			return true
		}
	}
	return false
}

func (l *testLogger) Messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.records...)
}

func (l *testLogger) append(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, msg)
}
