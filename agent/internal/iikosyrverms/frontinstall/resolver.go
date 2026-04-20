package frontinstall

import (
	"context"
	"errors"

	"etalon-agent/internal/iikosyrverms/domain"
)

var ErrNotFound = errors.New("установка фронта не найдена")

type Resolver interface {
	Resolve(context.Context, []domain.SoftwareType) (domain.FrontInstallation, error)
}
