package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
)

type CommandRequest struct {
	Executable string
	Args       []string
	WorkingDir string
	Stdin      []byte
	Env        map[string]string
}

type CommandResult struct {
	StartedAt   time.Time
	CompletedAt time.Time
	ExitCode    *int
	Stdout      string
	Stderr      string
}

type CommandRunner interface {
	Run(context.Context, CommandRequest) (CommandResult, error)
}

type OSCommandRunner struct{}

func (OSCommandRunner) Run(ctx context.Context, req CommandRequest) (CommandResult, error) {
	command := exec.CommandContext(ctx, req.Executable, req.Args...)
	command.Dir = strings.TrimSpace(req.WorkingDir)
	if len(req.Stdin) > 0 {
		command.Stdin = bytes.NewReader(req.Stdin)
	}

	env := os.Environ()
	for key, value := range req.Env {
		env = append(env, key+"="+value)
	}
	command.Env = env

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	startedAt := time.Now().UTC()
	err := command.Run()
	completedAt := time.Now().UTC()

	result := CommandResult{
		StartedAt:   startedAt,
		CompletedAt: completedAt,
		Stdout:      stdout.String(),
		Stderr:      stderr.String(),
	}
	if command.ProcessState != nil {
		exitCode := command.ProcessState.ExitCode()
		result.ExitCode = &exitCode
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return result, nil
		}
		return result, err
	}
	if result.ExitCode == nil {
		exitCode := 0
		result.ExitCode = &exitCode
	}
	return result, nil
}

func NewExternalCommandHandler(runner CommandRunner) saga.StepHandler {
	if runner == nil {
		runner = OSCommandRunner{}
	}

	return saga.StepHandlerFunc(func(ctx context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		input, err := decodeExternalCommandInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}
		if strings.TrimSpace(input.Executable) == "" {
			return saga.StepOutcome{}, fmt.Errorf("в input шага %s отсутствует executable", step.ID)
		}

		result, runErr := runner.Run(ctx, CommandRequest{
			Executable: strings.TrimSpace(input.Executable),
			Args:       append([]string(nil), input.Args...),
			WorkingDir: strings.TrimSpace(input.WorkingDir),
			Stdin:      cloneBytes(input.Stdin),
			Env:        cloneEnv(input.Env),
		})
		output := mustMarshalJSON(map[string]any{
			"status":       buildCommandStatus(result.ExitCode, runErr),
			"executable":   strings.TrimSpace(input.Executable),
			"args":         append([]string(nil), input.Args...),
			"working_dir":  strings.TrimSpace(input.WorkingDir),
			"duration_ms":  result.CompletedAt.Sub(result.StartedAt).Milliseconds(),
			"exit_code":    cloneIntPointer(result.ExitCode),
			"stdout":       result.Stdout,
			"stderr":       result.Stderr,
			"completed_at": result.CompletedAt.Format(time.RFC3339),
		})
		switch {
		case runErr != nil:
			return saga.StepOutcome{}, &saga.StepExecutionError{Err: runErr, Output: output}
		case result.ExitCode != nil && *result.ExitCode != 0:
			return saga.StepOutcome{}, &saga.StepExecutionError{
				Err:    fmt.Errorf("внешняя команда завершилась с кодом %d", *result.ExitCode),
				Output: output,
			}
		default:
			return saga.StepOutcome{Output: output}, nil
		}
	})
}

func decodeExternalCommandInput(step saga.Step, execution *saga.Execution) (protocol.SagaExternalCommandStepInput, error) {
	raw := step.Input
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = execution.Input()
	}

	var input protocol.SagaExternalCommandStepInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return protocol.SagaExternalCommandStepInput{}, fmt.Errorf("невалидный input шага %s: %w", step.ID, err)
	}
	input.Executable = strings.TrimSpace(input.Executable)
	input.WorkingDir = strings.TrimSpace(input.WorkingDir)
	return input, nil
}

func buildCommandStatus(exitCode *int, runErr error) string {
	if runErr != nil {
		return "failed"
	}
	if exitCode != nil && *exitCode != 0 {
		return "failed"
	}
	return "completed"
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return bytes.Clone(value)
}

func cloneEnv(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}
