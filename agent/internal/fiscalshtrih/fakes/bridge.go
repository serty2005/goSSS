package fakes

import (
	"context"
	"fmt"

	"etalon-agent/internal/fiscalshtrih/domain"
	"etalon-agent/internal/fiscalshtrih/drvfr"
)

type Bridge struct {
	ProbeResult drvfr.ProbeResult
	ProbeError  error
	Results     map[string]CollectResult
}

type CollectResult struct {
	Payload  domain.FiscalPayload
	Meta     drvfr.CollectMeta
	Warnings []string
	Err      error
}

func (b *Bridge) Probe(context.Context) (drvfr.ProbeResult, error) {
	return b.ProbeResult, b.ProbeError
}

func (b *Bridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, drvfr.CollectMeta, []string, error) {
	label := endpoint.ConnectionLabel()
	if label == "" {
		return domain.FiscalPayload{}, drvfr.CollectMeta{}, nil, fmt.Errorf("тестовый bridge не знает endpoint без connection_label")
	}

	result, ok := b.Results[label]
	if !ok {
		return domain.FiscalPayload{}, drvfr.CollectMeta{}, nil, fmt.Errorf("для endpoint %s не задан тестовый результат", label)
	}
	return result.Payload, result.Meta, result.Warnings, result.Err
}
