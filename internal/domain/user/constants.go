package user

import (
	"regexp"
	"strings"
)

const (
	RoleAdmin             = "admin"
	RoleSupportSpecialist = "support_specialist"
	RoleIntern            = "intern"
)

const (
	ScheduleTwoTwo     = "2/2"
	ScheduleThreeThree = "3/3"
	ScheduleFiveTwo    = "5/2"
)

const (
	ExternalTypeTelegram = "telegram"
	ExternalTypeNaumen   = "naumen"
	ExternalTypeBitrix24 = "bitrix24"
	ExternalTypePyrus    = "pyrus"
)

func IsValidRole(role string) bool {
	switch role {
	case RoleAdmin, RoleSupportSpecialist, RoleIntern:
		return true
	default:
		return false
	}
}

func IsValidSchedule(schedule string) bool {
	switch schedule {
	case ScheduleTwoTwo, ScheduleThreeThree, ScheduleFiveTwo:
		return true
	default:
		return false
	}
}

func IsValidExternalType(externalType string) bool {
	switch strings.ToLower(strings.TrimSpace(externalType)) {
	case ExternalTypeTelegram, ExternalTypeNaumen, ExternalTypeBitrix24, ExternalTypePyrus:
		return true
	default:
		return false
	}
}

func IsValidExternalID(externalType, externalID string) bool {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(externalType)) {
	case ExternalTypeTelegram:
		return strings.HasPrefix(externalID, "@") && regexp.MustCompile(`^@[A-Za-z0-9_]+$`).MatchString(externalID)
	case ExternalTypeNaumen:
		return strings.HasPrefix(externalID, "$")
	case ExternalTypeBitrix24:
		return regexp.MustCompile(`^[0-9]+$`).MatchString(externalID)
	case ExternalTypePyrus:
		return regexp.MustCompile(`^[0-9]+$`).MatchString(externalID)
	default:
		return false
	}
}
