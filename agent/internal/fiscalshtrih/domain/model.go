package domain

import (
	"fmt"
	"strconv"
	"strings"
)

type Transport string

const (
	TransportCOM Transport = "com"
	TransportTCP Transport = "tcp"
)

const (
	DeviceStatusSuccess = "success"
	DeviceStatusFailed  = "failed"
)

type Endpoint struct {
	Transport Transport `json:"transport"`
	COMPort   string    `json:"com_port,omitempty"`
	BaudRate  string    `json:"baudrate,omitempty"`
	IP        string    `json:"ip,omitempty"`
	Port      int       `json:"port,omitempty"`
}

func (e Endpoint) ConnectionLabel() string {
	switch e.Transport {
	case TransportCOM:
		return e.COMPort
	case TransportTCP:
		if e.IP == "" || e.Port == 0 {
			return ""
		}
		return fmt.Sprintf("%s:%d", e.IP, e.Port)
	default:
		return ""
	}
}

func (e Endpoint) Validate() error {
	switch e.Transport {
	case TransportCOM:
		if strings.TrimSpace(e.COMPort) == "" {
			return fmt.Errorf("для transport=com требуется поле com_port")
		}
		if strings.TrimSpace(e.BaudRate) == "" {
			return fmt.Errorf("для transport=com требуется валидный baudrate")
		}
		if _, err := strconv.Atoi(strings.TrimSpace(e.BaudRate)); err != nil {
			return fmt.Errorf("baudrate должен быть целым числом: %w", err)
		}
		return nil
	case TransportTCP:
		if strings.TrimSpace(e.IP) == "" {
			return fmt.Errorf("для transport=tcp требуется поле ip")
		}
		if e.Port <= 0 || e.Port > 65535 {
			return fmt.Errorf("для transport=tcp требуется порт в диапазоне 1..65535")
		}
		return nil
	default:
		return fmt.Errorf("неподдерживаемый transport %q", e.Transport)
	}
}

type FiscalPayload struct {
	ModelName        string `json:"modelName"`
	SerialNumber     string `json:"serialNumber"`
	RNM              string `json:"RNM"`
	OrganizationName string `json:"organizationName"`
	FNSerial         string `json:"fn_serial"`
	DateTimeReg      string `json:"datetime_reg"`
	DateTimeEnd      string `json:"dateTime_end"`
	OFDName          string `json:"ofdName"`
	BootVersion      string `json:"bootVersion"`
	FFDVersion       string `json:"ffdVersion"`
	INN              string `json:"INN"`
	Address          string `json:"address"`
	AttributeExcise  string `json:"attribute_excise"`
	AttributeMarked  string `json:"attribute_marked"`
	FNExecution      string `json:"fnExecution"`
	InstalledDriver  string `json:"installed_driver"`
	Licenses         string `json:"licenses"`
}

type DeviceResult struct {
	Endpoint        Endpoint       `json:"endpoint"`
	Status          string         `json:"status"`
	Message         string         `json:"message,omitempty"`
	Warnings        []string       `json:"warnings,omitempty"`
	Error           string         `json:"error,omitempty"`
	Payload         *FiscalPayload `json:"payload,omitempty"`
	ConnectionLabel string         `json:"connection_label,omitempty"`
	Transport       Transport      `json:"transport,omitempty"`
	DriverVersion   string         `json:"driver_version,omitempty"`
}

type CollectResult struct {
	Devices []DeviceResult `json:"devices"`
}
