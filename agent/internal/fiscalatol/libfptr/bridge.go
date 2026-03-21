package libfptr

import (
	"context"

	"etalon-agent/internal/fiscalatol/domain"
)

type Variant string

const (
	Variant108 Variant = "10.8"
	Variant109 Variant = "10.9+"
)

type ProbeResult struct {
	Supported     bool
	DriverPresent bool
	DriverPath    string
	DriverVersion string
	DriverVariant Variant
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
