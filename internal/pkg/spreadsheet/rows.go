package spreadsheet

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	xlsreader "github.com/shakinm/xlsReader/xls"
	"github.com/xuri/excelize/v2"
)

// ParseRows разбирает табличный файл Excel-подобного формата и возвращает строки первого листа.
func ParseRows(fileName string, content []byte) ([][]string, error) {
	if len(content) == 0 {
		return nil, errors.New("файл пустой")
	}

	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(fileName)))
	if ext == ".xls" {
		rows, err := parseXLSRows(content)
		if err == nil {
			return rows, nil
		}
		rows, xlsxErr := parseXLSXRows(content)
		if xlsxErr == nil {
			return rows, nil
		}
		return nil, fmt.Errorf("не удалось разобрать XLS файл: %v", err)
	}

	if ext == ".xlsx" || ext == ".xlsm" || ext == ".xltx" || ext == ".xltm" {
		rows, err := parseXLSXRows(content)
		if err == nil {
			return rows, nil
		}
		rows, xlsErr := parseXLSRows(content)
		if xlsErr == nil {
			return rows, nil
		}
		return nil, fmt.Errorf("не удалось разобрать XLSX файл: %v", err)
	}

	rows, err := parseXLSXRows(content)
	if err == nil {
		return rows, nil
	}
	rows, xlsErr := parseXLSRows(content)
	if xlsErr == nil {
		return rows, nil
	}

	return nil, errors.New("поддерживаются только файлы .xls и .xlsx")
}

func parseXLSXRows(content []byte) ([][]string, error) {
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	defer func() { _ = book.Close() }()

	sheets := book.GetSheetList()
	if len(sheets) == 0 {
		return nil, errors.New("в файле нет листов")
	}

	rows, err := book.GetRows(sheets[0])
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("в файле нет данных")
	}

	return rows, nil
}

func parseXLSRows(content []byte) ([][]string, error) {
	book, err := xlsreader.OpenReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}

	sheet, err := book.GetSheet(0)
	if err != nil || sheet == nil {
		return nil, errors.New("в файле нет листов")
	}

	rowCount := sheet.GetNumberRows()
	if rowCount == 0 {
		return nil, errors.New("в файле нет данных")
	}

	maxCols := 0
	for i := range rowCount {
		row, rowErr := sheet.GetRow(i)
		if rowErr != nil || row == nil {
			continue
		}
		maxCols = max(maxCols, len(row.GetCols()))
	}
	if maxCols == 0 {
		maxCols = 1
	}

	rows := make([][]string, 0, rowCount)
	for i := range rowCount {
		values := make([]string, maxCols)
		row, rowErr := sheet.GetRow(i)
		if rowErr == nil && row != nil {
			for c := range maxCols {
				cell, cellErr := row.GetCol(c)
				if cellErr != nil || cell == nil {
					continue
				}
				values[c] = normalizeCell(cell.GetString())
			}
		}
		rows = append(rows, trimRightEmpty(values))
	}

	return rows, nil
}

func trimRightEmpty(values []string) []string {
	last := len(values)
	for last > 0 {
		if normalizeCell(values[last-1]) != "" {
			break
		}
		last--
	}
	if last == 0 {
		return []string{}
	}
	return values[:last]
}

func normalizeCell(value string) string {
	replacer := strings.NewReplacer("\u00a0", " ", "\t", " ", "\r", " ", "\n", " ", "\x00", "")
	normalized := replacer.Replace(value)
	return strings.Join(strings.Fields(strings.TrimSpace(normalized)), " ")
}
