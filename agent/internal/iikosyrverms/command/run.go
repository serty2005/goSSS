package command

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"etalon-agent/internal/iikosyrverms/contract"
	"etalon-agent/internal/iikosyrverms/domain"
	"etalon-agent/internal/iikosyrverms/frontinstall"
	"etalon-agent/internal/iikosyrverms/shutdown"
)

type RunHandler struct {
	Runner Runner
}

func (h RunHandler) Handle(ctx context.Context, request contract.RunRequest) contract.RunResponse {
	switch contract.NormalizeTaskType(request.TaskType) {
	case contract.TaskTypeCollect:
		return h.handleCollect(ctx)
	case contract.TaskTypeReadFrontConfig:
		return h.handleReadFrontConfig(ctx)
	case contract.TaskTypeSoftShutdownFront:
		return h.handleSoftShutdownFront(ctx)
	case contract.TaskTypeInspectAutorun:
		return h.handleInspectAutorun(ctx)
	case contract.TaskTypeEnsureAutorun:
		return h.handleEnsureAutorun(ctx, request.Payload)
	default:
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Неподдерживаемый task_type",
			Result:    contract.RunResult{SoftwareType: domain.SoftwareTypeUnknown},
			ErrorCode: "unsupported_task_type",
		}
	}
}

func (h RunHandler) handleCollect(ctx context.Context) contract.RunResponse {
	report, err := h.Runner.Collect(ctx)
	if err != nil {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Не удалось выполнить сбор данных iiko/syrve",
			Result:    contract.RunResult{SoftwareType: domain.SoftwareTypeUnknown},
			Details:   map[string]any{"scan_error": err.Error()},
			ErrorCode: "scan_failed",
		}
	}

	return contract.RunResponse{
		Status:   collectStatus(report),
		Message:  collectMessage(report),
		Result:   collectResult(report),
		Details:  detailsFromReport(report),
		Warnings: cloneWarnings(report.Warnings),
	}
}

func collectStatus(report domain.ScanReport) string {
	switch {
	case !report.Supported:
		return "failed"
	case !report.HasUsableRoots():
		return "failed"
	case !report.HasKnownSoftware():
		return "failed"
	case report.SoftwareType == domain.SoftwareTypeSyrve:
		return "partial"
	case report.RMSURL == "":
		return "partial"
	case report.CRMID == "":
		return "partial"
	case report.PluginsRoot == "":
		return "partial"
	case len(report.Warnings) > 0:
		return "partial"
	default:
		return "success"
	}
}

func collectMessage(report domain.ScanReport) string {
	switch {
	case !report.Supported:
		return "Текущее окружение не поддерживается адаптером"
	case !report.HasUsableRoots():
		return "Не удалось получить доступ к %AppData% и стабильным fallback-путям"
	case !report.HasKnownSoftware():
		return "Поддерживаемое ПО iiko/syrve не найдено"
	case report.SoftwareType == domain.SoftwareTypeSyrve:
		return "Syrve найден, но расширенный collect пока возвращает только частичный результат"
	case report.RMSURL == "":
		return "Поддерживаемое ПО найдено, но RMS URL извлечь не удалось"
	case report.CRMID == "":
		return "Поддерживаемое ПО найдено, но CRMID извлечь не удалось"
	case report.PluginsRoot == "":
		return "Поддерживаемое ПО найдено, но каталог Plugins определить не удалось"
	case len(report.Warnings) > 0:
		return "Сбор данных завершён частично"
	default:
		return "Сбор данных завершён успешно"
	}
}

func collectResult(report domain.ScanReport) contract.RunResult {
	return contract.RunResult{
		SoftwareType: report.SoftwareType,
		RMSURL:       report.RMSURL,
		CRMID:        report.CRMID,
		Plugins:      report.Plugins,
	}
}

func (h RunHandler) handleReadFrontConfig(ctx context.Context) contract.RunResponse {
	report, err := h.Runner.ReadFrontConfig(ctx)
	if err != nil {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Не удалось прочитать конфигурацию фронта",
			Result:    contract.RunResult{SoftwareType: domain.SoftwareTypeUnknown},
			Details:   map[string]any{"scan_error": err.Error()},
			ErrorCode: "scan_failed",
		}
	}

	status := "success"
	message := "Конфигурация фронта успешно прочитана"
	switch {
	case !report.Supported:
		status = "failed"
		message = "Текущее окружение не поддерживается адаптером"
	case !report.HasUsableRoots():
		status = "failed"
		message = "Не удалось получить доступ к %AppData% и стабильным fallback-путям"
	case !report.HasKnownSoftware():
		status = "failed"
		message = "Поддерживаемое ПО iiko/syrve не найдено"
	case report.ConfigSnapshot.SourceFile == "":
		status = "failed"
		message = "config.xml не найден"
	}

	return contract.RunResponse{
		Status:  status,
		Message: message,
		Result: contract.RunResult{
			SoftwareType: report.SoftwareType,
			SourceFile:   report.ConfigSnapshot.SourceFile,
			Settings:     report.ConfigSnapshot.Settings,
		},
		Details: detailsFromReport(report),
	}
}

func (h RunHandler) handleSoftShutdownFront(ctx context.Context) contract.RunResponse {
	result, err := h.Runner.SoftShutdownFront(ctx)
	if err != nil {
		errorCode := "shutdown_failed"
		message := "Не удалось выполнить мягкое завершение фронта"
		switch {
		case errors.Is(err, shutdown.ErrProcessNotFound):
			errorCode = "process_not_found"
			message = "Процесс фронта не найден"
		case errors.Is(err, shutdown.ErrWindowNotFound):
			errorCode = "window_not_found"
			message = "Процесс фронта найден, но видимые окна не обнаружены"
		case errors.Is(err, frontinstall.ErrNotFound):
			errorCode = "front_not_found"
			message = "Установку фронта определить не удалось"
		}
		return contract.RunResponse{
			Status:    "failed",
			Message:   message,
			Result:    softShutdownResult(result),
			Details:   map[string]any{"shutdown_error": err.Error()},
			ErrorCode: errorCode,
		}
	}

	return contract.RunResponse{
		Status:  "success",
		Message: "Команда мягкого завершения отправлена",
		Result:  softShutdownResult(result),
		Details: map[string]any{
			"matched_pids":    result.MatchedPIDs,
			"windows_closed":  result.WindowsClosed,
			"close_initiated": result.CloseSent,
		},
	}
}

func softShutdownResult(result domain.ShutdownResult) contract.RunResult {
	return contract.RunResult{
		SoftwareType:  result.SoftwareType,
		ProcessName:   result.ProcessName,
		MatchedPIDs:   result.MatchedPIDs,
		WindowsClosed: result.WindowsClosed,
		CloseSent:     result.CloseSent,
	}
}

func (h RunHandler) handleInspectAutorun(ctx context.Context) contract.RunResponse {
	result, err := h.Runner.InspectAutorun(ctx)
	if err != nil {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Не удалось проверить источники автозапуска",
			Result:    contract.RunResult{SoftwareType: domain.SoftwareTypeUnknown},
			Details:   map[string]any{"autorun_error": err.Error()},
			ErrorCode: "autorun_inspect_failed",
		}
	}

	message := "Источники автозапуска проверены"
	if len(result.Entries) == 0 {
		message = "Источники автозапуска проверены, запуск фронта не найден"
	}
	return contract.RunResponse{
		Status:  "success",
		Message: message,
		Result: contract.RunResult{
			SoftwareType: result.SoftwareType,
			Entries:      result.Entries,
		},
		Details: map[string]any{
			"entries_count": len(result.Entries),
		},
	}
}

func (h RunHandler) handleEnsureAutorun(ctx context.Context, rawPayload json.RawMessage) contract.RunResponse {
	var payload contract.AutorunEnsurePayload
	if len(strings.TrimSpace(string(rawPayload))) > 0 && string(rawPayload) != "null" {
		if err := json.Unmarshal(rawPayload, &payload); err != nil {
			return contract.RunResponse{
				Status:    "failed",
				Message:   "Невалидный payload для ensure_autorun",
				Result:    contract.RunResult{SoftwareType: domain.SoftwareTypeUnknown},
				ErrorCode: "invalid_payload",
				Details:   map[string]any{"payload_error": err.Error()},
			}
		}
	}
	if strings.TrimSpace(payload.Method) == "" {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Для ensure_autorun обязательно поле method",
			Result:    contract.RunResult{SoftwareType: payload.SoftwareType},
			ErrorCode: "invalid_payload",
		}
	}

	result, err := h.Runner.EnsureAutorun(ctx, payload)
	if err != nil {
		return contract.RunResponse{
			Status:    "failed",
			Message:   "Не удалось настроить автозапуск",
			Result:    ensureAutorunResult(result),
			Details:   map[string]any{"autorun_error": err.Error()},
			ErrorCode: "autorun_ensure_failed",
		}
	}

	message := "Автозапуск уже был настроен"
	switch {
	case result.Created:
		message = "Автозапуск успешно создан"
	case result.Updated:
		message = "Автозапуск успешно обновлён"
	}
	return contract.RunResponse{
		Status:  "success",
		Message: message,
		Result:  ensureAutorunResult(result),
		Details: map[string]any{
			"path":      result.Path,
			"task_name": result.TaskName,
		},
	}
}

func ensureAutorunResult(result domain.AutorunEnsureResult) contract.RunResult {
	return contract.RunResult{
		SoftwareType: result.SoftwareType,
		Method:       result.Method,
		Created:      result.Created,
		Updated:      result.Updated,
		Path:         result.Path,
		TaskName:     result.TaskName,
	}
}

func cloneWarnings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	return slices.Clone(items)
}
