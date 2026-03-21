package protocol

import (
	"context"
	"fmt"

	"etalon-agent/internal/fiscalmitsu/domain"
)

const (
	getModelCommand   = "<GET DEV='?' />"
	getVersionCommand = "<GET VER='?' />"
	getRegDataCommand = "<GET REG='?'/>"
	getFNDataCommand  = "<GET INFO='FN'/>"
	driverNotFound    = "Error"
)

type ProbeResult struct {
	Supported     bool
	DriverPresent bool
	DriverPath    string
	DriverVersion string
	SearchPaths   []string
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

type runtimeAPI interface {
	Probe(context.Context) (ProbeResult, error)
	SendCommand(context.Context, domain.Endpoint, string) (string, error)
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

func (b *runtimeBridge) Collect(ctx context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, CollectMeta, []string, error) {
	meta := CollectMeta{
		ConnectionLabel: endpoint.ConnectionLabel(),
		Transport:       endpoint.Transport,
	}
	warnings := make([]string, 0, 2)

	probe, probeErr := b.runtime.Probe(ctx)
	installedDriver := driverNotFound
	if probeErr != nil {
		warnings = append(warnings, fmt.Sprintf("не удалось определить установленную версию MitsuCube.exe: %v", probeErr))
	} else {
		meta.DriverVersion = probe.DriverVersion
		if probe.DriverVersion != "" {
			installedDriver = probe.DriverVersion
		} else if !probe.DriverPresent {
			warnings = append(warnings, "MitsuCube.exe не найден, поле installed_driver возвращено как Error")
		} else {
			warnings = append(warnings, "версия MitsuCube.exe не определена, поле installed_driver возвращено как Error")
		}
	}

	model, err := b.runtime.SendCommand(ctx, endpoint, getModelCommand)
	if err != nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("не удалось получить модель Mitsu по endpoint %s: %w", endpoint.ConnectionLabel(), err)
	}
	version, err := b.runtime.SendCommand(ctx, endpoint, getVersionCommand)
	if err != nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("не удалось получить версию Mitsu по endpoint %s: %w", endpoint.ConnectionLabel(), err)
	}
	regData, err := b.runtime.SendCommand(ctx, endpoint, getRegDataCommand)
	if err != nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("не удалось получить регистрационные данные Mitsu по endpoint %s: %w", endpoint.ConnectionLabel(), err)
	}
	fnData, err := b.runtime.SendCommand(ctx, endpoint, getFNDataCommand)
	if err != nil {
		return domain.FiscalPayload{}, meta, warnings, fmt.Errorf("не удалось получить данные ФН Mitsu по endpoint %s: %w", endpoint.ConnectionLabel(), err)
	}

	payload, err := buildPayload(model, version, regData, fnData, installedDriver)
	if err != nil {
		return domain.FiscalPayload{}, meta, warnings, err
	}
	return payload, meta, warnings, nil
}
