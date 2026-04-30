//go:build !windows

package frontinstall

import (
	"context"

	"etalon-agent/internal/iikosyrverms/domain"
)

type stubResolver struct{}

func New() Resolver {
	return stubResolver{}
}

func (stubResolver) Resolve(context.Context, []domain.SoftwareType) (domain.FrontInstallation, error) {
	return domain.FrontInstallation{}, ErrNotFound
}
