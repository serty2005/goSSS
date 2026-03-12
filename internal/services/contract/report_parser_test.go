package contract

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseContractReportHTML(t *testing.T) {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "test-from-1C.html"))
	if err != nil {
		t.Fatalf("не удалось прочитать пример html-отчёта: %v", err)
	}

	rows, err := parseContractReportHTML(content, time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("не удалось разобрать html-отчёт: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("ожидали непустой набор строк отчёта")
	}

	var foundCloud bool
	for _, row := range rows {
		if row.ServicePointName != "Кафе на Рябиновой" {
			continue
		}
		foundCloud = true
		if row.ContractorID != "36860ee6-880b-11f0-a430-8e166aa88cae" {
			t.Fatalf("ожидали идентификатор контрагента из отчёта, получили %q", row.ContractorID)
		}
		if row.ContractType != "TS Cloud" {
			t.Fatalf("ожидали тип контракта TS Cloud, получили %q", row.ContractType)
		}
		if !row.ContractOn {
			t.Fatal("ожидали активный контракт для строки с обслуживанием = Да")
		}
	}
	if !foundCloud {
		t.Fatal("не найдена контрольная строка 'Кафе на Рябиновой'")
	}
}

func TestAggregateContractReportRowsPrefersActiveRow(t *testing.T) {
	activeStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	activeEnd := time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC)
	inactiveEnd := time.Date(2025, time.December, 31, 0, 0, 0, 0, time.UTC)

	rows := AggregateContractReportRows([]ContractReportRow{
		{
			ContractorID:     "point-1",
			ServicePointName: "Точка 1",
			ContractOn:       false,
			ContractType:     "TS Standart",
			EndDate:          &inactiveEnd,
		},
		{
			ContractorID:     "point-1",
			ServicePointName: "Точка 1",
			ContractOn:       true,
			ContractType:     "TS Cloud",
			StartDate:        &activeStart,
			EndDate:          &activeEnd,
		},
	})

	if len(rows) != 1 {
		t.Fatalf("ожидали одну агрегированную строку, получили %d", len(rows))
	}
	if !rows[0].ContractOn {
		t.Fatal("ожидали, что агрегатор выберет активную строку")
	}
	if rows[0].ContractType != "TS Cloud" {
		t.Fatalf("ожидали тип TS Cloud после агрегации, получили %q", rows[0].ContractType)
	}
}
