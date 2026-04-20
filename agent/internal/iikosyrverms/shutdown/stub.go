//go:build !windows

package shutdown

import (
	"context"
	"fmt"

	"etalon-agent/internal/iikosyrverms/domain"
)

type Controller struct{}

func New() *Controller {
	return &Controller{}
}

func (c *Controller) SoftShutdown(context.Context, domain.SoftwareType, string) (domain.ShutdownResult, error) {
	return domain.ShutdownResult{}, fmt.Errorf("мягкое завершение фронта поддерживается только на Windows")
}
