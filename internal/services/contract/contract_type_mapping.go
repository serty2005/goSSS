package contract

import "strings"

func NormalizeServicePointContractType(value string) string {
	normalized := normalizeServicePointContractTypeToken(value)
	switch strings.ToLower(normalized) {
	case "":
		return ""
	case "нет", "не активен", "неактивен":
		return "Не активен"
	case "да", "ts standart", "ts standard":
		return "TS Standart"
	case "ts cloud":
		return "TS Cloud"
	default:
		return normalized
	}
}

func IsServicePointContractActive(contractOn *bool, contractType string) bool {
	if contractOn != nil {
		return *contractOn
	}
	switch NormalizeServicePointContractType(contractType) {
	case "TS Cloud", "TS Standart":
		return true
	default:
		return false
	}
}

func normalizeServicePointContractTypeToken(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	normalized = strings.ReplaceAll(normalized, "Ё", "Е")
	return strings.Join(strings.Fields(normalized), " ")
}
