package contract

import (
	"archive/zip"
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestParseContractReportHTML(t *testing.T) {
	t.Helper()

	content := []byte(`
		<html><body>
		<table>
			<tr>
				<th>Идентификатор контрагента</th>
				<th>Точка обслуживания.Код</th>
				<th>Точка обслуживания</th>
				<th>Обслуживается</th>
				<th>Бесплатное обслуживание</th>
				<th>Дата начала</th>
				<th>Дата окончания</th>
				<th>Заказ клиента</th>
			</tr>
			<tr>
				<td>36860ee6-880b-11f0-a430-8e166aa88cae</td>
				<td>12345</td>
				<td>Кафе на Рябиновой</td>
				<td>Да</td>
				<td>Да</td>
				<td>01.03.2026</td>
				<td>31.12.2026</td>
				<td>Заказ-1</td>
			</tr>
		</table>
		</body></html>
	`)

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

func TestParseContractReportZIP_ReadsAllSupportedReports(t *testing.T) {
	now := time.Date(2026, time.March, 12, 12, 0, 0, 0, time.UTC)
	content := buildContractReportZIP(t, map[string]string{
		"company-a.html": legacyContractReportHTML("111", "Кафе на Рябиновой"),
		"company-b.html": legacyContractReportHTML("222", "Бар на Лесной"),
	})

	rows, err := parseContractReportZIP(nil, "reports.zip", content, now)
	if err != nil {
		t.Fatalf("не удалось разобрать zip-архив с несколькими отчётами: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("ожидали две строки из двух внутренних отчётов, получили %d", len(rows))
	}

	rowByName := make(map[string]ContractReportRow, len(rows))
	for _, row := range rows {
		rowByName[row.ServicePointName] = row
	}
	if rowByName["Кафе на Рябиновой"].ServicePointCode != "ru111" {
		t.Fatalf("не найдена строка первого внутреннего отчёта: %+v", rowByName)
	}
	if rowByName["Бар на Лесной"].ServicePointCode != "ru222" {
		t.Fatalf("не найдена строка второго внутреннего отчёта: %+v", rowByName)
	}
}

func TestParseContractReportSpreadsheet_IDFormat(t *testing.T) {
	t.Helper()

	content := buildIDFormatContractReportXLSX(t)

	rows, err := parseContractReportSpreadsheet(
		"contract-report-id-format.xlsx",
		content,
		time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("не удалось разобрать xls-отчёт нового формата: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("ожидали непустой набор строк нового отчёта")
	}

	rowByName := make(map[string]ContractReportRow, len(rows))
	for _, row := range rows {
		rowByName[row.ServicePointName] = row
	}

	hedonist, ok := rowByName["Hedonist"]
	if !ok {
		t.Fatal("не найдена агрегированная строка Hedonist")
	}
	if hedonist.ServicePointCode != "id000000054" {
		t.Fatalf("ожидали код Syrve для Hedonist, получили %q", hedonist.ServicePointCode)
	}
	if hedonist.ContractType != "TS Standart" {
		t.Fatalf("ожидали тип TS Standart для Hedonist, получили %q", hedonist.ContractType)
	}
	if !hedonist.ContractOn {
		t.Fatal("ожидали активный контракт для Hedonist")
	}

	harvey, ok := rowByName["Harvey Bar"]
	if !ok {
		t.Fatal("не найдена агрегированная строка Harvey Bar")
	}
	if harvey.ServicePointCode != "id000000031" {
		t.Fatalf("ожидали код Syrve для Harvey Bar, получили %q", harvey.ServicePointCode)
	}
	if harvey.ContractType != "Не активен" {
		t.Fatalf("ожидали неактивный контракт для Harvey Bar, получили %q", harvey.ContractType)
	}
	if harvey.ContractOn {
		t.Fatal("ожидали неактивный контракт для Harvey Bar")
	}

	frangi, ok := rowByName["FRANGI.Br & Coffee"]
	if !ok {
		t.Fatal("не найдена агрегированная строка FRANGI.Br & Coffee")
	}
	if frangi.ServicePointCode != "id000000265" {
		t.Fatalf("ожидали код Syrve для FRANGI.Br & Coffee, получили %q", frangi.ServicePointCode)
	}
	if frangi.ContractType != "TS Cloud" {
		t.Fatalf("ожидали тип TS Cloud для FRANGI.Br & Coffee, получили %q", frangi.ContractType)
	}
	if !frangi.ContractOn {
		t.Fatal("ожидали активный контракт TS Cloud для FRANGI.Br & Coffee")
	}
}

func buildContractReportZIP(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	for name, content := range files {
		fileWriter, err := writer.Create(name)
		if err != nil {
			t.Fatalf("не удалось создать файл %q внутри zip: %v", name, err)
		}
		if _, err := fileWriter.Write([]byte(content)); err != nil {
			t.Fatalf("не удалось записать файл %q внутри zip: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("не удалось закрыть zip-архив: %v", err)
	}
	return out.Bytes()
}

func legacyContractReportHTML(pointCode string, pointName string) string {
	return `
		<html><body>
		<table>
			<tr>
				<th>Идентификатор контрагента</th>
				<th>Точка обслуживания.Код</th>
				<th>Точка обслуживания</th>
				<th>Обслуживается</th>
				<th>Бесплатное обслуживание</th>
				<th>Дата начала</th>
				<th>Дата окончания</th>
				<th>Заказ клиента</th>
			</tr>
			<tr>
				<td>` + pointCode + `</td>
				<td>` + pointCode + `</td>
				<td>` + pointName + `</td>
				<td>Да</td>
				<td>Нет</td>
				<td>01.03.2026</td>
				<td>31.12.2026</td>
				<td>Заказ</td>
			</tr>
		</table>
		</body></html>
	`
}

func buildIDFormatContractReportXLSX(t *testing.T) []byte {
	t.Helper()

	book := excelize.NewFile()
	defer func() { _ = book.Close() }()

	sheet := book.GetSheetName(0)
	rows := [][]string{
		{
			"Точка обслуживания.Код",
			"Точка обслуживания",
			"Обслуживается",
			"Дата окончания",
		},
		{"000000053", "Hedonist TS", "Да", "31.12.2026"},
		{"000000054", "Hedonist Syrve", "Нет", "31.12.2026"},
		{"000000030", "Harvey Bar TS", "Нет", "31.12.2025"},
		{"000000031", "Harvey Bar Syrve", "Нет", "31.12.2025"},
		{"000000264", "FRANGI.Br & Coffee TS", "Нет", "31.12.2026"},
		{"000000265", "FRANGI.Br & Coffee Syrve", "Да", "31.12.2026"},
	}
	for rowIndex, row := range rows {
		cell, err := excelize.CoordinatesToCellName(1, rowIndex+1)
		if err != nil {
			t.Fatalf("не удалось вычислить координаты ячейки: %v", err)
		}
		if err := book.SetSheetRow(sheet, cell, &row); err != nil {
			t.Fatalf("не удалось записать строку XLSX: %v", err)
		}
	}

	var out bytes.Buffer
	if _, err := book.WriteTo(&out); err != nil {
		t.Fatalf("не удалось сериализовать XLSX-отчёт: %v", err)
	}
	return out.Bytes()
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

func TestAggregateContractReportRows_PrefersServicedDuplicateByName(t *testing.T) {
	openEndedStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	expiredEnd := time.Date(2025, time.October, 31, 0, 0, 0, 0, time.UTC)
	serviced := true
	notServiced := false

	rows := AggregateContractReportRows([]ContractReportRow{
		{
			ContractorID:     "point-42",
			ServicePointCode: "OLD-42",
			ServicePointName: "Дублирующаяся точка",
			Serviced:         &notServiced,
			ContractOn:       false,
			ContractType:     "Не активен",
			EndDate:          &expiredEnd,
		},
		{
			ContractorID:     "point-42",
			ServicePointCode: "NEW-42",
			ServicePointName: "Дублирующаяся точка",
			Serviced:         &serviced,
			ContractOn:       true,
			ContractType:     "TS Standart",
			StartDate:        &openEndedStart,
		},
	})

	if len(rows) != 1 {
		t.Fatalf("ожидали одну агрегированную строку, получили %d", len(rows))
	}
	if rows[0].ServicePointCode != "NEW-42" {
		t.Fatalf("ожидали, что агрегатор оставит обслуживаемую строку, получили код %q", rows[0].ServicePointCode)
	}
	if rows[0].ContractType != "TS Standart" {
		t.Fatalf("ожидали тип TS Standart, получили %q", rows[0].ContractType)
	}
	if !rows[0].ContractOn {
		t.Fatal("ожидали активный контракт у выбранной строки")
	}
}

func TestAggregateContractReportRows_DoesNotMergeDifferentSourcesWithSameName(t *testing.T) {
	rows := AggregateContractReportRows([]ContractReportRow{
		{
			ContractorID:     "ru000111",
			ServicePointCode: "ru000111",
			ServicePointName: "Общая точка",
			ContractType:     "TS Standart",
			ContractOn:       true,
		},
		{
			ContractorID:     "id000111",
			ServicePointCode: "id000111",
			ServicePointName: "Общая точка",
			ContractType:     "TS Cloud",
			ContractOn:       true,
		},
	})

	if len(rows) != 2 {
		t.Fatalf("ожидали две независимые строки из разных источников, получили %d", len(rows))
	}
}
