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

	"etalon-server/internal/infra/logger"
	"etalon-server/internal/pkg/spreadsheet"

	"golang.org/x/net/html"
)

const (
	reportColumnContractorID = "идентификатор контрагента"
	reportColumnPointCode    = "точка обслуживания.код"
	reportColumnPointName    = "точка обслуживания"
	reportColumnServiced     = "обслуживается"
	reportColumnFree         = "бесплатное обслуживание"
	reportColumnServiceType  = "вид сервиса"
	reportColumnStartDate    = "дата начала"
	reportColumnEndDate      = "дата окончания"
	reportColumnClientOrder  = "заказ клиента"

	legacyContractReportCodePrefix = "ru"
	idContractReportCodePrefix     = "id"
)

type contractReportFormat string

const (
	contractReportFormatLegacy contractReportFormat = "legacy"
	contractReportFormatID     contractReportFormat = "id"
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
	ServicePointCode string
	ServicePointName string
	Serviced         *bool
	ContractOn       bool
	ContractType     string
	StartDate        *time.Time
	EndDate          *time.Time
	ClientOrder      string
}

// parseContractReportArchive разбирает вложение отчета в одном из поддерживаемых форматов.
func parseContractReportArchive(log logger.LoggerInterface, fileName string, content []byte, now time.Time) ([]ContractReportRow, string, error) {
	if len(content) == 0 {
		return nil, "", errors.New("вложение с контрактами пустое")
	}

	hash := sha256.Sum256(content)
	archiveHash := hex.EncodeToString(hash[:])
	if log != nil {
		log.Debug(
			"контракты: определяем формат вложения",
			"attachment_name", fileName,
			"attachment_ext", strings.ToLower(filepath.Ext(strings.TrimSpace(fileName))),
			"attachment_size", len(content),
			"attachment_hash", archiveHash,
		)
	}

	rows, err := parseContractReportFile(log, fileName, content, now)
	if err != nil {
		return nil, "", err
	}

	return rows, archiveHash, nil
}

func parseContractReportFile(log logger.LoggerInterface, fileName string, content []byte, now time.Time) ([]ContractReportRow, error) {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if log != nil {
		log.Debug(
			"контракты: выбор парсера вложения",
			"file_name", fileName,
			"file_ext", extension,
			"file_size", len(content),
		)
	}

	switch extension {
	case ".zip":
		return parseContractReportZIP(log, fileName, content, now)
	case ".html", ".htm":
		return parseContractReportHTML(content, now)
	case ".xls", ".xlsx":
		return parseContractReportSpreadsheet(fileName, content, now)
	default:
		if log != nil {
			log.Debug("контракты: расширение файла не распознано, запускаем резервные парсеры", "file_name", fileName)
		}
		if rows, err := parseContractReportSpreadsheet(fileName, content, now); err == nil {
			if log != nil {
				log.Debug("контракты: резервный парсер spreadsheet успешно отработал", "file_name", fileName, "rows_count", len(rows))
			}
			return rows, nil
		}
		if rows, err := parseContractReportHTML(content, now); err == nil {
			if log != nil {
				log.Debug("контракты: резервный парсер html успешно отработал", "file_name", fileName, "rows_count", len(rows))
			}
			return rows, nil
		}
		return nil, fmt.Errorf("формат вложения %q не поддерживается", fileName)
	}
}

func parseContractReportZIP(log logger.LoggerInterface, fileName string, content []byte, now time.Time) ([]ContractReportRow, error) {
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть zip-архив %q: %w", fileName, err)
	}
	if log != nil {
		log.Debug("контракты: zip-архив открыт", "archive_name", fileName, "files_total", len(reader.File))
	}

	files := prioritizeReportFiles(reader.File)
	if log != nil {
		log.Debug("контракты: выбраны кандидаты внутри архива", "archive_name", fileName, "candidate_files", zipFileNames(files))
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("в архиве %q не найден поддерживаемый отчет", fileName)
	}

	parsedRows := make([]ContractReportRow, 0, 128)
	var lastErr error
	for _, file := range files {
		if file == nil {
			continue
		}
		if log != nil {
			log.Debug(
				"контракты: пробуем файл из архива",
				"archive_name", fileName,
				"inner_file_name", file.Name,
				"inner_file_size", file.UncompressedSize64,
			)
		}
		rc, err := file.Open()
		if err != nil {
			lastErr = fmt.Errorf("не удалось открыть файл %q внутри архива: %w", file.Name, err)
			if log != nil {
				log.Debug("контракты: не удалось открыть файл внутри архива", "archive_name", fileName, "inner_file_name", file.Name, "error", err)
			}
			continue
		}
		fileContent, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("не удалось прочитать файл %q внутри архива: %w", file.Name, readErr)
			if log != nil {
				log.Debug("контракты: не удалось прочитать файл внутри архива", "archive_name", fileName, "inner_file_name", file.Name, "error", readErr)
			}
			continue
		}
		rows, err := parseContractReportFile(log, file.Name, fileContent, now)
		if err != nil {
			lastErr = fmt.Errorf("не удалось разобрать файл %q внутри архива: %w", file.Name, err)
			if log != nil {
				log.Debug("контракты: файл внутри архива не подошёл", "archive_name", fileName, "inner_file_name", file.Name, "error", err)
			}
			continue
		}
		if log != nil {
			log.Debug("контракты: файл внутри архива успешно разобран", "archive_name", fileName, "inner_file_name", file.Name, "rows_count", len(rows))
		}
		parsedRows = append(parsedRows, rows...)
	}

	if len(parsedRows) > 0 {
		aggregatedRows := AggregateContractReportRows(parsedRows)
		if log != nil {
			log.Debug(
				"контракты: zip-архив успешно разобран",
				"archive_name", fileName,
				"rows_count", len(aggregatedRows),
				"raw_rows_count", len(parsedRows),
			)
		}
		return aggregatedRows, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("в архиве %q не найден поддерживаемый отчет", fileName)
}

func zipFileNames(files []*zip.File) []string {
	names := make([]string, 0, len(files))
	for _, file := range files {
		if file == nil {
			continue
		}
		names = append(names, file.Name)
	}
	return names
}

func prioritizeReportFiles(files []*zip.File) []*zip.File {
	priorities := map[string]int{
		".xlsx": 0,
		".xls":  1,
		".html": 2,
		".htm":  3,
		".zip":  4,
	}

	type candidate struct {
		file     *zip.File
		priority int
	}

	candidates := make([]candidate, 0, len(files))
	for _, file := range files {
		priority, ok := priorities[strings.ToLower(filepath.Ext(strings.TrimSpace(file.Name)))]
		if !ok {
			continue
		}
		candidates = append(candidates, candidate{
			file:     file,
			priority: priority,
		})
	}

	slices.SortFunc(candidates, func(left, right candidate) int {
		if left.priority != right.priority {
			return cmp.Compare(left.priority, right.priority)
		}
		return strings.Compare(left.file.Name, right.file.Name)
	})

	result := make([]*zip.File, 0, len(candidates))
	for _, item := range candidates {
		result = append(result, item.file)
	}
	return result
}

func parseContractReportSpreadsheet(fileName string, content []byte, now time.Time) ([]ContractReportRow, error) {
	rows, err := spreadsheet.ParseRows(fileName, content)
	if err != nil {
		return nil, err
	}
	return parseContractReportTableRows(rows, now)
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

	return parseContractReportTableRows(rows, now)
}

func parseContractReportTableRows(rows [][]string, now time.Time) ([]ContractReportRow, error) {
	headerRow, headerMap, format, err := detectContractReportHeader(rows)
	if err != nil {
		return nil, err
	}

	switch format {
	case contractReportFormatLegacy:
		return parseLegacyContractReportRows(rows, headerRow, headerMap, now)
	case contractReportFormatID:
		return parseIDContractReportRows(rows, headerRow, headerMap, now)
	default:
		return nil, errors.New("формат отчёта не поддерживается")
	}
}

func parseLegacyContractReportRows(
	rows [][]string,
	headerRow int,
	headerMap map[string]int,
	now time.Time,
) ([]ContractReportRow, error) {
	parsed := make([]ContractReportRow, 0, len(rows)-headerRow-1)
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		pointCode := normalizeCell(valueByColumn(row, headerMap, reportColumnPointCode))
		contractorID := normalizeCell(valueByColumn(row, headerMap, reportColumnContractorID))
		item := ContractReportRow{
			ContractorID:     contractorID,
			ServicePointName: normalizeCell(valueByColumn(row, headerMap, reportColumnPointName)),
			ClientOrder:      normalizeCell(valueByColumn(row, headerMap, reportColumnClientOrder)),
		}
		item.ServicePointCode = applyServicePointCodePrefix(legacyContractReportCodePrefix, cmp.Or(pointCode, contractorID))
		if item.ContractorID == "" && item.ServicePointCode == "" && item.ServicePointName == "" {
			continue
		}

		serviced := parseContractStatus(valueByColumn(row, headerMap, reportColumnServiced))
		item.Serviced = serviced
		item.StartDate = parseReportDate(valueByColumn(row, headerMap, reportColumnStartDate))
		item.EndDate = parseReportDate(valueByColumn(row, headerMap, reportColumnEndDate))
		item.ContractType = detectContractType(
			valueByColumn(row, headerMap, reportColumnServiced),
			valueByColumn(row, headerMap, reportColumnFree),
		)
		item.ContractOn = resolveContractActivity(serviced, item.StartDate, item.EndDate, now)

		if item.ServicePointName == "" || item.ServicePointCode == "" {
			continue
		}
		parsed = append(parsed, item)
	}

	if len(parsed) == 0 {
		return nil, errors.New("в html-отчёте не найдено строк с контрактами")
	}
	return AggregateContractReportRows(parsed), nil
}

type groupedIDContractReportRows struct {
	DisplayName string
	TSRows      []ContractReportRow
	SyrveRows   []ContractReportRow
}

func parseIDContractReportRows(
	rows [][]string,
	headerRow int,
	headerMap map[string]int,
	now time.Time,
) ([]ContractReportRow, error) {
	grouped := make(map[string]*groupedIDContractReportRows, len(rows)-headerRow-1)
	order := make([]string, 0, len(rows)-headerRow-1)

	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		rawName := normalizeCell(valueByColumn(row, headerMap, reportColumnPointName))
		baseName, product := splitIDReportServicePointName(rawName)
		if baseName == "" || product == "" {
			continue
		}

		pointCode := applyServicePointCodePrefix(
			idContractReportCodePrefix,
			normalizeCell(valueByColumn(row, headerMap, reportColumnPointCode)),
		)
		if pointCode == "" {
			continue
		}

		serviced := parseContractStatus(valueByColumn(row, headerMap, reportColumnServiced))
		item := ContractReportRow{
			ContractorID:     pointCode,
			ServicePointCode: pointCode,
			ServicePointName: baseName,
			Serviced:         serviced,
			EndDate:          parseReportDate(valueByColumn(row, headerMap, reportColumnEndDate)),
		}
		item.ContractOn = resolveContractActivity(serviced, nil, item.EndDate, now)

		groupKey := normalizePointName(baseName)
		group := grouped[groupKey]
		if group == nil {
			group = &groupedIDContractReportRows{DisplayName: baseName}
			grouped[groupKey] = group
			order = append(order, groupKey)
		}

		switch product {
		case "ts":
			group.TSRows = append(group.TSRows, item)
		case "syrve":
			group.SyrveRows = append(group.SyrveRows, item)
		}
	}

	parsed := make([]ContractReportRow, 0, len(grouped))
	for _, key := range order {
		group := grouped[key]
		if group == nil {
			continue
		}
		tsRow := selectPreferredContractRow(group.TSRows)
		syrveRow := selectPreferredContractRow(group.SyrveRows)
		if tsRow == nil && syrveRow == nil {
			continue
		}

		contractType := detectIDReportContractType(tsRow, syrveRow)
		result := ContractReportRow{
			ServicePointName: group.DisplayName,
			ServicePointCode: preferredIDReportServicePointCode(tsRow, syrveRow),
			ContractType:     contractType,
			ContractOn:       IsServicePointContractActive(nil, contractType),
			EndDate:          preferredIDReportEndDate(contractType, tsRow, syrveRow),
		}
		result.ContractorID = result.ServicePointCode
		result.Serviced = preferredIDReportServiced(contractType, tsRow, syrveRow)
		if result.ServicePointName == "" || result.ServicePointCode == "" {
			continue
		}
		parsed = append(parsed, result)
	}

	if len(parsed) == 0 {
		return nil, errors.New("в отчёте не найдено строк с поддерживаемыми продуктами TS/Syrve")
	}
	return parsed, nil
}

// AggregateContractReportRows схлопывает повторяющиеся строки одной точки в один канонический контракт.
func AggregateContractReportRows(rows []ContractReportRow) []ContractReportRow {
	grouped := make(map[string][]ContractReportRow, len(rows))
	order := make([]string, 0, len(rows))

	for _, row := range rows {
		key := contractReportRowGroupKey(row)
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

func contractReportRowGroupKey(row ContractReportRow) string {
	source := contractReportRowSource(row)
	name := normalizePointName(row.ServicePointName)
	if name != "" {
		return source + "|" + name
	}
	code := normalizeCell(cmp.Or(row.ServicePointCode, row.ContractorID))
	if code == "" {
		return ""
	}
	return source + "|" + code
}

func contractReportRowSource(row ContractReportRow) string {
	return contractReportCodeSource(cmp.Or(row.ServicePointCode, row.ContractorID))
}

func contractReportCodeSource(code string) string {
	normalizedCode := strings.ToLower(normalizeCell(code))
	switch {
	case strings.HasPrefix(normalizedCode, idContractReportCodePrefix):
		return idContractReportCodePrefix
	case strings.HasPrefix(normalizedCode, legacyContractReportCodePrefix):
		return legacyContractReportCodePrefix
	default:
		return legacyContractReportCodePrefix
	}
}

// compareContractRows задает приоритет выбора строки при агрегации дублей внутри отчета.
func compareContractRows(a, b ContractReportRow) int {
	aServiced := a.Serviced != nil && *a.Serviced
	bServiced := b.Serviced != nil && *b.Serviced
	if aServiced != bServiced {
		if aServiced {
			return -1
		}
		return 1
	}

	if a.ContractOn != b.ContractOn {
		if a.ContractOn {
			return -1
		}
		return 1
	}

	aOpenEnded := a.EndDate == nil
	bOpenEnded := b.EndDate == nil
	if aOpenEnded != bOpenEnded {
		if aOpenEnded {
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

// detectContractType определяет тип контракта по признакам обслуживания и бесплатного режима.
func detectContractType(servicedRaw string, freeServiceRaw string) string {
	serviced := parseContractStatus(servicedRaw)
	if serviced == nil || !*serviced {
		return "Не активен"
	}

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
func detectContractReportHeader(rows [][]string) (int, map[string]int, contractReportFormat, error) {
	legacyRequiredColumns := []string{
		reportColumnContractorID,
		reportColumnPointName,
		reportColumnServiced,
		reportColumnFree,
		reportColumnStartDate,
		reportColumnEndDate,
		reportColumnClientOrder,
	}
	idRequiredColumns := []string{
		reportColumnPointCode,
		reportColumnPointName,
		reportColumnServiced,
		reportColumnEndDate,
	}

	for rowIndex, row := range rows {
		headerMap := make(map[string]int, len(row))
		for columnIndex, cell := range row {
			headerMap[strings.ToLower(normalizeCell(cell))] = columnIndex
		}

		allLegacyColumnsPresent := true
		for _, column := range legacyRequiredColumns {
			if _, exists := headerMap[column]; !exists {
				allLegacyColumnsPresent = false
				break
			}
		}
		if allLegacyColumnsPresent {
			return rowIndex, headerMap, contractReportFormatLegacy, nil
		}

		allIDColumnsPresent := true
		for _, column := range idRequiredColumns {
			if _, exists := headerMap[column]; !exists {
				allIDColumnsPresent = false
				break
			}
		}
		if allIDColumnsPresent {
			return rowIndex, headerMap, contractReportFormatID, nil
		}
	}

	return 0, nil, "", errors.New("в отчёте не найдены обязательные колонки")
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

func applyServicePointCodePrefix(prefix string, rawCode string) string {
	code := normalizeCell(rawCode)
	if code == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(code), strings.ToLower(prefix)) {
		return code
	}
	return prefix + code
}

func splitIDReportServicePointName(name string) (string, string) {
	normalizedName := normalizeCell(name)
	if normalizedName == "" {
		return "", ""
	}

	parts := strings.Fields(normalizedName)
	if len(parts) == 0 {
		return "", ""
	}

	suffix := normalizePointName(parts[len(parts)-1])
	switch suffix {
	case "ts":
		return normalizeCell(strings.Join(parts[:len(parts)-1], " ")), "ts"
	case "syrve", "syr", "syrv", "sy":
		return normalizeCell(strings.Join(parts[:len(parts)-1], " ")), "syrve"
	default:
		return "", ""
	}
}

func selectPreferredContractRow(rows []ContractReportRow) *ContractReportRow {
	if len(rows) == 0 {
		return nil
	}
	items := slices.Clone(rows)
	slices.SortFunc(items, compareContractRows)
	return &items[0]
}

func detectIDReportContractType(tsRow, syrveRow *ContractReportRow) string {
	if tsRow != nil && tsRow.ContractOn {
		return "TS Standart"
	}
	if syrveRow != nil && syrveRow.ContractOn {
		return "TS Cloud"
	}
	return "Не активен"
}

func preferredIDReportServicePointCode(tsRow, syrveRow *ContractReportRow) string {
	switch {
	case syrveRow != nil && strings.TrimSpace(syrveRow.ServicePointCode) != "":
		return syrveRow.ServicePointCode
	case tsRow != nil:
		return tsRow.ServicePointCode
	default:
		return ""
	}
}

func preferredIDReportEndDate(contractType string, tsRow, syrveRow *ContractReportRow) *time.Time {
	switch contractType {
	case "TS Standart":
		if tsRow != nil && tsRow.EndDate != nil {
			return tsRow.EndDate
		}
		if syrveRow != nil {
			return syrveRow.EndDate
		}
	case "TS Cloud":
		if syrveRow != nil && syrveRow.EndDate != nil {
			return syrveRow.EndDate
		}
		if tsRow != nil {
			return tsRow.EndDate
		}
	default:
		return latestContractEndDate(tsRow, syrveRow)
	}
	return nil
}

func preferredIDReportServiced(contractType string, tsRow, syrveRow *ContractReportRow) *bool {
	switch contractType {
	case "TS Standart":
		if tsRow != nil {
			return tsRow.Serviced
		}
	case "TS Cloud":
		if syrveRow != nil {
			return syrveRow.Serviced
		}
	default:
		value := false
		return &value
	}
	return nil
}

func latestContractEndDate(rows ...*ContractReportRow) *time.Time {
	var latest *time.Time
	for _, row := range rows {
		if row == nil || row.EndDate == nil {
			continue
		}
		if latest == nil || row.EndDate.After(*latest) {
			latest = row.EndDate
		}
	}
	return latest
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
