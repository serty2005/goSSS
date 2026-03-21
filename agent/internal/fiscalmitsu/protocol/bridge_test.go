package protocol

import (
	"context"
	"errors"
	"testing"

	"etalon-agent/internal/fiscalmitsu/domain"
)

type fakeRuntime struct {
	probeResult ProbeResult
	probeErr    error
	responses   map[string]string
	sendErr     map[string]error
	requests    []string
}

func (r *fakeRuntime) Probe(context.Context) (ProbeResult, error) {
	return r.probeResult, r.probeErr
}

func (r *fakeRuntime) SendCommand(_ context.Context, _ domain.Endpoint, command string) (string, error) {
	r.requests = append(r.requests, command)
	if err, ok := r.sendErr[command]; ok {
		return "", err
	}
	return r.responses[command], nil
}

func TestBridgeCollectMapsPayload(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{
		probeResult: ProbeResult{
			Supported:     true,
			DriverPresent: true,
			DriverVersion: "1.2.3.4",
		},
		responses: map[string]string{
			getModelCommand:   `<OK DEV='Mitsu M' />`,
			getVersionCommand: `<OK SERIAL='1234567890' VER='3.5.7' />`,
			getRegDataCommand: `<OK T1188='Ф' T1037='0000000001234567' DATE='2024-03-04 10:11:12' T1209='4' T1018='7701234567' ExtMODE='17'><T1048>ООО &quot;Ромашка&quot;</T1048><T1046>ОФД Тест</T1046><T1009>г. Москва</T1009></OK>`,
			getFNDataCommand:  `<OK FN='9999078900000001' VALID='2025-03-04 10:11:12' EDITION='ФН-1.2' />`,
		},
	}

	endpoint := domain.Endpoint{
		Transport: domain.TransportTCP,
		IP:        "10.127.1.124",
		Port:      8200,
	}

	payload, meta, warnings, err := newBridgeWithRuntime(runtime).Collect(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("Collect вернул ошибку: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("не ожидались предупреждения, получено %v", warnings)
	}
	if len(runtime.requests) != 4 {
		t.Fatalf("ожидались 4 команды Mitsu, получено %d", len(runtime.requests))
	}
	if payload.ModelName != "Mitsu M Ф" {
		t.Fatalf("ожидался modelName Mitsu M Ф, получено %q", payload.ModelName)
	}
	if payload.OrganizationName != `ООО "Ромашка"` {
		t.Fatalf("ожидалось декодированное имя организации, получено %q", payload.OrganizationName)
	}
	if payload.AttributeExcise != "True" || payload.AttributeMarked != "True" {
		t.Fatalf("ожидались attribute_excise=True и attribute_marked=True, получено %q и %q", payload.AttributeExcise, payload.AttributeMarked)
	}
	if payload.FFDVersion != "120" {
		t.Fatalf("ожидалась FFD 120, получено %q", payload.FFDVersion)
	}
	if payload.InstalledDriver != "1.2.3.4" {
		t.Fatalf("ожидалась версия установленного драйвера 1.2.3.4, получено %q", payload.InstalledDriver)
	}
	if payload.Licenses != "None" {
		t.Fatalf("ожидалось licenses=None, получено %q", payload.Licenses)
	}
	if meta.DriverVersion != "1.2.3.4" {
		t.Fatalf("ожидалась meta driver_version 1.2.3.4, получено %q", meta.DriverVersion)
	}
}

func TestBridgeCollectKeepsWorkingWithoutDriverVersion(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{
		probeResult: ProbeResult{
			Supported: false,
		},
		responses: map[string]string{
			getModelCommand:   `<OK DEV='Mitsu M' />`,
			getVersionCommand: `<OK SERIAL='1234567890' VER='3.5.7' />`,
			getRegDataCommand: `<OK T1188='Ф' T1037='0000000001234567' DATE='2024-03-04 10:11:12' T1209='4' T1018='7701234567' ExtMODE='0'><T1048>ООО Тест</T1048><T1046>ОФД Тест</T1046><T1009>г. Москва</T1009></OK>`,
			getFNDataCommand:  `<OK FN='9999078900000001' VALID='2025-03-04 10:11:12' EDITION='ФН-1.2' />`,
		},
	}

	payload, _, warnings, err := newBridgeWithRuntime(runtime).Collect(context.Background(), domain.Endpoint{
		Transport: domain.TransportCOM,
		COMPort:   "COM7",
		BaudRate:  "115200",
	})
	if err != nil {
		t.Fatalf("Collect вернул ошибку: %v", err)
	}
	if payload.InstalledDriver != "Error" {
		t.Fatalf("ожидалось installed_driver=Error, получено %q", payload.InstalledDriver)
	}
	if len(warnings) == 0 {
		t.Fatal("ожидалось предупреждение о недоступной версии MitsuCube.exe")
	}
}

func TestBridgeCollectReturnsTransportError(t *testing.T) {
	t.Parallel()

	runtime := &fakeRuntime{
		probeResult: ProbeResult{Supported: true},
		sendErr: map[string]error{
			getModelCommand: errors.New("порт недоступен"),
		},
	}

	_, _, _, err := newBridgeWithRuntime(runtime).Collect(context.Background(), domain.Endpoint{
		Transport: domain.TransportTCP,
		IP:        "10.127.1.124",
		Port:      8200,
	})
	if err == nil {
		t.Fatal("ожидалась ошибка transport-слоя")
	}
}
