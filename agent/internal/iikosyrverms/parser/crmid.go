package parser

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

const maxCashServerLogReadBytes int64 = 1 << 20

var crmIDPattern = regexp.MustCompile(`ID организации\s*:\s*(\d+)`)

func ReadCRMID(path string) (string, string, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("не удалось открыть cash-server.log %q: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", "", fmt.Errorf("не удалось прочитать метаданные cash-server.log %q: %w", path, err)
	}

	start := max(0, info.Size()-maxCashServerLogReadBytes)
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return "", "", fmt.Errorf("не удалось перейти к хвосту cash-server.log %q: %w", path, err)
	}

	reader := bufio.NewReader(file)
	if start > 0 {
		if _, err := reader.ReadString('\n'); err != nil && err != io.EOF {
			return "", "", fmt.Errorf("не удалось синхронизировать чтение cash-server.log %q: %w", path, err)
		}
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), int(maxCashServerLogReadBytes))

	lastMatch := ""
	for scanner.Scan() {
		matches := crmIDPattern.FindStringSubmatch(scanner.Text())
		if len(matches) == 2 {
			lastMatch = matches[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("не удалось прочитать cash-server.log %q: %w", path, err)
	}
	if lastMatch == "" {
		return "", "cash-server.log найден, но строка с ID организации не обнаружена", nil
	}
	return lastMatch, "CRMid успешно извлечён из cash-server.log", nil
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
