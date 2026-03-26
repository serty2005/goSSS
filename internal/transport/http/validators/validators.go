package validators

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var (
	uniqueIDRegex            = regexp.MustCompile(`^\d{3}-\d{3}-\d{3}$`)
	remoteAccessIDRegex      = regexp.MustCompile(`(\d\s*){9,10}`)
	LiteManagerIDRegex       = regexp.MustCompile(`MH_\d{5}`)
	iikoCloudDomainRegex     = regexp.MustCompile(`(?i)(?:https?://)?(?:[a-z0-9-]+\.)?([a-z0-9-]+\.iiko\.it)`)
	syrveCloudDomainRegex    = regexp.MustCompile(`(?i)(?:https?://)?(?:[a-z0-9-]+\.)?([a-z0-9-]+\.syrve\.online)`)
	iikoWebAppHostRegex      = regexp.MustCompile(`(?i)\b((?:[a-z0-9-]+\.)*[a-z0-9-]+\.(?:syrve\.app|iikoweb\.ru))\b`)
	cabinetLinkIDRegex       = regexp.MustCompile(`\d+`)
	workstationRemoteIDRegex = regexp.MustCompile(`^[A-Za-z0-9._:-]{3,128}$`)
)

// ValidateUniqueID проверяет формат UniqueID.
func ValidateUniqueID(uniqueID string) *string {
	if uniqueIDRegex.MatchString(uniqueID) {
		return &uniqueID
	}
	return nil
}

// ValidateRemoteAccessID находит и нормализует ID удаленного доступа (TeamViewer, Anydesk).
func ValidateRemoteAccessID(raw string) *string {
	found := remoteAccessIDRegex.FindString(raw)
	if found == "" {
		return nil
	}
	normalized := strings.ReplaceAll(found, " ", "")
	return &normalized
}

// ValidateWorkstationRemoteID валидирует пользовательский ID удалённого доступа для РС.
func ValidateWorkstationRemoteID(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !workstationRemoteIDRegex.MatchString(raw) {
		return nil
	}
	return &raw
}

// ValidateIikoWebLink извлекает и нормализует ссылку на SyrveApp или iikoWeb.
func ValidateIikoWebLink(raw string) *string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	matches := iikoWebAppHostRegex.FindStringSubmatch(strings.ToLower(raw))
	if len(matches) < 2 {
		return nil
	}

	host := strings.TrimSpace(matches[1])
	if host == "" {
		return nil
	}

	normalized := fmt.Sprintf("https://%s/", host)
	return &normalized
}

// ExtractLiteManagerID извлекает LiteManager ID из данных.
func ExtractLiteManagerID(data map[string]interface{}, fallback string) *string {
	if id, ok := data["litemanagerID"].(string); ok && LiteManagerIDRegex.MatchString(id) {
		return &id
	}
	found := LiteManagerIDRegex.FindString(fallback)
	if found != "" {
		return &found
	}
	return nil
}

// DetermineCompanyTypeFromIP определяет тип компании ("syrve" или "iiko") по IP/домену.
func DetermineCompanyTypeFromIP(ip string) string {
	if strings.Contains(strings.ToLower(ip), "syrve") {
		return "syrve"
	}
	return "iiko"
}

// ValidateCabinetLink извлекает clientId из ссылки на личный кабинет.
func ValidateCabinetLink(raw string, companyType string) string {
	_ = companyType
	idStr := cabinetLinkIDRegex.FindString(raw)
	if idStr != "" {
		return idStr
	}
	return "N/A"
}

// BuildPartnersPortalLink формирует ссылку в партнёрский портал по clientId и ip.
func BuildPartnersPortalLink(clientID string, ip string) *string {
	if clientID == "" || clientID == "N/A" {
		return nil
	}

	var link string
	if DetermineCompanyTypeFromIP(ip) == "syrve" {
		link = fmt.Sprintf("https://pp.syrve.com/en/cabinet/client-area/index.html?clientId=%s", clientID)
	} else {
		link = fmt.Sprintf("https://pp.iiko.ru/ru/cabinet/client-area/index.html?clientId=%s", clientID)
	}
	return &link
}

// ValidateIPAddress валидирует и нормализует IP-адрес или домен.
// НОВАЯ РЕАЛИЗАЦИЯ: Использует пакет net/url для надежного парсинга.
func ValidateIPAddress(raw string) *string {
	if raw == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)

	// 1. Приоритетная проверка на специальные облачные домены.
	if matches := iikoCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 1 {
		res := fmt.Sprintf("%s:443", matches[1])
		return &res
	}
	if matches := syrveCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 1 {
		res := fmt.Sprintf("%s:443", matches[1])
		return &res
	}

	// 2. Используем url.Parse для надежного разбора.
	// Добавляем схему "http://", если она отсутствует, чтобы парсер корректно работал
	// с адресами вида "domain.com:8080".
	parseableURL := raw
	if !strings.Contains(parseableURL, "://") {
		parseableURL = "http://" + parseableURL
	}

	parsedURL, err := url.Parse(parseableURL)
	if err != nil {
		// Если даже с добавленной схемой парсинг не удался, считаем адрес невалидным.
		return nil
	}

	hostname := parsedURL.Hostname()
	port := parsedURL.Port()

	// Если не удалось извлечь хост, адрес невалидный.
	if hostname == "" {
		return nil
	}

	// 3. Формируем итоговую строку.
	var result string
	if port != "" {
		// Если порт был указан в исходной строке, используем его.
		result = fmt.Sprintf("%s:%s", hostname, port)
	} else {
		// Если порт не указан, используем порт по умолчанию 8080.
		// Это стандарт для локальных серверов iikoRMS.
		result = fmt.Sprintf("%s:8080", hostname)
	}

	return &result
}
