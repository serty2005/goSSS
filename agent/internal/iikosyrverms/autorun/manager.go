package autorun

import (
	"context"

	"etalon-agent/internal/iikosyrverms/domain"
)

type EnsureRequest struct {
	Method       string
	SoftwareType domain.SoftwareType
	Installation domain.FrontInstallation
	Arguments    string
	TaskName     string
	ShortcutName string
}

type Manager interface {
	Inspect(context.Context) ([]domain.AutorunEntry, error)
	Ensure(context.Context, EnsureRequest) (domain.AutorunEnsureResult, error)
}
