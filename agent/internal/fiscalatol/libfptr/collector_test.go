package libfptr

import (
	"fmt"
	"testing"
	"time"
)

type fakeQueryAPI struct {
	registrationsCount int
	currentDataType    int
	currentFNDataType  int
	currentDocument    int
	currentRecordsType int
	currentRecordIndex int
}

type fakeLicense struct {
	number int
	name   string
	from   time.Time
	until  time.Time
}

var (
	lastRegistrationTime = time.Date(2024, time.January, 10, 8, 30, 0, 0, time.Local)
	firstDocumentTime    = time.Date(2023, time.June, 5, 9, 45, 0, 0, time.Local)
	validityTime         = time.Date(2026, time.December, 31, 23, 59, 59, 0, time.Local)
	licenseItems         = []fakeLicense{
		{
			number: 1,
			name:   "Маркировка",
			from:   time.Date(2024, time.January, 1, 0, 0, 0, 0, time.Local),
			until:  time.Date(2025, time.January, 1, 0, 0, 0, 0, time.Local),
		},
		{
			number: 2,
			name:   "Удалённый доступ",
			from:   time.Date(2024, time.February, 1, 0, 0, 0, 0, time.Local),
			until:  time.Date(2025, time.February, 1, 0, 0, 0, 0, time.Local),
		},
	}
)

func (f *fakeQueryAPI) SetParamInt(id, value int) error {
	switch id {
	case paramDataType:
		f.currentDataType = value
	case paramFNDataType:
		f.currentFNDataType = value
	case paramDocumentNumber:
		f.currentDocument = value
	case paramRecordsType:
		f.currentRecordsType = value
	}
	return nil
}

func (f *fakeQueryAPI) SetParamString(id int, value string) error {
	if id == paramRecordsID && value != "licenses" {
		return fmt.Errorf("неожиданный recordsID %q", value)
	}
	return nil
}

func (f *fakeQueryAPI) QueryData() error {
	return nil
}

func (f *fakeQueryAPI) FNQueryData() error {
	return nil
}

func (f *fakeQueryAPI) GetParamString(id int) (string, error) {
	switch {
	case f.currentFNDataType == fndtRegInfo:
		switch id {
		case fiscalTagRNM:
			return "0000000001234567", nil
		case fiscalTagOFDName:
			return "Первый ОФД", nil
		case fiscalTagOrganizationName:
			return "ООО Тест", nil
		case fiscalTagINN:
			return "7701234567", nil
		case fiscalTagAddress:
			return "г. Москва, ул. Тестовая, д. 1", nil
		}
	case f.currentFNDataType == fndtFNInfo:
		switch id {
		case paramSerialNumber:
			return "9999078900000001", nil
		case 65874:
			return "Исполнение ФН-1.2", nil
		}
	case f.currentDataType == dataTypeUnitVersion:
		if id == paramUnitVersion {
			return "5.15.0", nil
		}
	case f.currentDataType == dataTypeStatus:
		switch id {
		case paramModelName:
			return "АТОЛ 30Ф", nil
		case paramSerialNumber:
			return "SN123456", nil
		}
	}

	if id == paramRecordsID {
		return "licenses", nil
	}
	if f.currentRecordsType == recordsTypeLicenses && f.currentRecordIndex >= 0 && f.currentRecordIndex < len(licenseItems) {
		if id == paramLicenseName {
			return licenseItems[f.currentRecordIndex].name, nil
		}
	}
	return "", fmt.Errorf("неожиданный запрос строкового параметра id=%d", id)
}

func (f *fakeQueryAPI) GetParamInt(id int) (int, error) {
	switch {
	case f.currentFNDataType == fndtLastRegistration && id == paramRegistrationsCnt:
		return f.registrationsCount, nil
	case f.currentFNDataType == fndtFFDVersions && id == paramFFDVersion:
		return 120, nil
	case f.currentRecordsType == recordsTypeLicenses && f.currentRecordIndex >= 0 && f.currentRecordIndex < len(licenseItems) && id == paramLicenseNumber:
		return licenseItems[f.currentRecordIndex].number, nil
	}
	return 0, fmt.Errorf("неожиданный запрос числового параметра id=%d", id)
}

func (f *fakeQueryAPI) GetParamBool(id int) (bool, error) {
	switch id {
	case fiscalTagAttributeExcise:
		return true, nil
	case 65855:
		return false, nil
	}
	return false, fmt.Errorf("неожиданный запрос bool параметра id=%d", id)
}

func (f *fakeQueryAPI) GetParamDateTime(id int) (time.Time, error) {
	switch {
	case id == paramDateTime && f.currentFNDataType == fndtLastRegistration:
		return lastRegistrationTime, nil
	case id == paramDateTime && f.currentFNDataType == fndtDocumentByNumber && f.currentDocument == 1:
		return firstDocumentTime, nil
	case id == paramDateTime && f.currentFNDataType == fndtValidity:
		return validityTime, nil
	case f.currentRecordsType == recordsTypeLicenses && f.currentRecordIndex >= 0 && f.currentRecordIndex < len(licenseItems):
		switch id {
		case paramLicenseValidFrom:
			return licenseItems[f.currentRecordIndex].from, nil
		case paramLicenseValidUntil:
			return licenseItems[f.currentRecordIndex].until, nil
		}
	}
	return time.Time{}, fmt.Errorf("неожиданный запрос datetime параметра id=%d", id)
}

func (f *fakeQueryAPI) BeginReadRecords() error {
	f.currentRecordIndex = -1
	return nil
}

func (f *fakeQueryAPI) ReadNextRecord(recordsID string) (int, error) {
	if recordsID != "licenses" {
		return 0, fmt.Errorf("неожиданный recordsID %q", recordsID)
	}
	if f.currentRecordIndex+1 >= len(licenseItems) {
		return driverNoMoreData, nil
	}
	f.currentRecordIndex++
	return driverOK, nil
}

func (f *fakeQueryAPI) EndReadRecords(recordsID string) error {
	if recordsID != "licenses" {
		return fmt.Errorf("неожиданный recordsID %q", recordsID)
	}
	return nil
}

func TestCollectPayloadVariant109(t *testing.T) {
	t.Parallel()

	currentProfile, err := selectProfile("10.10.8.0")
	if err != nil {
		t.Fatalf("не удалось выбрать профиль: %v", err)
	}

	payload, warnings, err := collectPayload(&fakeQueryAPI{registrationsCount: 1}, currentProfile, "10.10.8.0")
	if err != nil {
		t.Fatalf("collectPayload вернул ошибку: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("не ожидались предупреждения, получено: %v", warnings)
	}
	if payload.ModelName != "АТОЛ 30Ф" {
		t.Fatalf("ожидался modelName АТОЛ 30Ф, получено %q", payload.ModelName)
	}
	if payload.FNExecution != "Исполнение ФН-1.2" {
		t.Fatalf("ожидался fnExecution, получено %q", payload.FNExecution)
	}
	if payload.AttributeMarked != "False" {
		t.Fatalf("ожидался attribute_marked=False, получено %q", payload.AttributeMarked)
	}
	if payload.DateTimeReg != "2024-01-10 08:30:00" {
		t.Fatalf("ожидалась дата регистрации по последней регистрации, получено %q", payload.DateTimeReg)
	}
	if len(payload.Licenses) != 2 {
		t.Fatalf("ожидались 2 лицензии, получено %d", len(payload.Licenses))
	}
}

func TestCollectPayloadVariant108UsesFirstDocumentFallback(t *testing.T) {
	t.Parallel()

	currentProfile, err := selectProfile("10.8.5.0")
	if err != nil {
		t.Fatalf("не удалось выбрать профиль: %v", err)
	}

	payload, warnings, err := collectPayload(&fakeQueryAPI{registrationsCount: 2}, currentProfile, "10.8.5.0")
	if err != nil {
		t.Fatalf("collectPayload вернул ошибку: %v", err)
	}
	if payload.DateTimeReg != "2023-06-05 09:45:00" {
		t.Fatalf("ожидалась дата из документа №1, получено %q", payload.DateTimeReg)
	}
	if payload.AttributeMarked != "" {
		t.Fatalf("для ветки 10.8 ожидалось пустое attribute_marked, получено %q", payload.AttributeMarked)
	}
	if payload.FNExecution != "" {
		t.Fatalf("для ветки 10.8 ожидался пустой fnExecution, получено %q", payload.FNExecution)
	}
	if len(warnings) != 2 {
		t.Fatalf("ожидались 2 предупреждения по недоступным полям, получено %d", len(warnings))
	}
}
