package drvfr

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type licenseInfo struct {
	quarter string
	year    int
}

var licenseMap = map[string]licenseInfo{
	"FFFFFFFF": {"4", 2027},
	"FFFFFF7F": {"3", 2027},
	"FFFFFF3F": {"2", 2027},
	"FFFFFF1F": {"1", 2027},
	"FFFFFF0F": {"4", 2026},
	"FFFFFF07": {"3", 2026},
	"FFFFFF03": {"2", 2026},
	"FFFFFF01": {"1", 2026},
	"FFFFFF00": {"4", 2025},
	"FFFF7F00": {"3", 2025},
	"FFFF3F00": {"2", 2025},
	"FFFF1F00": {"1", 2025},
	"FFFF0F00": {"4", 2024},
	"FFFF0700": {"3", 2024},
	"FFFF0300": {"2", 2024},
	"FFFF0100": {"1", 2024},
	"FFFF":     {"4", 2023},
	"FF7F":     {"3", 2023},
	"FF3F":     {"2", 2023},
	"FF1F":     {"1", 2023},
	"FF0F":     {"4", 2022},
	"FF07":     {"3", 2022},
	"FF03":     {"2", 2022},
	"FF01":     {"1", 2022},
	"FF00":     {"4", 2021},
	"7F00":     {"3", 2021},
	"3F00":     {"2", 2021},
	"1F00":     {"1", 2021},
	"0F00":     {"4", 2020},
	"0700":     {"3", 2020},
	"0300":     {"2", 2020},
	"0100":     {"1", 2020},
}

var sortedLicenseKeys = slices.SortedFunc(maps.Keys(licenseMap), func(a, b string) int {
	return cmp.Compare(len(b), len(a))
})

func decodeLicense(hex string) string {
	if hex == "" {
		return ""
	}

	upperHex := strings.ToUpper(strings.TrimSpace(hex))
	if len(upperHex) < 16 {
		return ""
	}

	subscriptionHex := upperHex[16:]
	for _, code := range sortedLicenseKeys {
		if strings.HasPrefix(subscriptionHex, code) {
			info := licenseMap[code]
			return fmt.Sprintf("Подписка до %s квартала %d года", info.quarter, info.year)
		}
	}

	return ""
}
