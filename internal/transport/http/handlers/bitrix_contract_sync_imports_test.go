package handlers

import (
	"encoding/json"
	"testing"
	"time"

	contractdom "etalon-server/internal/domain/contract"
	contractsvc "etalon-server/internal/services/contract"

	"gorm.io/datatypes"
)

func TestFindActiveReportImports_FiltersStaleRowsFromMixedSourceImport(t *testing.T) {
	latest := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	older := time.Date(2026, time.March, 11, 10, 0, 0, 0, time.UTC)
	items := []contractdom.MailImport{
		buildHandlerTestMailImport(t, "id-new", "id-new.xlsx", &latest, []contractsvc.ContractReportRow{
			{
				ServicePointCode: "id200",
				ContractorID:     "id200",
				ServicePointName: "Новая ID-точка",
				ContractType:     "TS Cloud",
				ContractOn:       true,
			},
		}),
		buildHandlerTestMailImport(t, "zip-old", "mixed.zip", &older, []contractsvc.ContractReportRow{
			{
				ServicePointCode: "ru100",
				ContractorID:     "ru100",
				ServicePointName: "Актуальная RU-точка из ZIP",
				ContractType:     "TS Standart",
				ContractOn:       true,
			},
			{
				ServicePointCode: "id100",
				ContractorID:     "id100",
				ServicePointName: "Устаревшая ID-точка из ZIP",
				ContractType:     "Не активен",
				ContractOn:       false,
			},
		}),
	}

	activeImports, rows, err := findActiveReportImports(items)
	if err != nil {
		t.Fatalf("findActiveReportImports завершился ошибкой: %v", err)
	}
	if len(activeImports) != 2 {
		t.Fatalf("ожидали два активных импорта для покрытия id и ru, получили %d", len(activeImports))
	}

	rowByName := make(map[string]contractsvc.ContractReportRow, len(rows))
	for _, row := range rows {
		rowByName[row.ServicePointName] = row
	}
	if _, ok := rowByName["Новая ID-точка"]; !ok {
		t.Fatal("новая строка source id не попала в активный набор")
	}
	if _, ok := rowByName["Актуальная RU-точка из ZIP"]; !ok {
		t.Fatal("строка source ru из ZIP не попала в активный набор")
	}
	if _, ok := rowByName["Устаревшая ID-точка из ZIP"]; ok {
		t.Fatal("устаревшая строка source id из ZIP не должна попадать в активный набор")
	}
}

func buildHandlerTestMailImport(
	t *testing.T,
	hash string,
	name string,
	processedAt *time.Time,
	rows []contractsvc.ContractReportRow,
) contractdom.MailImport {
	t.Helper()

	rawRows, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("не удалось сериализовать строки отчета: %v", err)
	}
	return contractdom.MailImport{
		AttachmentName: name,
		AttachmentHash: hash,
		Status:         contractdom.MailImportStatusProcessed,
		ProcessedAt:    processedAt,
		ReportRows:     datatypes.JSON(rawRows),
	}
}
