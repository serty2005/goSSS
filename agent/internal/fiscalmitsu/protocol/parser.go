package protocol

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"etalon-agent/internal/fiscalmitsu/domain"
)

var ffdVersionMap = map[int]string{
	1: "100",
	2: "105",
	3: "110",
	4: "120",
}

func buildPayload(model, version, regData, fnData, installedDriver string) (domain.FiscalPayload, error) {
	modelDev, err := mustExtractValue(model, "DEV=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	modelVersion, err := mustExtractValue(regData, "T1188=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	serialNumber, err := mustExtractValue(version, "SERIAL=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	rnm, err := mustExtractValue(regData, "T1037=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	organizationName, err := mustExtractValue(regData, "<T1048>")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	fnSerial, err := mustExtractValue(fnData, "FN=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	dateTimeReg, err := mustExtractValue(regData, "DATE=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	dateTimeEnd, err := mustExtractValue(fnData, "VALID=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	ofdName, err := mustExtractValue(regData, "<T1046>")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	bootVersion, err := mustExtractValue(version, "VER=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	ffdCodeRaw, err := mustExtractValue(regData, "T1209=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	ffdCode, err := strconv.Atoi(strings.TrimSpace(ffdCodeRaw))
	if err != nil {
		return domain.FiscalPayload{}, fmt.Errorf("не удалось разобрать код FFD Mitsu %q: %w", ffdCodeRaw, err)
	}
	ffdVersion, ok := ffdVersionMap[ffdCode]
	if !ok {
		return domain.FiscalPayload{}, fmt.Errorf("неподдерживаемый код FFD Mitsu %d", ffdCode)
	}
	inn, err := mustExtractValue(regData, "T1018=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	address, err := mustExtractValue(regData, "<T1009>")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	attributesRaw, err := mustExtractValue(regData, "ExtMODE=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}
	attributes, err := strconv.Atoi(strings.TrimSpace(attributesRaw))
	if err != nil {
		return domain.FiscalPayload{}, fmt.Errorf("не удалось разобрать битовую маску Mitsu ExtMODE %q: %w", attributesRaw, err)
	}
	fnExecution, err := mustExtractValue(fnData, "EDITION=")
	if err != nil {
		return domain.FiscalPayload{}, err
	}

	return domain.FiscalPayload{
		ModelName:        strings.TrimSpace(html.UnescapeString(modelDev + " " + modelVersion)),
		SerialNumber:     strings.TrimSpace(serialNumber),
		RNM:              strings.TrimSpace(rnm),
		OrganizationName: strings.TrimSpace(html.UnescapeString(organizationName)),
		FNSerial:         strings.TrimSpace(fnSerial),
		DateTimeReg:      strings.TrimSpace(dateTimeReg),
		DateTimeEnd:      strings.TrimSpace(dateTimeEnd),
		OFDName:          strings.TrimSpace(html.UnescapeString(ofdName)),
		BootVersion:      strings.TrimSpace(bootVersion),
		FFDVersion:       ffdVersion,
		INN:              strings.TrimSpace(inn),
		Address:          strings.TrimSpace(html.UnescapeString(address)),
		AttributeExcise:  formatPythonBool((attributes>>0)&1 == 1),
		AttributeMarked:  formatPythonBool((attributes>>4)&1 == 1),
		FNExecution:      strings.TrimSpace(html.UnescapeString(fnExecution)),
		InstalledDriver:  strings.TrimSpace(installedDriver),
		Licenses:         "None",
	}, nil
}

func extractValueByTag(xmlData, tag string) (string, bool) {
	cleanTag := strings.TrimSpace(tag)
	switch {
	case strings.HasPrefix(cleanTag, "<") && strings.HasSuffix(cleanTag, ">"):
		cleanTag = cleanTag[1 : len(cleanTag)-1]
	case strings.Contains(cleanTag, "="):
		cleanTag, _, _ = strings.Cut(cleanTag, "=")
	}

	if cleanTag == "" {
		return "", false
	}

	attrPattern := regexp.MustCompile(fmt.Sprintf(`%s='([^']*)'|%s="([^"]*)"`, regexp.QuoteMeta(cleanTag), regexp.QuoteMeta(cleanTag)))
	if matches := attrPattern.FindStringSubmatch(xmlData); len(matches) > 0 {
		if matches[1] != "" {
			return matches[1], true
		}
		return matches[2], true
	}

	tagPattern := regexp.MustCompile(fmt.Sprintf(`(?s)<%s>(.*?)</%s>`, regexp.QuoteMeta(cleanTag), regexp.QuoteMeta(cleanTag)))
	if matches := tagPattern.FindStringSubmatch(xmlData); len(matches) > 1 {
		return matches[1], true
	}

	return "", false
}

func mustExtractValue(xmlData, tag string) (string, error) {
	value, ok := extractValueByTag(xmlData, tag)
	if !ok {
		return "", fmt.Errorf("не удалось извлечь значение Mitsu по тегу %q", tag)
	}
	return value, nil
}

func formatPythonBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}
