package libfptr

import (
	"fmt"
	"strconv"
	"strings"
)

type ParsedVersion struct {
	Major int
	Minor int
	Patch int
	Build int
}

func ParseVersion(raw string) (ParsedVersion, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) < 2 {
		return ParsedVersion{}, fmt.Errorf("некорректный формат версии драйвера %q", raw)
	}

	values := [4]int{}
	for idx := range values {
		if idx >= len(parts) {
			continue
		}
		number, err := strconv.Atoi(strings.TrimSpace(parts[idx]))
		if err != nil {
			return ParsedVersion{}, fmt.Errorf("некорректный формат версии драйвера %q", raw)
		}
		values[idx] = number
	}

	return ParsedVersion{
		Major: values[0],
		Minor: values[1],
		Patch: values[2],
		Build: values[3],
	}, nil
}

func VariantFromVersion(raw string) (Variant, error) {
	version, err := ParseVersion(raw)
	if err != nil {
		return "", err
	}

	if version.Major > 10 || (version.Major == 10 && version.Minor >= 9) {
		return Variant109, nil
	}
	return Variant108, nil
}

type profile struct {
	Variant                  Variant
	ParamTradeMarkedProducts int
	ParamFNExecution         int
}

func selectProfile(driverVersion string) (profile, error) {
	variant, err := VariantFromVersion(driverVersion)
	if err != nil {
		return profile{}, err
	}

	result := profile{
		Variant:                  variant,
		ParamTradeMarkedProducts: 0,
		ParamFNExecution:         0,
	}
	if variant == Variant109 {
		result.ParamTradeMarkedProducts = 65855
		result.ParamFNExecution = 65874
	}
	return result, nil
}
