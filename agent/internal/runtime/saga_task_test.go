package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"etalon-agent/internal/config"
	"etalon-agent/internal/inventory"
	"etalon-agent/internal/protocol"
	"etalon-agent/internal/state"
	"etalon-agent/internal/workflows"
)

func TestAgentHeartbeatReturnsSagaResultInTaskResults(t *testing.T) {
	t.Parallel()

	content := []byte("agent-binary-v2")
	expectedSHA := sha256Hex(content)
	tempDir := t.TempDir()

	var heartbeatCalls int
	sagaWorkflow, err := workflows.NewSagaRunWorkflow(workflows.SagaRunWorkflowOptions{
		CurrentVersion: "1.0.0",
		SelfUpdater: testSelfUpdateDownloader{
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
			return nil
		},
		ApplyAndRestart: func(string, []string) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("не удалось создать saga workflow: %v", err)
	}

	agent := &Agent{
		cfg: config.Config{
			AgentVersion:           "1.0.0",
			AgentType:              "sssruner",
			AccessTokenGracePeriod: time.Minute,
			DataDir:                tempDir,
		},
		client: stubRuntimeClient{
			sendHeartbeatFn: func(_ context.Context, agentUUID string, data protocol.AgentDataDTO, accessToken string) (*protocol.HeartbeatResponseDTO, error) {
				heartbeatCalls++
				if agentUUID != "agent-uuid" {
					t.Fatalf("ожидался agent_uuid agent-uuid, получено %q", agentUUID)
				}
				if accessToken != "access-token" {
					t.Fatalf("ожидался access token access-token, получено %q", accessToken)
				}

				switch heartbeatCalls {
				case 1:
					if len(data.TaskResults) != 0 {
						t.Fatalf("первый heartbeat не должен содержать task_results, получено %d", len(data.TaskResults))
					}
					return &protocol.HeartbeatResponseDTO{
						Status: "ok",
						Tasks: []protocol.AgentTaskDTO{{
							ID:   77,
							Type: "saga_run",
							Payload: []byte(`{
								"saga_id":"saga-self-update-77",
								"saga_type":"agent_self_update",
								"input":{
									"target_version":"2.0.0",
									"download_url":"https://example.test/agent-2.0.0.exe",
									"sha256":"` + expectedSHA + `"
								}
							}`),
						}},
					}, nil
				case 2:
					if len(data.TaskResults) != 1 {
						t.Fatalf("второй heartbeat должен содержать один task_result, получено %d", len(data.TaskResults))
					}
					result := data.TaskResults[0]
					if result.ID != 77 {
						t.Fatalf("ожидался task id 77, получено %d", result.ID)
					}
					if result.Type != "saga_run" {
						t.Fatalf("ожидался task type saga_run, получено %q", result.Type)
					}
					if result.Status != "completed" {
						t.Fatalf("ожидался статус completed, получено %q", result.Status)
					}
					if result.SagaID != "saga-self-update-77" {
						t.Fatalf("ожидался saga_id saga-self-update-77, получено %q", result.SagaID)
					}
					if result.SagaType != "agent_self_update" {
						t.Fatalf("ожидался saga_type agent_self_update, получено %q", result.SagaType)
					}
					if len(result.Steps) != 4 {
						t.Fatalf("ожидалось 4 результата шага, получено %d", len(result.Steps))
					}
					if !strings.Contains(string(result.FinalResult), `"target_version":"2.0.0"`) {
						t.Fatalf("финальный результат не содержит target_version: %s", string(result.FinalResult))
					}
					return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
				default:
					return &protocol.HeartbeatResponseDTO{Status: "ok"}, nil
				}
			},
		},
		workflows: make(map[string]workflow),
		identity:  &state.Identity{UUID: "agent-uuid"},
		tokens: &state.Tokens{
			AccessToken:          "access-token",
			AccessTokenExpiresAt: time.Now().Add(time.Hour),
		},
		inventoryService: stubRuntimeInventoryService{
			snapshot: SnapshotResult{
				value: inventory.Snapshot{Hostname: "cash-1"},
				ok:    true,
			},
		},
		adapterManager: stubRuntimeAdapterManager{},
	}
	agent.registerWorkflow(sagaWorkflow)
	agent.registerWorkflowAlias("run_saga", sagaWorkflow)

	if err := agent.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("первый heartbeat завершился ошибкой: %v", err)
	}
	if err := agent.heartbeatLocked(t.Context()); err != nil {
		t.Fatalf("второй heartbeat завершился ошибкой: %v", err)
	}
	if heartbeatCalls != 2 {
		t.Fatalf("ожидалось два heartbeat, получено %d", heartbeatCalls)
	}
}

type testSelfUpdateDownloader struct {
	downloadFn func(context.Context, string, string) (string, error)
}

func (s testSelfUpdateDownloader) Download(ctx context.Context, url, fileName string) (string, error) {
	return s.downloadFn(ctx, url, fileName)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
