package utils

import (
	"fmt"
	"net"
	"regexp"
	"strings"
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
	// Убираем возможные пробелы на концах строки
	dateStr = strings.TrimSpace(dateStr)
	t, err := time.Parse(TimeLayoutAgent, dateStr)
	if err != nil {
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
func NormalizeRNKKT(rnm string) string {
	return nonDigitRegex.ReplaceAllString(rnm, "")
}

// FormatRNKKT форматирует чистый РН ККТ для вывода в ServiceDesk, добавляя пробелы.
func FormatRNKKT(rnm string) string {
	cleanRnm := NormalizeRNKKT(rnm)
	if len(cleanRnm) != 16 {
		return cleanRnm // Возвращаем как есть, если длина некорректна
	}
	return fmt.Sprintf("%s %s %s %s", cleanRnm[0:4], cleanRnm[4:8], cleanRnm[8:12], cleanRnm[12:16])
}

// IsPrivateIP проверяет, является ли IP-адрес приватным (локальным).
func IsPrivateIP(ipStr string) (bool, error) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false, fmt.Errorf("некорректный IP-адрес: %s", ipStr)
	}

	_, private24, _ := net.ParseCIDR("10.0.0.0/8")
	_, private20, _ := net.ParseCIDR("172.16.0.0/12")
	_, private16, _ := net.ParseCIDR("192.168.0.0/16")

	return private24.Contains(ip) || private20.Contains(ip) || private16.Contains(ip), nil
}
