package drvfr

import (
	"context"
	"errors"
	"testing"

	"etalon-agent/internal/fiscalshtrih/domain"
)

type fakeRuntime struct {
	probeResult   ProbeResult
	probeErr      error
	driver        Driver
	createdConfig []Config
}

type fakeDriver struct {
	connectErr    error
	disconnectErr error
	infoErr       error
	info          *FiscalInfo
	connected     bool
}

func (r *fakeRuntime) Probe(context.Context) (ProbeResult, error) {
	return r.probeResult, r.probeErr
}

func (r *fakeRuntime) NewDriver(config Config) Driver {
	r.createdConfig = append(r.createdConfig, config)
	return r.driver
}

func (d *fakeDriver) Connect() error {
	if d.connectErr != nil {
		return d.connectErr
	}
	d.connected = true
	return nil
}

func (d *fakeDriver) Disconnect() error {
	d.connected = false
	return d.disconnectErr
}

func (d *fakeDriver) GetFiscalInfo() (*FiscalInfo, error) {
	if !d.connected {
		return nil, errors.New("драйвер не подключен")
	}
	if d.infoErr != nil {
		return nil, d.infoErr
	}
	return d.info, nil
}

func TestBridgeCollectMapsCOMEndpointAndPayload(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{
		driver: &fakeDriver{
			info: &FiscalInfo{
				ModelName:        " ШТРИХ-М-01Ф ",
				SerialNumber:     " 0012345678901234 ",
				RNM:              " 0000000001000001 ",
				OrganizationName: " ООО Ромашка ",
				Address:          " г. Москва ",
				INN:              " 7701234567 ",
				FNSerial:         " 9999078900000001 ",
				RegistrationDate: "2024-02-03 10:11:12",
				FNEndDate:        "2025-02-03 10:11:12",
				OFDName:          " Тестовый ОФД ",
				SoftwareDate:     "2024-01-31",
				FFDVersion:       "120",
				FNExecution:      " ФН-1.2 ",
				InstalledDriver:  "4.17.0.0",
				AttributeExcise:  true,
				AttributeMarked:  false,
				SubscriptionInfo: " Подписка до 4 квартала 2027 года ",
			},
		},
	}

	endpoint := domain.Endpoint{
		Transport: domain.TransportCOM,
		COMPort:   "COM4",
		BaudRate:  "115200",
	}

	payload, meta, warnings, err := newBridgeWithRuntime(runtime).Collect(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Collect вернул ошибку: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("не ожидались предупреждения, получено %v", warnings)
	}
	if len(runtime.createdConfig) != 1 {
		t.Fatalf("ожидался один вызов NewDriver, получено %d", len(runtime.createdConfig))
	}
	if runtime.createdConfig[0].ConnectionType != connectionTypeCOM {
		t.Fatalf("ожидался COM connectionType, получено %d", runtime.createdConfig[0].ConnectionType)
	}
	if runtime.createdConfig[0].ComNumber != 4 {
		t.Fatalf("ожидался номер COM-порта 4, получено %d", runtime.createdConfig[0].ComNumber)
	}
	if runtime.createdConfig[0].BaudRate != 6 {
		t.Fatalf("ожидался индекс скорости 6 для 115200, получено %d", runtime.createdConfig[0].BaudRate)
	}
	if payload.ModelName != "ШТРИХ-М-01Ф" {
		t.Fatalf("ожидался modelName ШТРИХ-М-01Ф, получено %q", payload.ModelName)
	}
	if payload.AttributeExcise != "True" {
		t.Fatalf("ожидался attribute_excise=True, получено %q", payload.AttributeExcise)
	}
	if payload.AttributeMarked != "False" {
		t.Fatalf("ожидался attribute_marked=False, получено %q", payload.AttributeMarked)
	}
	if payload.Licenses != "Подписка до 4 квартала 2027 года" {
		t.Fatalf("ожидалась строка лицензии, получено %q", payload.Licenses)
	}
	if meta.DriverVersion != "4.17.0.0" {
		t.Fatalf("ожидалась версия драйвера 4.17.0.0, получено %q", meta.DriverVersion)
	}
}

func TestBridgeCollectAddsDisconnectWarning(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{
		driver: &fakeDriver{
			disconnectErr: errors.New("разрыв COM завершился ошибкой"),
			info: &FiscalInfo{
				ModelName:       "ШТРИХ",
				InstalledDriver: "4.17.0.0",
			},
		},
	}

	endpoint := domain.Endpoint{
		Transport: domain.TransportTCP,
		IP:        "10.25.1.22",
		Port:      5555,
	}

	_, _, warnings, err := newBridgeWithRuntime(runtime).Collect(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Collect вернул ошибку: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("ожидалось одно предупреждение, получено %d", len(warnings))
	}
}

func TestBridgeCollectRejectsUnsupportedBaudRate(t *testing.T) {
	t.Parallel()

	_, _, _, err := newBridgeWithRuntime(&fakeRuntime{}).Collect(context.Background(), domain.Endpoint{
		Transport: domain.TransportCOM,
		COMPort:   "COM4",
		BaudRate:  "12345",
	})
	if err == nil {
		t.Fatal("ожидалась ошибка для неподдерживаемого baudrate")
	}
}

func TestBridgeProbeDelegatesToRuntime(t *testing.T) {
	t.Parallel()

	expected := ProbeResult{
		Supported:     true,
		DriverPresent: true,
		DriverVersion: "4.17.0.0",
		DriverProgID:  driverProgID,
		RequiredOS:    "windows",
		RequiredArch:  "386",
	}

	probe, err := newBridgeWithRuntime(&fakeRuntime{probeResult: expected}).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe вернул ошибку: %v", err)
	}
	if probe != expected {
		t.Fatalf("ожидался результат %+v, получено %+v", expected, probe)
	}
}
