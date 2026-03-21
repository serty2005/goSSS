package orchestrator

import (
	"context"

	"etalon-agent/internal/fiscalatol/domain"
	"etalon-agent/internal/fiscalatol/libfptr"
)

type Service struct {
	bridge libfptr.Bridge
}

func NewService(bridge libfptr.Bridge) *Service {
	return &Service{bridge: bridge}
}

func (s *Service) Collect(ctx context.Context, endpoints []domain.Endpoint) []domain.DeviceResult {
	results := make([]domain.DeviceResult, 0, len(endpoints))
	for _, endpoint := range endpoints {
		payload, meta, warnings, err := s.bridge.Collect(ctx, endpoint)
		result := domain.DeviceResult{
			Endpoint:        endpoint,
			ConnectionLabel: endpoint.ConnectionLabel(),
			Transport:       endpoint.Transport,
			Warnings:        warnings,
		}

		if meta.ConnectionLabel != "" {
			result.ConnectionLabel = meta.ConnectionLabel
		}
		if meta.Transport != "" {
			result.Transport = meta.Transport
		}
		result.DriverVersion = meta.DriverVersion

		if err != nil {
			result.Status = domain.DeviceStatusFailed
			result.Message = "Не удалось собрать данные по ККТ Атол"
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		result.Status = domain.DeviceStatusSuccess
		result.Message = "Данные по ККТ Атол собраны"
		if len(warnings) > 0 {
			result.Message = "Данные по ККТ Атол собраны с предупреждениями"
		}
		result.Payload = &payload
		results = append(results, result)
	}
	return results
}

func OverallStatus(results []domain.DeviceResult) string {
	if len(results) == 0 {
		return "failed"
	}

	successCount := 0
	for _, result := range results {
		if result.Status == domain.DeviceStatusSuccess {
			successCount++
		}
	}

	switch {
	case successCount == len(results):
		return "success"
	case successCount == 0:
		return "failed"
	default:
		return "partial"
	}
}
