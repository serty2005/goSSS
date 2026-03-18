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

func ptrInt64(value int64) *int64 {
	return &value
}
