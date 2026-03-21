//go:build windows && 386

package drvfr

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

type liveRuntime struct{}

type comDriver struct {
	config        Config
	dispatch      *ole.IDispatch
	connected     bool
	coInitialized bool
	threadLocked  bool
}

func newRuntime() runtimeAPI {
	return liveRuntime{}
}

func (liveRuntime) Probe(context.Context) (ProbeResult, error) {
	result := ProbeResult{
		Supported:    true,
		DriverProgID: driverProgID,
		RequiredOS:   "windows",
		RequiredArch: "386",
	}

	version, present, err := probeInstalledDriverVersion()
	if err != nil {
		return result, err
	}
	if !present {
		result.Message = "COM-драйвер Штрих не зарегистрирован"
		return result, nil
	}

	result.DriverPresent = true
	result.DriverVersion = version
	if version == "" {
		result.Message = "COM-драйвер Штрих найден, но его версия не определена"
		return result, nil
	}

	result.Message = "COM-драйвер Штрих найден"
	return result, nil
}

func (liveRuntime) NewDriver(config Config) Driver {
	return &comDriver{config: config}
}

func probeInstalledDriverVersion() (string, bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := initializeCOM(); err != nil {
		return "", false, fmt.Errorf("не удалось инициализировать COM для проверки драйвера Штрих: %w", err)
	}
	defer ole.CoUninitialize()

	unknown, err := oleutil.CreateObject(driverProgID)
	if err != nil {
		return "", false, nil
	}
	defer unknown.Release()

	dispatch, err := unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return "", false, fmt.Errorf("не удалось получить IDispatch драйвера Штрих: %w", err)
	}
	defer dispatch.Release()

	version, err := readDriverVersion(dispatch)
	if err != nil {
		return "", true, err
	}
	return version, true, nil
}

func (d *comDriver) Connect() error {
	if d.connected {
		return nil
	}

	runtime.LockOSThread()
	d.threadLocked = true

	if err := initializeCOM(); err != nil {
		d.releaseResources()
		return fmt.Errorf("не удалось инициализировать COM: %w", err)
	}
	d.coInitialized = true

	unknown, err := oleutil.CreateObject(driverProgID)
	if err != nil {
		d.releaseResources()
		return fmt.Errorf("не удалось создать COM-объект драйвера Штрих: %w", err)
	}
	defer unknown.Release()

	d.dispatch, err = unknown.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		d.releaseResources()
		return fmt.Errorf("не удалось получить интерфейс IDispatch драйвера Штрих: %w", err)
	}

	if err := d.applyConnectionSettings(); err != nil {
		d.releaseResources()
		return err
	}

	if _, err := oleutil.CallMethod(d.dispatch, "Connect"); err != nil {
		d.releaseResources()
		return fmt.Errorf("не удалось вызвать Connect драйвера Штрих: %w", err)
	}
	if err := d.checkError(); err != nil {
		d.releaseResources()
		return fmt.Errorf("драйвер Штрих вернул ошибку подключения: %w", err)
	}

	d.connected = true
	return nil
}

func (d *comDriver) Disconnect() error {
	var disconnectErrs []error

	if d.dispatch != nil {
		if d.connected {
			if _, err := oleutil.CallMethod(d.dispatch, "Disconnect"); err != nil {
				disconnectErrs = append(disconnectErrs, fmt.Errorf("не удалось вызвать Disconnect драйвера Штрих: %w", err))
			} else if err := d.checkError(); err != nil {
				disconnectErrs = append(disconnectErrs, fmt.Errorf("драйвер Штрих вернул ошибку при разрыве соединения: %w", err))
			}
		}
		d.dispatch.Release()
		d.dispatch = nil
	}

	if d.coInitialized {
		ole.CoUninitialize()
		d.coInitialized = false
	}
	if d.threadLocked {
		runtime.UnlockOSThread()
		d.threadLocked = false
	}

	d.connected = false
	return errors.Join(disconnectErrs...)
}

func (d *comDriver) GetFiscalInfo() (*FiscalInfo, error) {
	if !d.connected {
		return nil, fmt.Errorf("драйвер не подключен")
	}

	info := &FiscalInfo{}
	if err := d.getBaseDeviceInfo(info); err != nil {
		return nil, fmt.Errorf("ошибка получения базовой информации об устройстве: %w", err)
	}
	if err := d.getFiscalizationInfo(info); err != nil {
		return nil, fmt.Errorf("ошибка получения информации о фискализации: %w", err)
	}
	if err := d.getFnInfo(info); err != nil {
		return nil, fmt.Errorf("ошибка получения информации о ФН: %w", err)
	}
	if err := d.getInfoFromTables(info); err != nil {
		return nil, fmt.Errorf("ошибка получения информации из таблиц: %w", err)
	}
	return info, nil
}

func (d *comDriver) applyConnectionSettings() error {
	if _, err := oleutil.PutProperty(d.dispatch, "ConnectionType", d.config.ConnectionType); err != nil {
		return fmt.Errorf("не удалось установить ConnectionType: %w", err)
	}
	if _, err := oleutil.PutProperty(d.dispatch, "Password", d.config.Password); err != nil {
		return fmt.Errorf("не удалось установить Password: %w", err)
	}

	switch d.config.ConnectionType {
	case connectionTypeCOM:
		if _, err := oleutil.PutProperty(d.dispatch, "ComNumber", d.config.ComNumber); err != nil {
			return fmt.Errorf("не удалось установить ComNumber: %w", err)
		}
		if _, err := oleutil.PutProperty(d.dispatch, "BaudRate", d.config.BaudRate); err != nil {
			return fmt.Errorf("не удалось установить BaudRate: %w", err)
		}
	case connectionTypeTCP:
		if _, err := oleutil.PutProperty(d.dispatch, "IPAddress", d.config.IPAddress); err != nil {
			return fmt.Errorf("не удалось установить IPAddress: %w", err)
		}
		if _, err := oleutil.PutProperty(d.dispatch, "TCPPort", d.config.TCPPort); err != nil {
			return fmt.Errorf("не удалось установить TCPPort: %w", err)
		}
		if _, err := oleutil.PutProperty(d.dispatch, "UseIPAddress", true); err != nil {
			return fmt.Errorf("не удалось установить UseIPAddress: %w", err)
		}
	default:
		return fmt.Errorf("неподдерживаемый тип подключения %d", d.config.ConnectionType)
	}

	return nil
}

func (d *comDriver) getBaseDeviceInfo(info *FiscalInfo) error {
	version, err := readDriverVersion(d.dispatch)
	if err != nil {
		return err
	}
	info.InstalledDriver = version

	if _, err := oleutil.CallMethod(d.dispatch, "GetDeviceMetrics"); err != nil {
		return fmt.Errorf("не удалось вызвать GetDeviceMetrics: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}
	info.ModelName, _ = d.getPropertyString("UDescription")

	if _, err := oleutil.CallMethod(d.dispatch, "GetECRStatus"); err != nil {
		return fmt.Errorf("не удалось вызвать GetECRStatus: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}

	ecrSoftDate, err := d.getPropertyTime("ECRSoftDate")
	if err == nil && !ecrSoftDate.IsZero() {
		info.SoftwareDate = ecrSoftDate.Format("2006-01-02")
	}

	if _, err := oleutil.CallMethod(d.dispatch, "ReadFeatureLicenses"); err == nil {
		if err := d.checkError(); err == nil {
			licenseHex, _ := d.getPropertyString("License")
			info.SubscriptionInfo = decodeLicense(licenseHex)
		}
	}

	return nil
}

func (d *comDriver) getFiscalizationInfo(info *FiscalInfo) error {
	if _, err := oleutil.PutProperty(d.dispatch, "RegistrationNumber", 1); err != nil {
		return fmt.Errorf("не удалось установить RegistrationNumber: %w", err)
	}
	if _, err := oleutil.CallMethod(d.dispatch, "FNGetFiscalizationResult"); err != nil {
		return fmt.Errorf("не удалось вызвать FNGetFiscalizationResult: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}

	info.RNM, _ = d.getPropertyString("KKTRegistrationNumber")
	info.INN, _ = d.getPropertyString("INN")
	info.INN = strings.TrimSpace(info.INN)

	regDate, err := d.getPropertyTime("Date")
	if err == nil && !regDate.IsZero() {
		regTime, parseErr := time.Parse("15:04:05", strings.TrimSpace(d.mustGetPropertyString("Time")))
		if parseErr == nil {
			info.RegistrationDate = time.Date(
				regDate.Year(),
				regDate.Month(),
				regDate.Day(),
				regTime.Hour(),
				regTime.Minute(),
				regTime.Second(),
				0,
				time.Local,
			).Format("2006-01-02 15:04:05")
		}
	}

	workModeEx, _ := d.getPropertyInt32("WorkModeEx")
	info.AttributeMarked = (workModeEx & 0x10) != 0
	info.AttributeExcise = (workModeEx & 0x01) != 0
	return nil
}

func (d *comDriver) getFnInfo(info *FiscalInfo) error {
	if _, err := oleutil.CallMethod(d.dispatch, "FNGetSerial"); err != nil {
		return fmt.Errorf("не удалось вызвать FNGetSerial: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}
	info.FNSerial, _ = d.getPropertyString("SerialNumber")

	if _, err := oleutil.CallMethod(d.dispatch, "FNGetExpirationTime"); err != nil {
		return fmt.Errorf("не удалось вызвать FNGetExpirationTime: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}

	fnEndDate, err := d.getPropertyTime("Date")
	if err == nil && !fnEndDate.IsZero() {
		info.FNEndDate = fnEndDate.Format("2006-01-02 15:04:05")
	}

	if _, err := oleutil.CallMethod(d.dispatch, "FNGetImplementation"); err != nil {
		return fmt.Errorf("не удалось вызвать FNGetImplementation: %w", err)
	}
	if err := d.checkError(); err != nil {
		return err
	}

	info.FNExecution, _ = d.getPropertyString("FNImplementation")
	info.FNExecution = strings.TrimSpace(info.FNExecution)
	return nil
}

func (d *comDriver) getInfoFromTables(info *FiscalInfo) error {
	serialNumber, err := d.readTableField(18, 1, 1)
	if err == nil {
		info.SerialNumber = strings.TrimSpace(serialNumber)
	}

	organizationName, err := d.readTableField(18, 1, 7)
	if err == nil {
		info.OrganizationName = strings.TrimSpace(organizationName)
	}

	ofdName, err := d.readTableField(18, 1, 10)
	if err == nil {
		info.OFDName = strings.TrimSpace(ofdName)
	}

	address, err := d.readTableField(18, 1, 9)
	if err == nil {
		info.Address = strings.TrimSpace(address)
	}

	ffdValue, err := d.readTableField(17, 1, 17)
	if err != nil {
		info.FFDVersion = "не определена"
		return nil
	}

	code, convErr := strconv.Atoi(strings.TrimSpace(ffdValue))
	if convErr != nil {
		info.FFDVersion = fmt.Sprintf("неизвестный код (%s)", strings.TrimSpace(ffdValue))
		return nil
	}

	switch code {
	case 2:
		info.FFDVersion = "105"
	case 4:
		info.FFDVersion = "120"
	default:
		info.FFDVersion = fmt.Sprintf("неизвестный код (%d)", code)
	}
	return nil
}

func (d *comDriver) readTableField(tableNumber, rowNumber, fieldNumber int) (string, error) {
	if _, err := oleutil.PutProperty(d.dispatch, "TableNumber", tableNumber); err != nil {
		return "", fmt.Errorf("не удалось установить TableNumber: %w", err)
	}
	if _, err := oleutil.PutProperty(d.dispatch, "RowNumber", rowNumber); err != nil {
		return "", fmt.Errorf("не удалось установить RowNumber: %w", err)
	}
	if _, err := oleutil.PutProperty(d.dispatch, "FieldNumber", fieldNumber); err != nil {
		return "", fmt.Errorf("не удалось установить FieldNumber: %w", err)
	}
	if _, err := oleutil.CallMethod(d.dispatch, "ReadTable"); err != nil {
		return "", fmt.Errorf("не удалось вызвать ReadTable: %w", err)
	}
	if err := d.checkError(); err != nil {
		return "", err
	}
	return d.getPropertyString("ValueOfFieldString")
}

func (d *comDriver) checkError() error {
	resultCode, err := d.getPropertyInt32("ResultCode")
	if err != nil {
		return fmt.Errorf("не удалось прочитать ResultCode: %w", err)
	}
	if resultCode == 0 {
		return nil
	}

	description, _ := d.getPropertyString("ResultCodeDescription")
	return fmt.Errorf("[%d] %s", resultCode, strings.TrimSpace(description))
}

func (d *comDriver) getPropertyVariant(name string) (*ole.VARIANT, error) {
	return oleutil.GetProperty(d.dispatch, name)
}

func (d *comDriver) getPropertyString(name string) (string, error) {
	variant, err := d.getPropertyVariant(name)
	if err != nil {
		return "", fmt.Errorf("не удалось получить свойство %q: %w", name, err)
	}
	defer variant.Clear()
	return variant.ToString(), nil
}

func (d *comDriver) mustGetPropertyString(name string) string {
	value, _ := d.getPropertyString(name)
	return value
}

func (d *comDriver) getPropertyInt32(name string) (int32, error) {
	variant, err := d.getPropertyVariant(name)
	if err != nil {
		return 0, fmt.Errorf("не удалось получить свойство %q: %w", name, err)
	}
	defer variant.Clear()

	switch value := variant.Value().(type) {
	case nil:
		return 0, nil
	case int:
		return int32(value), nil
	case int8:
		return int32(value), nil
	case int16:
		return int32(value), nil
	case int32:
		return value, nil
	case int64:
		return int32(value), nil
	case uint:
		return int32(value), nil
	case uint8:
		return int32(value), nil
	case uint16:
		return int32(value), nil
	case uint32:
		return int32(value), nil
	case uint64:
		return int32(value), nil
	case string:
		number, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if parseErr != nil {
			return 0, fmt.Errorf("не удалось преобразовать строку %q в int32", value)
		}
		return int32(number), nil
	default:
		return 0, fmt.Errorf("неожиданный тип свойства %q: %T", name, value)
	}
}

func (d *comDriver) getPropertyTime(name string) (time.Time, error) {
	variant, err := d.getPropertyVariant(name)
	if err != nil {
		return time.Time{}, fmt.Errorf("не удалось получить свойство %q: %w", name, err)
	}
	defer variant.Clear()

	timeValue, ok := variant.Value().(time.Time)
	if !ok {
		return time.Time{}, fmt.Errorf("свойство %q не содержит дату", name)
	}
	return timeValue, nil
}

func (d *comDriver) releaseResources() {
	_ = d.Disconnect()
}

func initializeCOM() error {
	if err := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED); err != nil {
		if fallbackErr := ole.CoInitialize(0); fallbackErr != nil {
			return fallbackErr
		}
	}
	return nil
}

func readDriverVersion(dispatch *ole.IDispatch) (string, error) {
	major, err := getPropertyInt32FromDispatch(dispatch, "DriverMajorVersion")
	if err != nil {
		return "", err
	}
	minor, err := getPropertyInt32FromDispatch(dispatch, "DriverMinorVersion")
	if err != nil {
		return "", err
	}
	release, err := getPropertyInt32FromDispatch(dispatch, "DriverRelease")
	if err != nil {
		return "", err
	}
	build, err := getPropertyInt32FromDispatch(dispatch, "DriverBuild")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d.%d.%d.%d", major, minor, release, build), nil
}

func getPropertyInt32FromDispatch(dispatch *ole.IDispatch, name string) (int32, error) {
	variant, err := oleutil.GetProperty(dispatch, name)
	if err != nil {
		return 0, fmt.Errorf("не удалось получить свойство %q: %w", name, err)
	}
	defer variant.Clear()

	switch value := variant.Value().(type) {
	case nil:
		return 0, nil
	case int:
		return int32(value), nil
	case int8:
		return int32(value), nil
	case int16:
		return int32(value), nil
	case int32:
		return value, nil
	case int64:
		return int32(value), nil
	case uint:
		return int32(value), nil
	case uint8:
		return int32(value), nil
	case uint16:
		return int32(value), nil
	case uint32:
		return int32(value), nil
	case uint64:
		return int32(value), nil
	case string:
		number, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
		if parseErr != nil {
			return 0, fmt.Errorf("не удалось преобразовать строку %q в int32", value)
		}
		return int32(number), nil
	default:
		return 0, fmt.Errorf("неожиданный тип свойства %q: %T", name, value)
	}
}
