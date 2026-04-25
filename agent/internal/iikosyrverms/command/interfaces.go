package command

import (
	"context"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
)

type Scanner interface {
	Scan(context.Context) (domain.ScanReport, error)
}

type Runner interface {
	Collect(context.Context) (domain.ScanReport, error)
	ReadFrontConfig(context.Context) (domain.ScanReport, error)
	SoftShutdownFront(context.Context) (domain.ShutdownResult, error)
	InspectAutorun(context.Context) (domain.AutorunInspectionResult, error)
	EnsureAutorun(context.Context, contract.AutorunEnsurePayload) (domain.AutorunEnsureResult, error)
}

type Service interface {
	Scanner
	Runner
}
