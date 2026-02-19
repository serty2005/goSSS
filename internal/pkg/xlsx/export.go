package xlsx

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
)

// BuildWorkbook формирует xlsx-файл по заголовкам и строкам.
func BuildWorkbook(sheetName string, headers []string, rows [][]string) ([]byte, error) {
	book := excelize.NewFile()
	defer func() {
		_ = book.Close()
	}()

	if sheetName == "" {
		sheetName = "Отчет"
	}

	defaultSheet := book.GetSheetName(0)
	book.SetSheetName(defaultSheet, sheetName)

	for i, header := range headers {
		cell, err := excelize.CoordinatesToCellName(i+1, 1)
		if err != nil {
			return nil, fmt.Errorf("не удалось рассчитать ячейку заголовка: %w", err)
		}
		if err := book.SetCellValue(sheetName, cell, header); err != nil {
			return nil, fmt.Errorf("не удалось записать заголовок: %w", err)
		}
	}

	for rowIdx, row := range rows {
		for colIdx, value := range row {
			cell, err := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			if err != nil {
				return nil, fmt.Errorf("не удалось рассчитать ячейку данных: %w", err)
			}
			if err := book.SetCellValue(sheetName, cell, value); err != nil {
				return nil, fmt.Errorf("не удалось записать данные: %w", err)
			}
		}
	}

	headerStyleID, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	if err != nil {
		return nil, fmt.Errorf("не удалось создать стиль заголовка: %w", err)
	}
	if len(headers) > 0 {
		fromCell, _ := excelize.CoordinatesToCellName(1, 1)
		toCell, _ := excelize.CoordinatesToCellName(len(headers), 1)
		if err := book.SetCellStyle(sheetName, fromCell, toCell, headerStyleID); err != nil {
			return nil, fmt.Errorf("не удалось применить стиль заголовка: %w", err)
		}
	}

	for colIdx := range headers {
		maxLen := utf8.RuneCountInString(headers[colIdx])
		for _, row := range rows {
			if colIdx >= len(row) {
				continue
			}
			if l := utf8.RuneCountInString(row[colIdx]); l > maxLen {
				maxLen = l
			}
		}

		width := float64(maxLen + 2)
		if width < 12 {
			width = 12
		}
		if width > 80 {
			width = 80
		}

		colName, err := excelize.ColumnNumberToName(colIdx + 1)
		if err != nil {
			return nil, fmt.Errorf("не удалось рассчитать имя колонки: %w", err)
		}
		if err := book.SetColWidth(sheetName, colName, colName, width); err != nil {
			return nil, fmt.Errorf("не удалось установить ширину колонки: %w", err)
		}
	}

	buffer, err := book.WriteToBuffer()
	if err != nil {
		return nil, fmt.Errorf("не удалось сформировать xlsx: %w", err)
	}

	return bytes.Clone(buffer.Bytes()), nil
}
