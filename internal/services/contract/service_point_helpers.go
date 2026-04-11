package contract

import (
	"strings"

	bitrixdom "etalon-server/internal/domain/bitrix"
	contractdom "etalon-server/internal/domain/contract"
)

// NormalizeServicePointName приводит имя точки к единому виду для стабильного поиска.
func NormalizeServicePointName(name string) string {
	normalized := strings.ToLower(normalizeServicePointCell(name))
	normalized = strings.ReplaceAll(normalized, "ё", "е")
	normalized = strings.Map(func(r rune) rune {
		switch r {
		case '"', '\'', '`', '«', '»', '“', '”', '„', '‟', '‘', '’', '‚', '‛':
			return -1
		default:
			return r
		}
	}, normalized)
	return normalizeServicePointCell(normalized)
}

// BuildDailySnapshotFromBitrixServicePoint собирает доменный снимок контракта по локальной точке Bitrix24.
func BuildDailySnapshotFromBitrixServicePoint(companyID string, point bitrixdom.ServicePoint) contractdom.DailyCompanyContractSnapshot {
	contractType := NormalizeServicePointContractType(derefString(point.ContractType))
	return contractdom.DailyCompanyContractSnapshot{
		CompanyID:        strings.TrimSpace(companyID),
		ServicePointID:   point.B24ElementID,
		ServicePointName: strings.TrimSpace(point.Name),
		ServicePointCode: derefString(point.OneCCode),
		ContractorID:     derefString(point.OneCCode),
		ContractType:     contractType,
		Active:           IsServicePointContractActive(point.ContractOn, contractType),
		StartDate:        point.ContractStart,
		EndDate:          point.ContractEnd,
		ClientOrder:      derefString(point.ClientOrder),
	}
}

func normalizeServicePointCell(value string) string {
	replacer := strings.NewReplacer("\u00a0", " ", "\t", " ", "\r", " ", "\n", " ", "\x00", "")
	normalized := replacer.Replace(value)
	return strings.Join(strings.Fields(strings.TrimSpace(normalized)), " ")
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
