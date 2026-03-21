//go:build !windows

package libfptr

import (
	"context"
	"fmt"

	"etalon-agent/internal/fiscalatol/domain"
)

type unsupportedBridge struct{}

func NewBridge() Bridge {
	return &unsupportedBridge{}
}

func (b *unsupportedBridge) Probe(context.Context) (ProbeResult, error) {
	return ProbeResult{
		Supported:     false,
		DriverPresent: false,
		Message:       "Адаптер Атол в текущей реализации поддерживается только на Windows",
	}, nil
}

func (b *unsupportedBridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, CollectMeta, []string, error) {
	return domain.FiscalPayload{}, CollectMeta{
		ConnectionLabel: endpoint.ConnectionLabel(),
		Transport:       endpoint.Transport,
	}, nil, fmt.Errorf("адаптер Атол в текущей реализации поддерживается только на Windows")
}
