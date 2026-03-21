//go:build !windows || !386

package drvfr

import (
	"context"
	"fmt"
)

type unsupportedRuntime struct{}

type unsupportedDriver struct{}

func newRuntime() runtimeAPI {
	return unsupportedRuntime{}
}

func (unsupportedRuntime) Probe(context.Context) (ProbeResult, error) {
	return ProbeResult{
		Supported:     false,
		DriverPresent: false,
		DriverProgID:  driverProgID,
		RequiredOS:    "windows",
		RequiredArch:  "386",
		Message:       "Адаптер Штрих в текущей реализации поддерживается только в сборке Windows x86",
	}, nil
}

func (unsupportedRuntime) NewDriver(Config) Driver {
	return unsupportedDriver{}
}

func (unsupportedDriver) Connect() error {
	return fmt.Errorf("адаптер Штрих в текущей реализации поддерживается только в сборке Windows x86")
}

func (unsupportedDriver) Disconnect() error {
	return nil
}

func (unsupportedDriver) GetFiscalInfo() (*FiscalInfo, error) {
	return nil, fmt.Errorf("адаптер Штрих в текущей реализации поддерживается только в сборке Windows x86")
}
