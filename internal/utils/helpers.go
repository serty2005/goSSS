package utils

import (
	"regexp"
	"time"
)

// TimeLayoutServiceDesk определяет формат времени, используемый в ServiceDesk.
const TimeLayoutServiceDesk = "2006.01.02 15:04:05"

// TimeLayoutAgent определяет формат времени, используемый в JSON от агентов.
const TimeLayoutAgent = "2006-01-02 15:04:05"

// Regex для поиска любых символов, кроме цифр.
var nonDigitRegex = regexp.MustCompile(`\D`)

// ParseServiceDeskTime парсит строку времени из ServiceDesk.
// Возвращает nil, если строка пустая или не может быть распарсена.
func ParseServiceDeskTime(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	t, err := time.Parse(TimeLayoutServiceDesk, dateStr)
	if err != nil {
		return nil
	}
	return &t
}

// ParseAgentTime парсит строку времени из файла агента.
func ParseAgentTime(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	t, err := time.Parse(TimeLayoutAgent, dateStr)
	if err != nil {
		// Попробуем также формат ServiceDesk на всякий случай
		t, err2 := time.Parse(TimeLayoutServiceDesk, dateStr)
		if err2 != nil {
			return nil
		}
		return &t
	}
	return &t
}

// FormatFFDVersion преобразует версию ФФД из формата агента в эталонный.
func FormatFFDVersion(rawVersion string) string {
	switch rawVersion {
	case "120":
		return "1.2"
	case "105":
		return "1.05"
	default:
		// Возвращаем как есть, если формат неизвестен
		return rawVersion
	}
}

// SafeStringDereference безопасно разыменовывает указатель на строку.
func SafeStringDereference(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// NormalizeRNKKT очищает регистрационный номер ККТ, оставляя только цифры.
// "0007 2066 3405 9671" -> "0007206634059671"
func NormalizeRNKKT(rnm string) string {
	return nonDigitRegex.ReplaceAllString(rnm, "")
}

// FormatRNKKT форматирует чистый РН ККТ для вывода, добавляя пробелы.
// "0007206634059671" -> "0007 2066 3405 9671"
func FormatRNKKT(rnm string) string {
	if len(rnm) != 16 {
		return rnm // Возвращаем как есть, если длина некорректна
	}
	return rnm[0:4] + " " + rnm[4:8] + " " + rnm[8:12] + " " + rnm[12:16]
}
