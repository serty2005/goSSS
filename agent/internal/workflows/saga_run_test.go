package workflows

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sagahandlers "etalon-agent/internal/saga/handlers"
)

type stubSelfUpdateDownloader struct {
	downloadFn func(context.Context, string, string) (string, error)
}

func (s stubSelfUpdateDownloader) Download(ctx context.Context, url, fileName string) (string, error) {
	return s.downloadFn(ctx, url, fileName)
}

type stubCommandRunner struct {
	runFn func(context.Context, sagahandlers.CommandRequest) (sagahandlers.CommandResult, error)
}

func (s stubCommandRunner) Run(ctx context.Context, req sagahandlers.CommandRequest) (sagahandlers.CommandResult, error) {
	if s.runFn == nil {
		return sagahandlers.CommandResult{}, nil
	}
	return s.runFn(ctx, req)
}

func TestSagaRunWorkflowAgentSelfUpdateHappyPath(t *testing.T) {
	t.Parallel()

	content := []byte("agent-binary-v2")
	expectedSHA := sha256Hex(content)
	tempDir := t.TempDir()

	var appliedPath string
	workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: stubSelfUpdateDownloader{
			downloadFn: func(_ context.Context, _ string, fileName string) (string, error) {
				if strings.TrimSpace(fileName) == "" {
					fileName = "agent-update.bin"
				}
				target := filepath.Join(tempDir, fileName)
				if writeErr := os.WriteFile(target, content, 0o644); writeErr != nil {
					return "", writeErr
				}
				return target, nil
			},
		},
		DataDir: tempDir,
		VerifySHA256: func(path, expected string) error {
			if expected != expectedSHA {
				t.Fatalf("ожидался sha256 %s, получено %s", expectedSHA, expected)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if sha256Hex(data) != expected {
				t.Fatalf("файл %s сохранён с неожиданным содержимым", path)
			}
			return nil
		},
		ApplyAndRestart: func(path string, _ []string) error {
			appliedPath = path
			return nil
		},
	})
	if err != nil {
		t.Fatalf("не удалось создать workflow saga_run: %v", err)
	}

	result, runErr := workflow.Run(t.Context(), []byte(`{
		"saga_id":"saga-update-1",
		"saga_type":"agent_self_update",
		"request_id":"req-1",
		"input":{
			"target_version":"2.0.0",
			"download_url":"https://example.test/agent-2.0.0.exe",
			"sha256":"`+expectedSHA+`",
			"file_name":"agent-2.0.0.exe",
			"restart_policy":"immediate"
		}
	}`))
	if runErr != nil {
		t.Fatalf("Run завершился ошибкой: %v", runErr)
	}
	if result.Status != "completed" {
		t.Fatalf("ожидался статус completed, получено %q", result.Status)
	}
	if result.SagaID != "saga-update-1" {
		t.Fatalf("ожидался saga_id saga-update-1, получено %q", result.SagaID)
	}
	if result.SagaType != "agent_self_update" {
		t.Fatalf("ожидался saga_type agent_self_update, получено %q", result.SagaType)
	}
	if len(result.Steps) != 4 {
		t.Fatalf("ожидалось 4 шага, получено %d", len(result.Steps))
	}
	if result.Steps[3].Status != "completed" {
		t.Fatalf("ожидался completed для последнего шага, получено %q", result.Steps[3].Status)
	}
	if result.FinalResult == nil || !strings.Contains(string(result.FinalResult), `"target_version":"2.0.0"`) {
		t.Fatalf("финальный результат не содержит target_version: %s", string(result.FinalResult))
	}
	if appliedPath == "" {
		t.Fatal("ожидался вызов native ApplyAndRestart")
	}
}

func TestSagaRunWorkflowRejectsInvalidPayload(t *testing.T) {
	t.Parallel()

	workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: stubSelfUpdateDownloader{
			downloadFn: func(context.Context, string, string) (string, error) {
				return "", nil
			},
		},
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("не удалось создать workflow saga_run: %v", err)
	}

	result, runErr := workflow.Run(t.Context(), []byte(`{
		"saga_type":"agent_self_update",
		"input":{"target_version":"2.0.0","download_url":"https://example.test/agent.exe"}
	}`))
	if runErr == nil {
		t.Fatal("ожидалась ошибка из-за отсутствующего saga_id")
	}
	if result.Status != "failed" {
		t.Fatalf("ожидался статус failed, получено %q", result.Status)
	}
	if !strings.Contains(result.Error, "saga_id") {
		t.Fatalf("ожидалась ошибка про saga_id, получено %q", result.Error)
	}
}

func TestSagaRunWorkflowRejectsUnknownSagaType(t *testing.T) {
	t.Parallel()

	workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: stubSelfUpdateDownloader{
			downloadFn: func(context.Context, string, string) (string, error) {
				return "", nil
			},
		},
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("не удалось создать workflow saga_run: %v", err)
	}

	result, runErr := workflow.Run(t.Context(), []byte(`{
		"saga_id":"saga-unknown",
		"saga_type":"unknown_saga"
	}`))
	if runErr == nil {
		t.Fatal("ожидалась ошибка на неизвестном saga_type")
	}
	if result.Status != "failed" {
		t.Fatalf("ожидался статус failed, получено %q", result.Status)
	}
	if !strings.Contains(result.Error, "неизвестный saga_type") {
		t.Fatalf("получена неожиданная ошибка: %q", result.Error)
	}
}

func TestSagaRunWorkflowRejectsUnknownStepType(t *testing.T) {
	t.Parallel()

	workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: stubSelfUpdateDownloader{
			downloadFn: func(context.Context, string, string) (string, error) {
				return "", nil
			},
		},
		DataDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("не удалось создать workflow saga_run: %v", err)
	}

	result, runErr := workflow.Run(t.Context(), []byte(`{
		"saga_id":"saga-unknown-step",
		"saga_type":"agent_self_update",
		"input":{"target_version":"2.0.0","download_url":"https://example.test/agent.exe"},
		"steps":[{"id":"mystery","type":"runner.unknown"}]
	}`))
	if runErr == nil {
		t.Fatal("ожидалась ошибка на неизвестном типе шага")
	}
	if result.Status != "failed" {
		t.Fatalf("ожидался статус failed, получено %q", result.Status)
	}
	if !strings.Contains(result.Error, "неизвестный тип шага") {
		t.Fatalf("получена неожиданная ошибка: %q", result.Error)
	}
}

func TestSagaRunWorkflowStopsOnStepFailure(t *testing.T) {
	t.Parallel()

	content := []byte("agent-binary-v2")
	expectedSHA := sha256Hex(content)
	tempDir := t.TempDir()

	commandCalled := false
	workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: stubSelfUpdateDownloader{
			downloadFn: func(_ context.Context, _ string, fileName string) (string, error) {
				if strings.TrimSpace(fileName) == "" {
					fileName = "agent-update.bin"
				}
				target := filepath.Join(tempDir, fileName)
				if writeErr := os.WriteFile(target, content, 0o644); writeErr != nil {
					return "", writeErr
				}
				return target, nil
			},
		},
		DataDir: t.TempDir(),
		VerifySHA256: func(string, string) error {
			return nil
		},
		ApplyAndRestart: func(string, []string) error {
			return fmt.Errorf("не удалось применить обновление")
		},
		CommandRunner: stubCommandRunner{
			runFn: func(context.Context, sagahandlers.CommandRequest) (sagahandlers.CommandResult, error) {
				commandCalled = true
				return sagahandlers.CommandResult{}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("не удалось создать workflow saga_run: %v", err)
	}

	payload := `{
		"saga_id":"saga-fail-stop",
		"saga_type":"agent_self_update",
		"input":{
			"target_version":"2.0.0",
			"download_url":"https://example.test/agent-2.0.0.exe",
			"sha256":"` + expectedSHA + `"
		},
		"steps":[
			{"id":"preflight","type":"runner.self_update_preflight"},
			{"id":"version_check","type":"runner.self_update_target_version_check"},
			{"id":"metadata_check","type":"runner.self_update_download_metadata_check"},
			{"id":"apply_update","type":"native.agent_self_update"},
			{"id":"after_fail","type":"external.command_run","input":{"executable":"cmd.exe","args":["/c","echo should-not-run"]}}
		]
	}`
	result, runErr := workflow.Run(t.Context(), []byte(payload))
	if runErr == nil {
		t.Fatal("ожидалась ошибка при неуспешном native self-update")
	}
	if result.Status != "failed" {
		t.Fatalf("ожидался статус failed, получено %q", result.Status)
	}
	if commandCalled {
		t.Fatal("последующий шаг не должен запускаться после ошибки")
	}
	if len(result.Steps) != 4 {
		t.Fatalf("ожидались результаты только для выполненных шагов, получено %d", len(result.Steps))
	}
	if result.Steps[3].Status != "failed" {
		t.Fatalf("ожидался failed у шага apply_update, получено %q", result.Steps[3].Status)
	}
}

func TestSagaRunWorkflowWritesDebugTraceOnlyInDebugMode(t *testing.T) {
	t.Parallel()

	run := func(debug bool) ([]string, []string, []string) {
		infoMessages := make([]string, 0)
		debugMessages := make([]string, 0)

		workflow, err := NewSagaRunWorkflow(SagaRunWorkflowOptions{
			CurrentVersion: "2.0.0",
			SelfUpdater: stubSelfUpdateDownloader{
				downloadFn: func(context.Context, string, string) (string, error) {
					return "", nil
				},
			},
			DataDir: t.TempDir(),
			Debug:   debug,
			Infof: func(format string, args ...any) {
				infoMessages = append(infoMessages, strings.TrimSpace(fmt.Sprintf(format, args...)))
			},
			Debugf: func(format string, args ...any) {
				debugMessages = append(debugMessages, strings.TrimSpace(fmt.Sprintf(format, args...)))
			},
		})
		if err != nil {
			t.Fatalf("не удалось создать workflow saga_run: %v", err)
		}

		result, runErr := workflow.Run(t.Context(), []byte(`{
			"saga_id":"saga-debug",
			"saga_type":"agent_self_update",
			"input":{
				"target_version":"2.0.0",
				"download_url":"https://example.test/agent.exe"
			}
		}`))
		if runErr != nil {
			t.Fatalf("Run завершился ошибкой: %v", runErr)
		}

		levels := make([]string, 0, len(result.ExecutionLog))
		for _, entry := range result.ExecutionLog {
			levels = append(levels, entry.Level)
		}
		return infoMessages, debugMessages, levels
	}

	_, debugMessagesOff, levelsOff := run(false)
	if len(debugMessagesOff) != 0 {
		t.Fatalf("в обычном режиме debug-лог не должен писаться: %v", debugMessagesOff)
	}
	if slices.Contains(levelsOff, "debug") {
		t.Fatalf("в обычном режиме execution_log не должен содержать debug-записи: %v", levelsOff)
	}

	_, debugMessagesOn, levelsOn := run(true)
	if len(debugMessagesOn) == 0 {
		t.Fatal("в debug-режиме ожидались debug-сообщения")
	}
	if !slices.Contains(levelsOn, "debug") {
		t.Fatalf("в debug-режиме execution_log должен содержать debug-записи: %v", levelsOn)
	}
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
