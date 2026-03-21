package drvfr

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"etalon-agent/internal/fiscalshtrih/domain"
)

const (
	connectionTypeCOM int32 = 0
	connectionTypeTCP int32 = 6
	defaultPassword   int32 = 30

	driverProgID = "AddIn.DrvFR"
)

var baudRateMap = map[string]int32{
	"4800":   1,
	"9600":   2,
	"19200":  3,
	"38400":  4,
	"57600":  5,
	"115200": 6,
}

type ProbeResult struct {
	Supported     bool
	DriverPresent bool
	DriverVersion string
	DriverProgID  string
	RequiredOS    string
	RequiredArch  string
	Message       string
}

type CollectMeta struct {
	ConnectionLabel string
	Transport       domain.Transport
	DriverVersion   string
}

type Bridge interface {
	Probe(context.Context) (ProbeResult, error)
	Collect(context.Context, domain.Endpoint) (domain.FiscalPayload, CollectMeta, []string, error)
}

type Driver interface {
	Connect() error
	Disconnect() error
	GetFiscalInfo() (*FiscalInfo, error)
}

type Config struct {
	ConnectionType int32
	IPAddress      string
	TCPPort        int32
	ComName        string
	ComNumber      int32
	BaudRate       int32
	Password       int32
}

type FiscalInfo struct {
	ModelName        string
	SerialNumber     string
	RNM              string
	OrganizationName string
	Address          string
	INN              string
	FNSerial         string
	RegistrationDate string
	FNEndDate        string
	OFDName          string
	SoftwareDate     string
	FFDVersion       string
	FNExecution      string
	InstalledDriver  string
	AttributeExcise  bool
	AttributeMarked  bool
	SubscriptionInfo string
}

type runtimeAPI interface {
	Probe(context.Context) (ProbeResult, error)
	NewDriver(Config) Driver
}

type runtimeBridge struct {
	runtime runtimeAPI
}

func NewBridge() Bridge {
	return &runtimeBridge{runtime: newRuntime()}
}

func newBridgeWithRuntime(runtime runtimeAPI) Bridge {
	return &runtimeBridge{runtime: runtime}
}

func (b *runtimeBridge) Probe(ctx context.Context) (ProbeResult, error) {
	return b.runtime.Probe(ctx)
}

func (b *runtimeBridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, CollectMeta, []string, error) {
	meta := CollectMeta{
		ConnectionLabel: endpoint.ConnectionLabel(),
		Transport:       endpoint.Transport,
	}

	config, err := configFromEndpoint(endpoint)
	if err != nil {
		return domain.FiscalPayload{}, meta, nil, err
	}

	driver := b.runtime.NewDriver(config)
	if err := driver.Connect(); err != nil {
		return domain.FiscalPayload{}, meta, nil, fmt.Errorf("не удалось подключиться к ККТ Штрих по %s: %w", endpoint.ConnectionLabel(), err)
	}

	info, collectErr := driver.GetFiscalInfo()
	disconnectErr := driver.Disconnect()
	warnings := make([]string, 0, 1)
	if disconnectErr != nil {
		warnings = append(warnings, fmt.Sprintf("соединение с ККТ завершилось с предупреждением: %v", disconnectErr))
	}
	if collectErr != nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("не удалось получить данные ККТ Штрих по %s: %w", endpoint.ConnectionLabel(), collectErr)
	}
	if info == nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("драйвер Штрих не вернул данные по endpoint %s", endpoint.ConnectionLabel())
	}

	meta.DriverVersion = strings.TrimSpace(info.InstalledDriver)
	return payloadFromFiscalInfo(info), meta, warnings, nil
}

func configFromEndpoint(endpoint domain.Endpoint) (Config, error) {
	switch endpoint.Transport {
	case domain.TransportCOM:
		baudRate := strings.TrimSpace(endpoint.BaudRate)
		if baudRate == "" {
			baudRate = "115200"
		}

		baudIndex, ok := baudRateMap[baudRate]
		if !ok {
			return Config{}, fmt.Errorf("неподдерживаемый baudrate %q для COM-порта %s", baudRate, endpoint.COMPort)
		}

		comNumber, err := parseCOMNumber(endpoint.COMPort)
		if err != nil {
			return Config{}, err
		}

		return Config{
			ConnectionType: connectionTypeCOM,
			ComName:        endpoint.COMPort,
			ComNumber:      int32(comNumber),
			BaudRate:       baudIndex,
			Password:       defaultPassword,
		}, nil
	case domain.TransportTCP:
		return Config{
			ConnectionType: connectionTypeTCP,
			IPAddress:      endpoint.IP,
			TCPPort:        int32(endpoint.Port),
			Password:       defaultPassword,
		}, nil
	default:
		return Config{}, fmt.Errorf("неподдерживаемый transport %q", endpoint.Transport)
	}
}

func parseCOMNumber(name string) (int, error) {
	cleanName := strings.TrimSpace(strings.ToUpper(name))
	numberText, ok := strings.CutPrefix(cleanName, "COM")
	if !ok || numberText == "" {
		return 0, fmt.Errorf("некорректное имя COM-порта %q", name)
	}

	number, err := strconv.Atoi(numberText)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("некорректное имя COM-порта %q", name)
	}
	return number, nil
}

func payloadFromFiscalInfo(info *FiscalInfo) domain.FiscalPayload {
	return domain.FiscalPayload{
		ModelName:        strings.TrimSpace(info.ModelName),
		SerialNumber:     strings.TrimSpace(info.SerialNumber),
		RNM:              strings.TrimSpace(info.RNM),
		OrganizationName: strings.TrimSpace(info.OrganizationName),
		FNSerial:         strings.TrimSpace(info.FNSerial),
		DateTimeReg:      strings.TrimSpace(info.RegistrationDate),
		DateTimeEnd:      strings.TrimSpace(info.FNEndDate),
		OFDName:          strings.TrimSpace(info.OFDName),
		BootVersion:      strings.TrimSpace(info.SoftwareDate),
		FFDVersion:       strings.TrimSpace(info.FFDVersion),
		INN:              strings.TrimSpace(info.INN),
		Address:          strings.TrimSpace(info.Address),
		AttributeExcise:  formatPythonBool(info.AttributeExcise),
		AttributeMarked:  formatPythonBool(info.AttributeMarked),
		FNExecution:      strings.TrimSpace(info.FNExecution),
		InstalledDriver:  strings.TrimSpace(info.InstalledDriver),
		Licenses:         strings.TrimSpace(info.SubscriptionInfo),
	}
}

func formatPythonBool(value bool) string {
	if value {
		return "True"
	}
	return "False"
}
