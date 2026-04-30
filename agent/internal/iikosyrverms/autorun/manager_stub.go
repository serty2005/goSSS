//go:build !windows

package autorun

import (
	"context"
	"fmt"

	"etalon-agent/internal/iikosyrverms/domain"
)

type stubManager struct{}

func New() Manager {
	return stubManager{}
}

func (stubManager) Inspect(context.Context) ([]domain.AutorunEntry, error) {
	return nil, fmt.Errorf("проверка автозапуска поддерживается только на Windows")
}

func (stubManager) Ensure(context.Context, EnsureRequest) (domain.AutorunEnsureResult, error) {
	return domain.AutorunEnsureResult{}, fmt.Errorf("настройка автозапуска поддерживается только на Windows")
}
