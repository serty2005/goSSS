package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"etalon-agent/internal/iikosyrverms/command"
	"etalon-agent/internal/iikosyrverms/contract"
)

const (
	exitOK        = 0
	exitAppError  = 1
	exitBadInput  = 2
	exitCompatErr = 4
)

type App struct {
	version       string
	stdin         io.Reader
	stdout        io.Writer
	stderr        io.Writer
	describe      func(string) contract.DescribeResponse
	healthHandler command.HealthHandler
	runHandler    command.RunHandler
}

func New(version string, scanner command.Scanner) *App {
	return &App{
		version:       strings.TrimSpace(version),
		stdin:         os.Stdin,
		stdout:        os.Stdout,
		stderr:        os.Stderr,
		describe:      command.HandleDescribe,
		healthHandler: command.HealthHandler{Scanner: scanner},
		runHandler:    command.RunHandler{Scanner: scanner},
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

	if err := writeJSON(a.stdout, a.describe(a.version)); err != nil {
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

	if err := writeJSON(a.stdout, a.healthHandler.Handle(ctx)); err != nil {
		writeText(a.stderr, fmt.Sprintf("Не удалось записать ответ health: %v", err))
		return exitAppError
	}
	return exitOK
}

func (a *App) handleRun(ctx context.Context) int {
	var request contract.RunRequest
	if err := decodeJSON(a.stdin, &request); err != nil {
		return a.writeError(exitBadInput, contract.ErrorResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Невалидный JSON запроса run: %v", err),
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
	if strings.TrimSpace(strings.ToLower(request.TaskType)) != contract.TaskTypeCollect {
		return a.writeError(exitBadInput, contract.ErrorResponse{
			Status:    "failed",
			Message:   fmt.Sprintf("Неподдерживаемый task_type %q", request.TaskType),
			ErrorCode: "unsupported_task_type",
		})
	}

	if err := writeJSON(a.stdout, a.runHandler.Handle(ctx)); err != nil {
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
