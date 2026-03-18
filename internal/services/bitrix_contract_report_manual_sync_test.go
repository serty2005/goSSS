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
	if !testLog.Contains("выбран более заполненный элемент") {
		t.Fatalf("ожидалась диагностическая запись о выборе более заполненного дубля, логи: %#v", testLog.Messages())
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
