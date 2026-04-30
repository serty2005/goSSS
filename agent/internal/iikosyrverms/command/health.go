package command

import (
	"context"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
)

type HealthHandler struct {
	Scanner Scanner
}

func (h HealthHandler) Handle(ctx context.Context) contract.HealthResponse {
	report, err := h.Scanner.Scan(ctx)
	if err != nil {
		return contract.HealthResponse{
			Status:    "error",
			Message:   "Не удалось выполнить проверку окружения адаптера",
			Details:   map[string]any{"scan_error": err.Error()},
			ErrorCode: "scan_failed",
		}
	}

	response := contract.HealthResponse{
		Status:  healthStatus(report),
		Message: healthMessage(report),
		Details: detailsFromReport(report),
	}
	return response
}

func healthStatus(report domain.ScanReport) string {
	switch {
	case !report.Supported:
		return "error"
	case !report.HasUsableRoots():
		return "error"
	case !report.HasKnownSoftware():
		return "degraded"
	case report.RMSURL == "":
		return "degraded"
	default:
		return "ok"
	}
}

func healthMessage(report domain.ScanReport) string {
	switch {
	case !report.Supported:
		return "Текущее окружение не поддерживается адаптером"
	case !report.HasUsableRoots():
		return "Не удалось получить доступ к %AppData% и стабильным fallback-путям"
	case !report.HasKnownSoftware():
		return "Поддерживаемое ПО iiko/syrve не найдено"
	case report.RMSURL == "":
		return "Поддерживаемое ПО найдено, но RMS URL извлечь не удалось"
	default:
		return "Адаптер готов к работе"
	}
}
