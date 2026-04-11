package gateways

import (
	"context"
	"fmt"
	"strings"
	"testing"

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

func gatewayTestSQLiteDSN(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf(
		"file:%s?mode=memory&cache=shared",
		strings.NewReplacer("/", "_", "\\", "_", " ", "_").Replace(t.Name()),
	)
}
