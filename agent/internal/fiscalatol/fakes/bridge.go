package fakes

import (
	"context"
	"fmt"

	"etalon-agent/internal/fiscalatol/domain"
	"etalon-agent/internal/fiscalatol/libfptr"
)

type Bridge struct {
	ProbeResult libfptr.ProbeResult
	ProbeError  error
	Results     map[string]CollectResult
}

type CollectResult struct {
	Payload  domain.FiscalPayload
	Meta     libfptr.CollectMeta
	Warnings []string
	Err      error
}

func (b *Bridge) Probe(context.Context) (libfptr.ProbeResult, error) {
	return b.ProbeResult, b.ProbeError
}

func (b *Bridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, libfptr.CollectMeta, []string, error) {
	label := endpoint.ConnectionLabel()
	if label == "" {
		return domain.FiscalPayload{}, libfptr.CollectMeta{}, nil, fmt.Errorf("тестовый bridge не знает endpoint без connection_label")
	}

	result, ok := b.Results[label]
	if !ok {
		return domain.FiscalPayload{}, libfptr.CollectMeta{}, nil, fmt.Errorf("для endpoint %s не задан тестовый результат", label)
	}
	return result.Payload, result.Meta, result.Warnings, result.Err
}
