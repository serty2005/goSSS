package contract

import (
	"encoding/json"
	"testing"

	"etalon-agent/internal/fiscalmitsu/domain"
)

func TestEndpointInputToDomainCOMDefaultBaudRate(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"com","com_port":"COM7"}`), &input); err != nil {
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

func TestEndpointInputToDomainTCPDefaultPort(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"tcp","ip":"10.127.1.124"}`), &input); err != nil {
		t.Fatalf("не удалось разобрать endpoint: %v", err)
	}

	endpoint, err := input.ToDomain()
	if err != nil {
		t.Fatalf("ToDomain вернул ошибку: %v", err)
	}
	if endpoint.Transport != domain.TransportTCP {
		t.Fatalf("ожидался transport=tcp, получено %q", endpoint.Transport)
	}
	if endpoint.Port != 8200 {
		t.Fatalf("ожидался порт по умолчанию 8200, получено %d", endpoint.Port)
	}
}

func TestEndpointInputToDomainTCPSupportsStringPort(t *testing.T) {
	t.Parallel()

	var input EndpointInput
	if err := json.Unmarshal([]byte(`{"transport":"tcp","ip":"10.127.1.124","port":"8300"}`), &input); err != nil {
		t.Fatalf("не удалось разобрать endpoint: %v", err)
	}

	endpoint, err := input.ToDomain()
	if err != nil {
		t.Fatalf("ToDomain вернул ошибку: %v", err)
	}
	if endpoint.Port != 8300 {
		t.Fatalf("ожидался порт 8300, получено %d", endpoint.Port)
	}
}
