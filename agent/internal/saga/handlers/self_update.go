package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"etalon-agent/internal/protocol"
	"etalon-agent/internal/saga"
)

type SelfUpdateCapability interface {
	CurrentVersion() string
	Download(context.Context, string, string) (string, error)
	VerifySHA256(string, string) error
	ApplyAndRestart(string, []string) error
}

func NewSelfUpdatePreflightHandler() saga.StepHandler {
	return saga.StepHandlerFunc(func(_ context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		input, err := decodeSelfUpdateInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}
		switch {
		case input.TargetVersion == "":
			return saga.StepOutcome{}, fmt.Errorf("в input agent_self_update отсутствует target_version")
		case input.DownloadURL == "":
			return saga.StepOutcome{}, fmt.Errorf("в input agent_self_update отсутствует download_url")
		case input.RestartPolicy != "" && input.RestartPolicy != "immediate":
			return saga.StepOutcome{}, fmt.Errorf("restart_policy %q пока не поддерживается", input.RestartPolicy)
		}

		return saga.StepOutcome{
			Output: mustMarshalJSON(map[string]any{
				"target_version": input.TargetVersion,
				"download_url":   input.DownloadURL,
				"restart_policy": valueOrDefault(input.RestartPolicy, "immediate"),
			}),
		}, nil
	})
}

func NewSelfUpdateTargetVersionCheckHandler(capability SelfUpdateCapability) saga.StepHandler {
	return saga.StepHandlerFunc(func(_ context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		if capability == nil {
			return saga.StepOutcome{}, fmt.Errorf("native self-update capability не настроен")
		}

		input, err := decodeSelfUpdateInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}

		currentVersion := strings.TrimSpace(capability.CurrentVersion())
		output := mustMarshalJSON(map[string]any{
			"current_version": currentVersion,
			"target_version":  input.TargetVersion,
			"needs_update":    !strings.EqualFold(currentVersion, input.TargetVersion),
		})
		if currentVersion != "" && strings.EqualFold(currentVersion, input.TargetVersion) {
			return saga.StepOutcome{
				Status:      saga.StepStatusSkipped,
				Output:      output,
				Stop:        true,
				FinalStatus: saga.StatusCompleted,
				FinalResult: mustMarshalJSON(map[string]any{
					"skipped":         true,
					"reason":          "target_version уже запущена",
					"current_version": currentVersion,
					"target_version":  input.TargetVersion,
				}),
			}, nil
		}
		return saga.StepOutcome{Output: output}, nil
	})
}

func NewSelfUpdateDownloadMetadataCheckHandler() saga.StepHandler {
	return saga.StepHandlerFunc(func(_ context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		input, err := decodeSelfUpdateInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}
		if err := saga.ValidateSelfUpdateDownloadMetadata(input); err != nil {
			return saga.StepOutcome{}, err
		}
		return saga.StepOutcome{
			Output: mustMarshalJSON(map[string]any{
				"download_url":   input.DownloadURL,
				"sha256_present": strings.TrimSpace(input.SHA256) != "",
				"file_name":      resolveUpdateFileName(input),
			}),
		}, nil
	})
}

func NewNativeAgentSelfUpdateHandler(capability SelfUpdateCapability) saga.StepHandler {
	return saga.StepHandlerFunc(func(ctx context.Context, execution *saga.Execution, step saga.Step) (saga.StepOutcome, error) {
		if capability == nil {
			return saga.StepOutcome{}, fmt.Errorf("native self-update capability не настроен")
		}

		input, err := decodeSelfUpdateInput(step, execution)
		if err != nil {
			return saga.StepOutcome{}, err
		}

		fileName := resolveUpdateFileName(input)
		downloadedPath, err := capability.Download(ctx, input.DownloadURL, filepath.Base(fileName))
		if err != nil {
			return saga.StepOutcome{}, err
		}

		shouldCleanup := true
		defer func() {
			if shouldCleanup {
				_ = os.Remove(downloadedPath)
			}
		}()

		if err := capability.VerifySHA256(downloadedPath, input.SHA256); err != nil {
			return saga.StepOutcome{}, &saga.StepExecutionError{
				Err: err,
				Output: mustMarshalJSON(map[string]any{
					"downloaded_path": downloadedPath,
					"target_version":  input.TargetVersion,
				}),
			}
		}
		if err := capability.ApplyAndRestart(downloadedPath, input.Args); err != nil {
			return saga.StepOutcome{}, &saga.StepExecutionError{
				Err: err,
				Output: mustMarshalJSON(map[string]any{
					"downloaded_path": downloadedPath,
					"target_version":  input.TargetVersion,
				}),
			}
		}

		shouldCleanup = false
		return saga.StepOutcome{
			Output: mustMarshalJSON(map[string]any{
				"target_version":  input.TargetVersion,
				"downloaded_path": downloadedPath,
				"sha256_checked":  strings.TrimSpace(input.SHA256) != "",
				"restart_policy":  valueOrDefault(input.RestartPolicy, "immediate"),
			}),
		}, nil
	})
}

func decodeSelfUpdateInput(step saga.Step, execution *saga.Execution) (protocol.AgentSelfUpdateSagaInput, error) {
	raw := step.Input
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = execution.Input()
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage(`{}`)
	}

	var input protocol.AgentSelfUpdateSagaInput
	if err := json.Unmarshal(raw, &input); err != nil {
		return protocol.AgentSelfUpdateSagaInput{}, fmt.Errorf("невалидный input шага %s: %w", step.ID, err)
	}

	input.TargetVersion = strings.TrimSpace(input.TargetVersion)
	input.DownloadURL = strings.TrimSpace(input.DownloadURL)
	input.SHA256 = strings.TrimSpace(strings.ToLower(input.SHA256))
	input.FileName = sanitizeOptionalFileName(input.FileName)
	input.RestartPolicy = strings.ToLower(strings.TrimSpace(input.RestartPolicy))
	return input, nil
}

func resolveUpdateFileName(input protocol.AgentSelfUpdateSagaInput) string {
	if strings.TrimSpace(input.FileName) != "" {
		return filepath.Base(strings.TrimSpace(input.FileName))
	}
	if strings.TrimSpace(input.TargetVersion) != "" {
		return "agent-update-" + strings.TrimSpace(input.TargetVersion) + ".bin"
	}
	return "agent-update.bin"
}

func sanitizeOptionalFileName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	base := filepath.Base(value)
	if base == "." {
		return ""
	}
	return base
}

func mustMarshalJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

func valueOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}
