package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"etalon-agent/internal/fiscalshtrih/contract"
	"etalon-agent/internal/fiscalshtrih/domain"
	"etalon-agent/internal/fiscalshtrih/drvfr"
	"etalon-agent/internal/fiscalshtrih/orchestrator"
)

const (
	exitOK        = 0
	exitAppError  = 1
	exitBadInput  = 2
	exitCompatErr = 4
)

type App struct {
	version string
	stdin   io.Reader
	stdout  io.Writer
	stderr  io.Writer
	bridge  drvfr.Bridge
	service *orchestrator.Service
}

func New(version string, bridge drvfr.Bridge) *App {
	return &App{
		version: strings.TrimSpace(version),
		stdin:   os.Stdin,
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		bridge:  bridge,
		service: orchestrator.NewService(bridge),
	}
}

func (a *App) Execute(ctx context.Context, args []string) int {
	if len(args) != 1 {
		writeText(a.stderr, "Ожидалась одна команда адаптера: describe, health или run")
		return exitBadInput
	}

	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "describe":
		return a.handleDescribe()
	case "health":
		return a.handleHealth(ctx)
	case "run":
		return a.handleRun(ctx)
	default:
		writeText(a.stderr, fmt.Sprintf("Неизвестная команда адаптера %q", args[0]))
		return exitBadInput
	}
}

func (a *App) handleDescribe() int {
	var request contract.DescribeRequest
	if err := decodeJSON(a.stdin, &request); err != nil {
		return a.writeError(exitBadInput, contract.ErrorResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Невалидный JSON запроса describe: %v", err),
			ErrorCode: "invalid_json",
		})
	}
	if err := contract.EnsureProtocol(request.ProtocolVersion); err != nil {
		return a.writeError(exitCompatErr, contract.ErrorResponse{
			Status:    "failed",
			Message:   err.Error(),
			ErrorCode: "unsupported_protocol",
		})
	}

	response := contract.NewDescribeResponse(a.version)
	if err := writeJSON(a.stdout, response); err != nil {
		writeText(a.stderr, fmt.Sprintf("Не удалось записать ответ describe: %v", err))
		return exitAppError
	}
	return exitOK
}

func (a *App) handleHealth(ctx context.Context) int {
	var request contract.HealthRequest
	if err := decodeJSON(a.stdin, &request); err != nil {
		return a.writeError(exitBadInput, contract.ErrorResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Невалидный JSON запроса health: %v", err),
			ErrorCode: "invalid_json",
		})
	}
	if err := contract.EnsureProtocol(request.ProtocolVersion); err != nil {
		return a.writeError(exitCompatErr, contract.ErrorResponse{
			Status:    "failed",
			Message:   err.Error(),
			ErrorCode: "unsupported_protocol",
		})
	}

	probe, err := a.bridge.Probe(ctx)
	if err != nil {
		response := contract.HealthResponse{
			Status:  "error",
			Message: fmt.Sprintf("Не удалось проверить готовность драйвера Штрих: %v", err),
			Details: map[string]any{
				"driver_present": false,
			},
			ErrorCode: "probe_failed",
		}
		if writeErr := writeJSON(a.stdout, response); writeErr != nil {
			writeText(a.stderr, fmt.Sprintf("Не удалось записать ответ health: %v", writeErr))
			return exitAppError
		}
		return exitOK
	}

	response := contract.HealthResponse{
		Status:  "ok",
		Message: "Адаптер готов к работе",
		Details: map[string]any{
			"supported":      probe.Supported,
			"driver_present": probe.DriverPresent,
			"required_os":    probe.RequiredOS,
			"required_arch":  probe.RequiredArch,
			"driver_progid":  probe.DriverProgID,
		},
	}
	if probe.DriverVersion != "" {
		response.Details["driver_version"] = probe.DriverVersion
	}
	if probe.Message != "" {
		response.Details["driver_message"] = probe.Message
	}

	switch {
	case !probe.Supported:
		response.Status = "error"
		response.Message = "Текущая сборка адаптера не поддерживает это окружение"
	case !probe.DriverPresent:
		response.Status = "degraded"
		response.Message = "Драйвер Штрих не найден"
	case probe.DriverVersion == "":
		response.Status = "degraded"
		response.Message = "Драйвер Штрих найден, но его версия не определена"
	}

	if err := writeJSON(a.stdout, response); err != nil {
		writeText(a.stderr, fmt.Sprintf("Не удалось записать ответ health: %v", err))
		return exitAppError
	}
	return exitOK
}

func (a *App) handleRun(ctx context.Context) int {
	var request contract.RunRequest
	if err := decodeJSON(a.stdin, &request); err != nil {
		return a.writeError(exitBadInput, contract.RunResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Невалидный JSON запроса run: %v", err),
			ErrorCode: "invalid_json",
		})
	}
	if err := contract.EnsureProtocol(request.ProtocolVersion); err != nil {
		return a.writeError(exitCompatErr, contract.RunResponse{
			Status:    "failed",
			Message:   err.Error(),
			ErrorCode: "unsupported_protocol",
		})
	}
	if strings.TrimSpace(strings.ToLower(request.TaskType)) != contract.TaskTypeCollect {
		return a.writeError(exitBadInput, contract.RunResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Неподдерживаемый task_type %q", request.TaskType),
			ErrorCode: "unsupported_task_type",
		})
	}
	if len(request.Payload.Devices) == 0 {
		return a.writeError(exitBadInput, contract.RunResponse{
			Status:    "failed",
			Message:   "payload.devices не должен быть пустым",
			ErrorCode: "empty_devices",
		})
	}

	results := make([]domain.DeviceResult, len(request.Payload.Devices))
	validEndpoints := make([]domain.Endpoint, 0, len(request.Payload.Devices))
	validPositions := make([]int, 0, len(request.Payload.Devices))
	for idx, input := range request.Payload.Devices {
		endpoint, err := input.ToDomain()
		if err != nil {
			results[idx] = domain.DeviceResult{
				Endpoint:        endpoint,
				Status:          domain.DeviceStatusFailed,
				Message:         "Endpoint отклонен до запуска сбора",
				Error:           err.Error(),
				ConnectionLabel: endpoint.ConnectionLabel(),
				Transport:       endpoint.Transport,
			}
			continue
		}
		validEndpoints = append(validEndpoints, endpoint)
		validPositions = append(validPositions, idx)
	}

	validResults := a.service.Collect(ctx, validEndpoints)
	for idx, result := range validResults {
		results[validPositions[idx]] = result
	}

	status := orchestrator.OverallStatus(results)
	response := contract.RunResponse{
		Status:  status,
		Message: runStatusMessage(status),
		Result: &domain.CollectResult{
			Devices: results,
		},
	}
	if err := writeJSON(a.stdout, response); err != nil {
		writeText(a.stderr, fmt.Sprintf("Не удалось записать ответ run: %v", err))
		return exitAppError
	}
	return exitOK
}

func (a *App) writeError(exitCode int, response any) int {
	if err := writeJSON(a.stdout, response); err != nil {
		writeText(a.stderr, fmt.Sprintf("Не удалось записать JSON ошибки: %v", err))
		return exitAppError
	}
	return exitCode
}

func decodeJSON(reader io.Reader, target any) error {
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(target); err != nil {
		return err
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("ожидался один JSON-документ")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func writeText(writer io.Writer, message string) {
	_, _ = io.WriteString(writer, strings.TrimSpace(message)+"\n")
}

func runStatusMessage(status string) string {
	switch status {
	case "success":
		return "Сбор данных по всем endpoint завершён успешно"
	case "partial":
		return "Сбор данных завершён частично"
	default:
		return "Сбор данных завершился ошибками"
	}
}
