package libfptr

import (
	"fmt"
	"strconv"
	"time"

	"etalon-agent/internal/fiscalatol/domain"
)

const (
	paramDataType          = 65587
	paramModelName         = 65603
	paramSerialNumber      = 65559
	paramFNDataType        = 65622
	paramDateTime          = 65590
	paramRegistrationsCnt  = 65638
	paramDocumentNumber    = 65598
	paramUnitType          = 65609
	paramUnitVersion       = 65604
	paramFFDVersion        = 65629
	paramLicenseNumber     = 65610
	paramLicenseName       = 65821
	paramLicenseValidFrom  = 65824
	paramLicenseValidUntil = 65825
	paramRecordsType       = 65668
	paramRecordsID         = 65764

	dataTypeStatus      = 0
	dataTypeUnitVersion = 2

	fndtFNInfo           = 2
	fndtLastRegistration = 3
	fndtFFDVersions      = 7
	fndtValidity         = 8
	fndtRegInfo          = 9
	fndtDocumentByNumber = 13

	unitTypeConfiguration = 1
	recordsTypeLicenses   = 4

	fiscalTagRNM              = 1037
	fiscalTagOFDName          = 1046
	fiscalTagOrganizationName = 1048
	fiscalTagINN              = 1018
	fiscalTagAddress          = 1009
	fiscalTagAttributeExcise  = 1207

	driverOK         = 0
	driverNoMoreData = 30
)

type queryAPI interface {
	SetParamInt(id, value int) error
	SetParamString(id int, value string) error
	QueryData() error
	FNQueryData() error
	GetParamString(id int) (string, error)
	GetParamInt(id int) (int, error)
	GetParamBool(id int) (bool, error)
	GetParamDateTime(id int) (time.Time, error)
	BeginReadRecords() error
	ReadNextRecord(recordsID string) (int, error)
	EndReadRecords(recordsID string) error
}

func collectPayload(api queryAPI, currentProfile profile, driverVersion string) (domain.FiscalPayload, []string, error) {
	payload := domain.FiscalPayload{
		InstalledDriver: driverVersion,
		Licenses:        map[string]domain.License{},
	}
	var warnings []string

	if err := api.SetParamInt(paramDataType, dataTypeStatus); err != nil {
		return payload, warnings, err
	}
	if err := api.QueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить общую информацию о ККТ: %w", err)
	}

	modelName, err := api.GetParamString(paramModelName)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать modelName: %w", err)
	}
	serialNumber, err := api.GetParamString(paramSerialNumber)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать serialNumber: %w", err)
	}
	payload.ModelName = modelName
	payload.SerialNumber = serialNumber

	if err := api.SetParamInt(paramFNDataType, fndtRegInfo); err != nil {
		return payload, warnings, err
	}
	if err := api.FNQueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить регистрационные данные ФН: %w", err)
	}

	payload.RNM, err = api.GetParamString(fiscalTagRNM)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать RNM: %w", err)
	}
	payload.OFDName, err = api.GetParamString(fiscalTagOFDName)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать ofdName: %w", err)
	}
	payload.OrganizationName, err = api.GetParamString(fiscalTagOrganizationName)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать organizationName: %w", err)
	}
	payload.INN, err = api.GetParamString(fiscalTagINN)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать INN: %w", err)
	}
	attributeExcise, err := api.GetParamBool(fiscalTagAttributeExcise)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать attribute_excise: %w", err)
	}
	payload.AttributeExcise = formatPythonBool(attributeExcise)
	payload.Address, err = api.GetParamString(fiscalTagAddress)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать address: %w", err)
	}

	if currentProfile.ParamTradeMarkedProducts != 0 {
		attributeMarked, markedErr := api.GetParamBool(currentProfile.ParamTradeMarkedProducts)
		if markedErr != nil {
			warnings = append(warnings, fmt.Sprintf("не удалось прочитать attribute_marked: %v", markedErr))
		} else {
			payload.AttributeMarked = formatPythonBool(attributeMarked)
		}
	} else {
		warnings = append(warnings, "поле attribute_marked недоступно в ветке драйвера 10.8")
	}

	if err := api.SetParamInt(paramFNDataType, fndtFNInfo); err != nil {
		return payload, warnings, err
	}
	if err := api.FNQueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить данные ФН: %w", err)
	}

	payload.FNSerial, err = api.GetParamString(paramSerialNumber)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать fn_serial: %w", err)
	}
	if currentProfile.ParamFNExecution != 0 {
		payload.FNExecution, err = api.GetParamString(currentProfile.ParamFNExecution)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("не удалось прочитать fnExecution: %v", err))
		}
	} else {
		warnings = append(warnings, "поле fnExecution недоступно в ветке драйвера 10.8")
	}

	dateTimeReg, regWarnings, err := readRegistrationDate(api)
	warnings = append(warnings, regWarnings...)
	if err != nil {
		return payload, warnings, err
	}
	payload.DateTimeReg = formatDriverTime(dateTimeReg)

	if err := api.SetParamInt(paramFNDataType, fndtValidity); err != nil {
		return payload, warnings, err
	}
	if err := api.FNQueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить дату окончания ФН: %w", err)
	}
	dateTimeEnd, err := api.GetParamDateTime(paramDateTime)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать dateTime_end: %w", err)
	}
	payload.DateTimeEnd = formatDriverTime(dateTimeEnd)

	if err := api.SetParamInt(paramDataType, dataTypeUnitVersion); err != nil {
		return payload, warnings, err
	}
	if err := api.SetParamInt(paramUnitType, unitTypeConfiguration); err != nil {
		return payload, warnings, err
	}
	if err := api.QueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить bootVersion: %w", err)
	}
	payload.BootVersion, err = api.GetParamString(paramUnitVersion)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать bootVersion: %w", err)
	}

	if err := api.SetParamInt(paramFNDataType, fndtFFDVersions); err != nil {
		return payload, warnings, err
	}
	if err := api.FNQueryData(); err != nil {
		return payload, warnings, fmt.Errorf("не удалось запросить ffdVersion: %w", err)
	}
	ffdVersion, err := api.GetParamInt(paramFFDVersion)
	if err != nil {
		return payload, warnings, fmt.Errorf("не удалось прочитать ffdVersion: %w", err)
	}
	payload.FFDVersion = strconv.Itoa(ffdVersion)

	licenses, licenseWarnings, err := readLicenses(api)
	warnings = append(warnings, licenseWarnings...)
	if err != nil {
		return payload, warnings, err
	}
	payload.Licenses = licenses

	return payload, warnings, nil
}

func readRegistrationDate(api queryAPI) (time.Time, []string, error) {
	var warnings []string

	if err := api.SetParamInt(paramFNDataType, fndtLastRegistration); err != nil {
		return time.Time{}, warnings, err
	}
	if err := api.FNQueryData(); err != nil {
		return time.Time{}, warnings, fmt.Errorf("не удалось запросить дату регистрации ФН: %w", err)
	}

	registrationsCount, err := api.GetParamInt(paramRegistrationsCnt)
	if err != nil {
		return time.Time{}, warnings, fmt.Errorf("не удалось прочитать количество регистраций: %w", err)
	}
	if registrationsCount != 1 {
		if err := api.SetParamInt(paramFNDataType, fndtDocumentByNumber); err != nil {
			return time.Time{}, warnings, err
		}
		if err := api.SetParamInt(paramDocumentNumber, 1); err != nil {
			return time.Time{}, warnings, err
		}
		if err := api.FNQueryData(); err != nil {
			return time.Time{}, warnings, fmt.Errorf("не удалось запросить документ регистрации №1: %w", err)
		}
	}

	dateTimeReg, err := api.GetParamDateTime(paramDateTime)
	if err != nil {
		return time.Time{}, warnings, fmt.Errorf("не удалось прочитать datetime_reg: %w", err)
	}
	return dateTimeReg, warnings, nil
}

func readLicenses(api queryAPI) (map[string]domain.License, []string, error) {
	licenses := map[string]domain.License{}
	var warnings []string

	if err := api.SetParamInt(paramRecordsType, recordsTypeLicenses); err != nil {
		return licenses, warnings, err
	}
	if err := api.BeginReadRecords(); err != nil {
		return licenses, warnings, fmt.Errorf("не удалось начать чтение лицензий: %w", err)
	}

	recordsID, err := api.GetParamString(paramRecordsID)
	if err != nil {
		return licenses, warnings, fmt.Errorf("не удалось прочитать идентификатор списка лицензий: %w", err)
	}

	for {
		result, readErr := api.ReadNextRecord(recordsID)
		if readErr != nil {
			return licenses, warnings, fmt.Errorf("не удалось прочитать лицензию: %w", readErr)
		}
		if result == driverNoMoreData {
			break
		}
		if result != driverOK {
			return licenses, warnings, fmt.Errorf("драйвер вернул неожиданный код при чтении лицензий: %d", result)
		}

		licenseNumber, numberErr := api.GetParamInt(paramLicenseNumber)
		if numberErr != nil {
			return licenses, warnings, fmt.Errorf("не удалось прочитать номер лицензии: %w", numberErr)
		}
		licenseName, nameErr := api.GetParamString(paramLicenseName)
		if nameErr != nil {
			return licenses, warnings, fmt.Errorf("не удалось прочитать название лицензии: %w", nameErr)
		}
		dateFrom, fromErr := api.GetParamDateTime(paramLicenseValidFrom)
		if fromErr != nil {
			return licenses, warnings, fmt.Errorf("не удалось прочитать дату начала лицензии: %w", fromErr)
		}
		dateUntil, untilErr := api.GetParamDateTime(paramLicenseValidUntil)
		if untilErr != nil {
			return licenses, warnings, fmt.Errorf("не удалось прочитать дату окончания лицензии: %w", untilErr)
		}

		licenses[strconv.Itoa(licenseNumber)] = domain.License{
			Name:      licenseName,
			DateFrom:  formatDriverTime(dateFrom),
			DateUntil: formatDriverTime(dateUntil),
		}
	}

	if err := api.EndReadRecords(recordsID); err != nil {
		warnings = append(warnings, fmt.Sprintf("не удалось корректно завершить чтение лицензий: %v", err))
	}
	return licenses, warnings, nil
}

func formatDriverTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05")
}

func formatPythonBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}
