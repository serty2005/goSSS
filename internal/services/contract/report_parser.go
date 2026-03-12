package contract

import (
	"archive/zip"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	reportColumnContractorID = "идентификатор контрагента"
	reportColumnPointName    = "точка обслуживания"
	reportColumnServiced     = "обслуживается"
	reportColumnFree         = "бесплатное обслуживание"
	reportColumnStartDate    = "дата начала"
	reportColumnEndDate      = "дата окончания"
	reportColumnClientOrder  = "заказ клиента"
)

type ContractMailReport struct {
	MessageID      string
	Subject        string
	ReceivedAt     *time.Time
	AttachmentName string
	AttachmentHash string
	Rows           []ContractReportRow
}

type ContractReportRow struct {
	ContractorID     string
	ServicePointName string
	ContractOn       bool
	ContractType     string
	StartDate        *time.Time
	EndDate          *time.Time
	ClientOrder      string
}

// parseContractReportArchive открывает zip-архив и извлекает из него первый подходящий HTML-отчет.
func parseContractReportArchive(fileName string, content []byte, now time.Time) ([]ContractReportRow, string, error) {
	if len(content) == 0 {
		return nil, "", errors.New("архив с контрактами пустой")
	}

	hash := sha256.Sum256(content)
	archiveHash := hex.EncodeToString(hash[:])

	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, "", fmt.Errorf("не удалось открыть zip-архив %q: %w", fileName, err)
	}

	for _, file := range reader.File {
		if !strings.EqualFold(filepath.Ext(file.Name), ".html") && !strings.EqualFold(filepath.Ext(file.Name), ".htm") {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return nil, "", fmt.Errorf("не удалось открыть файл %q внутри архива: %w", file.Name, err)
		}
		htmlContent, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, "", fmt.Errorf("не удалось прочитать файл %q внутри архива: %w", file.Name, readErr)
		}
		rows, err := parseContractReportHTML(htmlContent, now)
		if err != nil {
			return nil, "", fmt.Errorf("не удалось разобрать html-отчёт %q: %w", file.Name, err)
		}
		return rows, archiveHash, nil
	}

	return nil, "", fmt.Errorf("в архиве %q не найден html-документ", fileName)
}

// parseContractReportHTML разбирает HTML-таблицу отчета и преобразует ее в нормализованные строки.
func parseContractReportHTML(content []byte, now time.Time) ([]ContractReportRow, error) {
	doc, err := html.Parse(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	rows := collectHTMLTableRows(doc)
	if len(rows) < 2 {
		return nil, errors.New("в html-отчёте не найдена таблица с данными")
	}

	headerRow, headerMap, err := detectContractReportHeader(rows)
	if err != nil {
		return nil, err
	}

	parsed := make([]ContractReportRow, 0, len(rows)-headerRow-1)
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		item := ContractReportRow{
			ContractorID:     normalizeCell(valueByColumn(row, headerMap, reportColumnContractorID)),
			ServicePointName: normalizeCell(valueByColumn(row, headerMap, reportColumnPointName)),
			ClientOrder:      normalizeCell(valueByColumn(row, headerMap, reportColumnClientOrder)),
		}
		if item.ContractorID == "" && item.ServicePointName == "" {
			continue
		}

		serviced := parseContractStatus(valueByColumn(row, headerMap, reportColumnServiced))
		item.StartDate = parseReportDate(valueByColumn(row, headerMap, reportColumnStartDate))
		item.EndDate = parseReportDate(valueByColumn(row, headerMap, reportColumnEndDate))
		item.ContractType = detectContractType(valueByColumn(row, headerMap, reportColumnFree))
		item.ContractOn = resolveContractActivity(serviced, item.StartDate, item.EndDate, now)

		if item.ContractorID == "" || item.ServicePointName == "" {
			continue
		}
		parsed = append(parsed, item)
	}

	if len(parsed) == 0 {
		return nil, errors.New("в html-отчёте не найдено строк с контрактами")
	}

	return AggregateContractReportRows(parsed), nil
}

// AggregateContractReportRows схлопывает повторяющиеся строки одной точки в один канонический контракт.
func AggregateContractReportRows(rows []ContractReportRow) []ContractReportRow {
	grouped := make(map[string][]ContractReportRow, len(rows))
	order := make([]string, 0, len(rows))

	for _, row := range rows {
		key := normalizeCell(row.ContractorID)
		if key == "" {
			key = normalizePointName(row.ServicePointName)
		}
		if key == "" {
			continue
		}
		if _, exists := grouped[key]; !exists {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], row)
	}

	result := make([]ContractReportRow, 0, len(grouped))
	for _, key := range order {
		items := grouped[key]
		slices.SortFunc(items, compareContractRows)
		result = append(result, items[0])
	}

	return result
}

// compareContractRows задает приоритет выбора строки при агрегации дублей внутри отчета.
func compareContractRows(a, b ContractReportRow) int {
	if a.ContractOn != b.ContractOn {
		if a.ContractOn {
			return -1
		}
		return 1
	}

	aEnd := cmp.Or(timeValue(a.EndDate), timeValue(a.StartDate))
	bEnd := cmp.Or(timeValue(b.EndDate), timeValue(b.StartDate))
	if !aEnd.Equal(bEnd) {
		if aEnd.After(bEnd) {
			return -1
		}
		return 1
	}

	aStart := timeValue(a.StartDate)
	bStart := timeValue(b.StartDate)
	if !aStart.Equal(bStart) {
		if aStart.After(bStart) {
			return -1
		}
		return 1
	}

	return strings.Compare(a.ServicePointName, b.ServicePointName)
}

// timeValue безопасно разворачивает указатель на время.
func timeValue(raw *time.Time) time.Time {
	if raw == nil {
		return time.Time{}
	}
	return *raw
}

// resolveContractActivity вычисляет активность контракта по признаку обслуживания и диапазону дат.
func resolveContractActivity(serviced *bool, startDate, endDate *time.Time, now time.Time) bool {
	active := serviced != nil && *serviced
	if !active {
		return false
	}
	if startDate != nil && startDate.After(now) {
		return false
	}
	if endDate != nil && endDate.Before(now) {
		return false
	}
	return true
}

// detectContractType определяет тип контракта по признаку бесплатного обслуживания.
func detectContractType(freeServiceRaw string) string {
	freeService := parseContractStatus(freeServiceRaw)
	if freeService != nil && *freeService {
		return "TS Cloud"
	}
	return "TS Standart"
}

// parseReportDate разбирает дату формата dd.mm.yyyy из отчета 1С.
func parseReportDate(raw string) *time.Time {
	value := normalizeCell(raw)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse("02.01.2006", value)
	if err != nil {
		return nil
	}
	date := parsed.UTC()
	return &date
}

// valueByColumn возвращает значение ячейки по имени колонки из найденного заголовка.
func valueByColumn(row []string, headerMap map[string]int, columnName string) string {
	index, ok := headerMap[columnName]
	if !ok || index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

// detectContractReportHeader находит строку заголовков и индексирует обязательные колонки отчета.
func detectContractReportHeader(rows [][]string) (int, map[string]int, error) {
	requiredColumns := []string{
		reportColumnContractorID,
		reportColumnPointName,
		reportColumnServiced,
		reportColumnFree,
		reportColumnStartDate,
		reportColumnEndDate,
		reportColumnClientOrder,
	}

	for rowIndex, row := range rows {
		headerMap := make(map[string]int, len(row))
		for columnIndex, cell := range row {
			headerMap[strings.ToLower(normalizeCell(cell))] = columnIndex
		}

		allPresent := true
		for _, column := range requiredColumns {
			if _, exists := headerMap[column]; !exists {
				allPresent = false
				break
			}
		}
		if allPresent {
			return rowIndex, headerMap, nil
		}
	}

	return 0, nil, errors.New("в html-отчёте не найдены обязательные колонки")
}

// collectHTMLTableRows собирает все строки таблиц из HTML-документа в плоский набор ячеек.
func collectHTMLTableRows(root *html.Node) [][]string {
	rows := make([][]string, 0, 64)

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node == nil {
			return
		}
		if node.Type == html.ElementNode && node.Data == "tr" {
			cells := make([]string, 0, 16)
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				if child.Type != html.ElementNode || (child.Data != "td" && child.Data != "th") {
					continue
				}
				cells = append(cells, normalizeCell(extractHTMLText(child)))
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(root)
	return rows
}

// extractHTMLText извлекает текст из HTML-узла с грубой нормализацией переносов.
func extractHTMLText(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current == nil {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		if current.Type == html.ElementNode && (current.Data == "br" || current.Data == "p" || current.Data == "div") {
			builder.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

// messageIDFromMail возвращает Message-ID из заголовков письма с fallback на envelope.
func messageIDFromMail(raw *mail.Message, fallback string) string {
	if raw == nil {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(cmp.Or(raw.Header.Get("Message-ID"), fallback))
}

// normalizeCell приводит строку к каноническому виду без лишних пробелов и управляющих символов.
func normalizeCell(value string) string {
	replacer := strings.NewReplacer("\u00a0", " ", "\t", " ", "\r", " ", "\n", " ", "\x00", "")
	normalized := replacer.Replace(value)
	return strings.Join(strings.Fields(strings.TrimSpace(normalized)), " ")
}

// normalizePointName нормализует название точки обслуживания для нечувствительного сравнения.
func normalizePointName(name string) string {
	normalized := strings.ToLower(normalizeCell(name))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	return normalized
}

// parseContractStatus разбирает типовые текстовые значения признаков из отчетов 1С.
func parseContractStatus(raw string) *bool {
	value := strings.ToLower(normalizeCell(raw))
	if value == "" {
		return nil
	}

	trueWords := map[string]struct{}{
		"да":            {},
		"yes":           {},
		"true":          {},
		"1":             {},
		"активный":      {},
		"активен":       {},
		"обслуживается": {},
		"действует":     {},
	}
	falseWords := map[string]struct{}{
		"нет":              {},
		"no":               {},
		"false":            {},
		"0":                {},
		"не обслуживается": {},
		"неактивный":       {},
		"не активен":       {},
		"закрыт":           {},
	}

	if _, ok := trueWords[value]; ok {
		v := true
		return &v
	}
	if _, ok := falseWords[value]; ok {
		v := false
		return &v
	}

	if strings.Contains(value, "нет") || strings.Contains(value, "не обслуж") {
		v := false
		return &v
	}
	if strings.Contains(value, "да") || strings.Contains(value, "актив") || strings.Contains(value, "обслуж") {
		v := true
		return &v
	}

	return nil
}
