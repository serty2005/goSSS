// Файл: internal/pkg/utils/helpers.go
package utils

import (
	"etalon-server/internal/transport/http/dtos" // <-- ИЗМЕНЕНИЕ ПУТИ
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// TimeLayoutServiceDesk определяет формат времени, используемый в ServiceDesk.
const TimeLayoutServiceDesk = "2006.01.02 15:04:05"

// TimeLayoutAgent определяет формат времени, используемый в JSON от агентов.
const TimeLayoutAgent = "2006-01-02 15:04:05"

// Regex для поиска любых символов, кроме цифр.
var nonDigitRegex = regexp.MustCompile(`\D`)

// Regex для извлечения квартала и года из legacy-строки лицензии.
var legacyLicenseRegex = regexp.MustCompile(`(\d)\s*квартала\s*(\d{4})`)

var htmlTagRegex = regexp.MustCompile(`<[^>]*>`)

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

// ExtractHostFromURL извлекает хост (IP или домен) из строки вида "host:port" или "host".
func ExtractHostFromURL(address string) string {
	if strings.Contains(address, "://") {
		// Если вдруг пришел полный URL
		u, err := url.Parse(address)
		if err == nil {
			return u.Hostname()
		}
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		// Ошибка обычно означает отсутствие порта, возвращаем строку как есть
		return address
	}
	return host
}

// CalculateFRFirmware вычисляет строку для поля FRFirmware на основе лицензий.
// Обрабатывает как новый (структурированный), так и старый (строковый) формат.
func CalculateFRFirmware(licenses dtos.LicensesField) string {
	// 1. Обработка нового, структурированного формата
	if len(licenses.Structured) > 0 {
		var parts []string
		now := time.Now()
		// Устанавливаем временное окно +- 3 года от текущей даты
		threeYearsAgo := now.AddDate(-3, 0, 0)
		threeYearsFromNow := now.AddDate(3, 0, 0)

		for id, licenseInfo := range licenses.Structured {
			dateUntil := ParseAgentTime(licenseInfo.DateUntil)
			if dateUntil == nil {
				continue // Пропускаем лицензии с некорректной датой
			}

			// Проверяем, что лицензия попадает в окно +- 3 года.
			if dateUntil.After(threeYearsAgo) && dateUntil.Before(threeYearsFromNow) {
				// Форматируем в "ID:ДД.ММ.ГГГГ"
				part := fmt.Sprintf("%s:%s", id, dateUntil.Format("02.01.2006"))
				parts = append(parts, part)
			}
		}
		sort.Strings(parts)
		return strings.Join(parts, "; ")
	}

	// 2. Обработка старого, строкового формата
	if licenses.Legacy != "" {
		matches := legacyLicenseRegex.FindStringSubmatch(licenses.Legacy)
		// matches[0] - вся строка, matches[1] - квартал, matches[2] - год
		if len(matches) == 3 {
			return fmt.Sprintf("%s.%s", matches[1], matches[2])
		}
	}

	return ""
}

// StringPtr создает указатель на строку.
// Возвращает nil, если строка пустая.
func StringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// StripHTML удаляет HTML-теги из строки и обрезает лишние пробелы.
// Используется для генерации текстового превью из RTF-полей.
func StripHTML(input string) string {
	// Заменяем <br> и </p> на переносы строк или пробелы, чтобы текст не слипался
	input = strings.ReplaceAll(input, "<br>", " ")
	input = strings.ReplaceAll(input, "</p>", " ")
	input = strings.ReplaceAll(input, "</div>", " ")
	// Декодируем HTML сущности (&nbsp; и т.д.), если нужно, но пока просто удалим теги

	stripped := htmlTagRegex.ReplaceAllString(input, "")
	// Убираем двойные пробелы и лишние переносы
	stripped = strings.Join(strings.Fields(stripped), " ")
	return strings.TrimSpace(stripped)
}
