package command

import (
	"context"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
)

type RunHandler struct {
	Scanner Scanner
}

func (h RunHandler) Handle(ctx context.Context) contract.RunResponse {
	report, err := h.Scanner.Scan(ctx)
	if err != nil {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Не удалось выполнить поиск iiko/syrve конфигурации",
			Result:    resultFromReport(domain.ScanReport{SoftwareType: domain.SoftwareTypeUnknown}),
			Details:   map[string]any{"scan_error": err.Error()},
			ErrorCode: "scan_failed",
		}
	}

	return contract.RunResponse{
		Status:  runStatus(report),
		Message: runMessage(report),
		Result:  resultFromReport(report),
		Details: detailsFromReport(report),
	}
}

func runStatus(report domain.ScanReport) string {
	switch {
	case !report.Supported:
		return "failed"
	case !report.HasUsableRoots():
		return "failed"
	case !report.HasKnownSoftware():
		return "failed"
	case report.RMSURL == "":
		return "partial"
	default:
		return "success"
	}
}

func runMessage(report domain.ScanReport) string {
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
		return "RMS URL успешно определён"
	}
}

func resultFromReport(report domain.ScanReport) contract.RunResult {
	return contract.RunResult{
		RMSURL:       report.RMSURL,
		SoftwareType: report.SoftwareType,
	}
}
