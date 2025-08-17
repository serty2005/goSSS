package validators

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

var (
	uniqueIDRegex         = regexp.MustCompile(`^\d{3}-\d{3}-\d{3}$`)
	remoteAccessIDRegex   = regexp.MustCompile(`(\d\s*){9,10}`)
	liteManagerIDRegex    = regexp.MustCompile(`MH_\d{5}`)
	iikoCloudDomainRegex  = regexp.MustCompile(`(?i)(https?://)?([a-z0-9-]+\.)?([a-z0-9-]+\.iiko\.it)`)
	syrveCloudDomainRegex = regexp.MustCompile(`(?i)(https?://)?([a-z0-9-]+\.)?([a-z0-9-]+\.syrve\.online)`)
	ipAddressRegex        = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(:(\d+))?$`)
	localDomainRegex      = regexp.MustCompile(`^[a-zA-Z0-9.-]+(:(\d+))?$`)
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

// ExtractLiteManagerID извлекает LiteManager ID из данных.
func ExtractLiteManagerID(data map[string]interface{}, fallback string) *string {
	if id, ok := data["litemanagerID"].(string); ok && liteManagerIDRegex.MatchString(id) {
		return &id
	}
	found := liteManagerIDRegex.FindString(fallback)
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
	lastIndex := strings.LastIndex(raw, "=")

	// Если "=" не найден или это последний символ в строке
	if lastIndex == -1 || lastIndex == len(raw)-1 {
		return "N/A"
	}

	// Берем подстроку после последнего "="
	idStr := raw[lastIndex+1:]

	// Дополнительно очищаем от возможных якорей (#) или других параметров (&)
	if anchorIndex := strings.Index(idStr, "#"); anchorIndex != -1 {
		idStr = idStr[:anchorIndex]
	}
	if paramIndex := strings.Index(idStr, "&"); paramIndex != -1 {
		idStr = idStr[:paramIndex]
	}

	// Проверяем, является ли полученная строка числом
	if _, err := strconv.Atoi(idStr); err == nil {
		return idStr
	}

	return "N/A"
}

// ValidateIPAddress валидирует и нормализует IP-адрес или домен.
func ValidateIPAddress(raw string) *string {
	if raw == "" {
		return nil
	}

	if matches := iikoCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 0 {
		res := fmt.Sprintf("%s:443", matches[3])
		return &res
	}

	if matches := syrveCloudDomainRegex.FindStringSubmatch(raw); len(matches) > 0 {
		res := fmt.Sprintf("%s:443", matches[3])
		return &res
	}

	if matches := ipAddressRegex.FindStringSubmatch(raw); len(matches) > 0 {
		ip := net.ParseIP(fmt.Sprintf("%s.%s.%s.%s", matches[1], matches[2], matches[3], matches[4]))
		if ip == nil {
			return nil
		}
		port := "8080"
		if matches[6] != "" {
			port = matches[6]
		}
		res := fmt.Sprintf("%s:%s", ip.String(), port)
		return &res
	}

	if localDomainRegex.MatchString(raw) {
		if !strings.Contains(raw, ":") {
			res := fmt.Sprintf("%s:8080", raw)
			return &res
		}
		return &raw
	}

	return nil
}
