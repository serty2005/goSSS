package contract

import (
	"encoding/json"
	"testing"

	"etalon-agent/internal/fiscalshtrih/domain"
)

func TestEndpointInputToDomainCOMDefaultBaudRate(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"com","com_port":"COM4"}`), &input); err != nil {
		t.Fatalf("не удалось разобрать endpoint: %v", err)
	}

	endpoint, err := input.ToDomain()
	if err != nil {
		t.Fatalf("ToDomain вернул ошибку: %v", err)
	}
	if endpoint.Transport != domain.TransportCOM {
		t.Fatalf("ожидался transport=com, получено %q", endpoint.Transport)
	}
	if endpoint.BaudRate != "115200" {
		t.Fatalf("ожидался baudrate по умолчанию 115200, получено %q", endpoint.BaudRate)
	}
}

func TestEndpointInputToDomainTCPSupportsStringPort(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"tcp","ip":"10.25.1.22","port":"5555"}`), &input); err != nil {
		t.Fatalf("не удалось разобрать endpoint: %v", err)
	}

	endpoint, err := input.ToDomain()
	if err != nil {
		t.Fatalf("ToDomain вернул ошибку: %v", err)
	}
	if endpoint.Transport != domain.TransportTCP {
		t.Fatalf("ожидался transport=tcp, получено %q", endpoint.Transport)
	}
	if endpoint.Port != 5555 {
		t.Fatalf("ожидался порт 5555, получено %d", endpoint.Port)
	}
}

func TestEndpointInputToDomainRejectsInvalidTransport(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"usb"}`), &input); err != nil {
		t.Fatalf("не удалось разобрать endpoint: %v", err)
	}

	if _, err := input.ToDomain(); err == nil {
		t.Fatal("ожидалась ошибка для неподдерживаемого transport")
	}
}
