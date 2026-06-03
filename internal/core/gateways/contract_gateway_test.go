package gateways

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"etalon-server/internal/domain/bitrix"
	"etalon-server/internal/infra/repositories"
	contractsvc "etalon-server/internal/services/contract"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestContractGateway_BuildDailySnapshots_UsesBitrixPointContractStatusAndType(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(gatewayTestSQLiteDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	bitrixRepo := repositories.NewBitrixRepo(db)

	contractOn := true
	contractType := "Да"
	oneCCode := "SP-001"
	if err := db.Create(&bitrix.ServicePoint{
		B24ElementID: 501,
		Name:         "Точка обслуживания 1",
		OneCCode:     &oneCCode,
		ContractOn:   &contractOn,
		ContractType: &contractType,
	}).Error; err != nil {
		t.Fatalf("не удалось создать точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            "company-1",
		BitrixServicePointID: 501,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	gateway := &contractGatewayImpl{bitrixRepo: bitrixRepo}
	rows := []contractsvc.ContractReportRow{
		{
			ContractorID:     "SP-001",
			ServicePointCode: "SP-001",
			ServicePointName: "Точка обслуживания 1",
			ContractOn:       false,
			ContractType:     "Не активен",
		},
	}

	snapshots, stats, err := gateway.buildDailySnapshots(ctx, "hash-1", rows)
	if err != nil {
		t.Fatalf("buildDailySnapshots завершился ошибкой: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("ожидали один снимок, получили %d", len(snapshots))
	}
	if stats.MatchedRows != 1 {
		t.Fatalf("ожидали один matched row, получили %d", stats.MatchedRows)
	}

	snapshot := snapshots[0]
	if !snapshot.Active {
		t.Fatalf("ожидали активный локальный контракт по данным точки Bitrix24, получили inactive")
	}
	if snapshot.ContractType != "TS Standart" {
		t.Fatalf("ожидали нормализованный тип TS Standart, получили %q", snapshot.ContractType)
	}
}

func TestContractGateway_BuildDailySnapshots_MatchesLegacyPointByUnprefixedCode(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(gatewayTestSQLiteDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	bitrixRepo := repositories.NewBitrixRepo(db)

	oneCCode := "000123"
	if err := db.Create(&bitrix.ServicePoint{
		B24ElementID: 502,
		Name:         "Точка обслуживания 2",
		OneCCode:     &oneCCode,
	}).Error; err != nil {
		t.Fatalf("не удалось создать точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            "company-2",
		BitrixServicePointID: 502,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	gateway := &contractGatewayImpl{bitrixRepo: bitrixRepo}
	rows := []contractsvc.ContractReportRow{
		{
			ContractorID:     "legacy-row",
			ServicePointCode: "ru000123",
			ServicePointName: "Точка обслуживания 2",
			ContractType:     "TS Standart",
			ContractOn:       true,
		},
		{
			ContractorID:     "new-row",
			ServicePointCode: "id000123",
			ServicePointName: "Точка обслуживания 2",
			ContractType:     "TS Cloud",
			ContractOn:       true,
		},
	}

	snapshots, stats, err := gateway.buildDailySnapshots(ctx, "hash-2", rows)
	if err != nil {
		t.Fatalf("buildDailySnapshots завершился ошибкой: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("ожидали один снимок, получили %d", len(snapshots))
	}
	if stats.MatchedRows != 1 {
		t.Fatalf("ожидали одно сопоставление по legacy-коду, получили %d", stats.MatchedRows)
	}

	snapshot := snapshots[0]
	if snapshot.ServicePointCode != "ru000123" {
		t.Fatalf("ожидали матчинг по коду ru000123, получили %q", snapshot.ServicePointCode)
	}
	if snapshot.ContractType != "TS Standart" {
		t.Fatalf("ожидали контракт из legacy-строки, получили %q", snapshot.ContractType)
	}
}

func TestContractGateway_BuildDailySnapshots_MatchesPointByNormalizedName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(gatewayTestSQLiteDSN(t)), &gorm.Config{})
	if err != nil {
		t.Fatalf("не удалось открыть in-memory БД: %v", err)
	}

	if err := db.AutoMigrate(&bitrix.ServicePoint{}, &bitrix.CompanyServicePointMapping{}); err != nil {
		t.Fatalf("не удалось подготовить схему: %v", err)
	}

	ctx := context.Background()
	bitrixRepo := repositories.NewBitrixRepo(db)

	if err := db.Create(&bitrix.ServicePoint{
		B24ElementID: 503,
		Name:         `  Кафе  "Уют"  `,
	}).Error; err != nil {
		t.Fatalf("не удалось создать точку Bitrix24: %v", err)
	}
	if err := bitrixRepo.UpsertCompanyServicePointMapping(ctx, &bitrix.CompanyServicePointMapping{
		CompanyID:            "company-3",
		BitrixServicePointID: 503,
	}); err != nil {
		t.Fatalf("не удалось создать mapping: %v", err)
	}

	gateway := &contractGatewayImpl{bitrixRepo: bitrixRepo}
	rows := []contractsvc.ContractReportRow{
		{
			ContractorID:     "contractor-3",
			ServicePointCode: "",
			ServicePointName: "кафе уют",
			ContractType:     "TS Cloud",
			ContractOn:       true,
		},
	}

	snapshots, stats, err := gateway.buildDailySnapshots(ctx, "hash-3", rows)
	if err != nil {
		t.Fatalf("buildDailySnapshots завершился ошибкой: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("ожидали один снимок, получили %d", len(snapshots))
	}
	if stats.MatchedRows != 1 {
		t.Fatalf("ожидали матчинг по нормализованному имени, получили %d", stats.MatchedRows)
	}
	if snapshots[0].ServicePointName != "кафе уют" {
		t.Fatalf("ожидали имя точки из строки отчёта, получили %q", snapshots[0].ServicePointName)
	}
	if snapshots[0].ContractType != "TS Cloud" {
		t.Fatalf("ожидали контракт TS Cloud, получили %q", snapshots[0].ContractType)
	}
}

func TestSelectLatestReportsBySource_KeepsAllReportsFromLatestMessage(t *testing.T) {
	older := time.Date(2026, time.March, 10, 8, 0, 0, 0, time.UTC)
	latest := time.Date(2026, time.March, 11, 8, 0, 0, 0, time.UTC)
	reports := []contractsvc.ContractMailReport{
		{
			MessageID:      "older-message",
			ReceivedAt:     &older,
			AttachmentName: "old-company.html",
			AttachmentHash: "hash-old",
			Rows: []contractsvc.ContractReportRow{{
				ServicePointCode: "ru001",
				ServicePointName: "Старая точка",
			}},
		},
		{
			MessageID:      "latest-message",
			ReceivedAt:     &latest,
			AttachmentName: "company-a.html",
			AttachmentHash: "hash-a",
			Rows: []contractsvc.ContractReportRow{{
				ServicePointCode: "ru002",
				ServicePointName: "Точка А",
			}},
		},
		{
			MessageID:      "latest-message",
			ReceivedAt:     &latest,
			AttachmentName: "company-b.html",
			AttachmentHash: "hash-b",
			Rows: []contractsvc.ContractReportRow{{
				ServicePointCode: "ru003",
				ServicePointName: "Точка Б",
			}},
		},
	}

	selected := selectLatestReportsBySource(reports)
	if len(selected) != 2 {
		t.Fatalf("ожидали два отчёта из последнего письма, получили %d: %+v", len(selected), selected)
	}

	selectedByHash := make(map[string]struct{}, len(selected))
	for _, report := range selected {
		selectedByHash[report.AttachmentHash] = struct{}{}
		if report.MessageID != "latest-message" {
			t.Fatalf("ожидали только отчёты из последнего письма, получили message_id=%q", report.MessageID)
		}
	}
	if _, ok := selectedByHash["hash-a"]; !ok {
		t.Fatal("не выбран первый отчёт из последнего письма")
	}
	if _, ok := selectedByHash["hash-b"]; !ok {
		t.Fatal("не выбран второй отчёт из последнего письма")
	}
	if _, ok := selectedByHash["hash-old"]; ok {
		t.Fatal("старый отчёт того же источника не должен попадать в актуальный набор")
	}
}

func TestSelectLatestReportsBySource_FiltersStaleSourceRowsFromOlderMixedReport(t *testing.T) {
	older := time.Date(2026, time.March, 10, 8, 0, 0, 0, time.UTC)
	latest := time.Date(2026, time.March, 11, 8, 0, 0, 0, time.UTC)
	reports := []contractsvc.ContractMailReport{
		{
			MessageID:      "mixed-old",
			ReceivedAt:     &older,
			AttachmentName: "mixed.zip",
			AttachmentHash: "hash-mixed",
			Rows: []contractsvc.ContractReportRow{
				{
					ServicePointCode: "ru100",
					ContractorID:     "ru100",
					ServicePointName: "Актуальная RU-точка из ZIP",
				},
				{
					ServicePointCode: "id100",
					ContractorID:     "id100",
					ServicePointName: "Устаревшая ID-точка из ZIP",
				},
			},
		},
		{
			MessageID:      "id-new",
			ReceivedAt:     &latest,
			AttachmentName: "id-new.xlsx",
			AttachmentHash: "hash-id-new",
			Rows: []contractsvc.ContractReportRow{{
				ServicePointCode: "id200",
				ContractorID:     "id200",
				ServicePointName: "Новая ID-точка",
			}},
		},
	}

	selected := selectLatestReportsBySource(reports)
	combined := buildCombinedContractMailReport(selected)
	rowByName := make(map[string]contractsvc.ContractReportRow, len(combined.Rows))
	for _, row := range combined.Rows {
		rowByName[row.ServicePointName] = row
	}
	if _, ok := rowByName["Новая ID-точка"]; !ok {
		t.Fatal("новая строка source id не попала в объединенный отчет")
	}
	if _, ok := rowByName["Актуальная RU-точка из ZIP"]; !ok {
		t.Fatal("строка source ru из ZIP не попала в объединенный отчет")
	}
	if _, ok := rowByName["Устаревшая ID-точка из ZIP"]; ok {
		t.Fatal("устаревшая строка source id из ZIP не должна попадать в объединенный отчет")
	}
}

func gatewayTestSQLiteDSN(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(t.Name()),
	)
}
