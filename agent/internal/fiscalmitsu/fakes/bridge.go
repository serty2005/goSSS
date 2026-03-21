package fakes

import (
	"context"
	"fmt"

	"etalon-agent/internal/fiscalmitsu/domain"
	"etalon-agent/internal/fiscalmitsu/protocol"
)

type Bridge struct {
	ProbeResult protocol.ProbeResult
	ProbeError  error
	Results     map[string]CollectResult
}

type CollectResult struct {
	Payload  domain.FiscalPayload
	Meta     protocol.CollectMeta
	Warnings []string
	Err      error
}

func (b *Bridge) Probe(context.Context) (protocol.ProbeResult, error) {
	return b.ProbeResult, b.ProbeError
}

func (b *Bridge) Collect(_ context.Context, endpoint domain.Endpoint) (domain.FiscalPayload, protocol.CollectMeta, []string, error) {
	label := endpoint.ConnectionLabel()
	if label == "" {
		return domain.FiscalPayload{}, protocol.CollectMeta{}, nil, fmt.Errorf("тестовый bridge не знает endpoint без connection_label")
	}

	result, ok := b.Results[label]
	if !ok {
		return domain.FiscalPayload{}, protocol.CollectMeta{}, nil, fmt.Errorf("для endpoint %s не задан тестовый результат", label)
	}
	return result.Payload, result.Meta, result.Warnings, result.Err
}
